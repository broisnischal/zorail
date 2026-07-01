package cfsetup

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

//go:embed worker.js
var workerJS string

// Options configure a setup/doctor run. Empty fields fall back to prompts or
// sensible defaults.
type Options struct {
	CFToken   string // Cloudflare API token
	Domain    string // mail domain (the Cloudflare zone)
	ServerURL string // local Zorail API base, e.g. http://127.0.0.1:8090
	Hostname  string // public ingress hostname; default ingest.<domain>
	EnvFile   string // dotenv path to read/write ZORAIL_API_TOKEN; default ./.env
	Yes       bool   // skip confirmation prompts
}

var slugRe = regexp.MustCompile(`[^a-z0-9-]+`)

func slug(s string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// Run executes the full guided setup.
func Run(ctx context.Context, o Options) error {
	in := bufio.NewReader(os.Stdin)

	fmt.Println(bold("\n  zorail setup — connect a real domain to your localhost Zorail\n"))
	fmt.Println("  This points your domain's mail at this machine via Cloudflare Email")
	fmt.Print("  Routing → an Email Worker → a Cloudflare Tunnel → /api/ingest.\n\n")

	// ---- defaults ----
	if o.ServerURL == "" {
		o.ServerURL = "http://127.0.0.1:8090"
	}
	if o.EnvFile == "" {
		o.EnvFile = RepoEnvFile()
	}
	origin := localOrigin(o.ServerURL)

	// ---- Cloudflare token (resolved first: we need it to list zones) ----
	o.CFToken = resolveCFToken(in, o)
	if o.CFToken == "" {
		return fmt.Errorf("a Cloudflare API token is required")
	}
	cf := NewCF(o.CFToken)
	step("Verifying Cloudflare token")
	if err := cf.VerifyToken(ctx); err != nil {
		if errors.Is(err, ErrAccountOwnedToken) {
			warnf("that token is an %s API token, which Cloudflare's Tunnel API rejects.", bold("account-owned"))
			fmt.Println("\n  Create a " + bold("user-owned") + " token instead (works with every step). The link below")
			fmt.Println("  opens the right page with the permissions pre-selected:")
			fmt.Println("  " + cmd(cfTokenTemplateURL()))
			fmt.Println("\n  Then re-run " + bold("zorail setup") + " and paste the new token.")
			return fmt.Errorf("account-owned token cannot be used for setup")
		}
		return err
	}

	// ---- mail domain (picked from the account's zones when not given) ----
	if o.Domain == "" {
		picked, err := pickDomain(ctx, in, cf)
		if err != nil {
			return err
		}
		o.Domain = picked
	}
	o.Domain = strings.ToLower(strings.TrimSpace(o.Domain))
	if o.Domain == "" {
		return fmt.Errorf("a domain is required")
	}
	if o.Hostname == "" {
		o.Hostname = "ingest." + o.Domain
	}

	zone, err := cf.Zone(ctx, o.Domain)
	if err != nil {
		return err
	}
	// If the mail domain is a subdomain of the zone, route just that subdomain.
	if !strings.EqualFold(o.Domain, zone.Name) {
		okf("zone %s · routing subdomain %s (account %s)", zone.Name, o.Domain, firstNonEmpty(zone.Account.Name, zone.Account.ID))
	} else {
		okf("zone %s (account %s)", zone.Name, firstNonEmpty(zone.Account.Name, zone.Account.ID))
	}

	// ---- 1b. verify the token can do EVERYTHING before we touch anything ----
	if err := preflightPerms(ctx, cf, zone.Account.ID, zone.Account.Name, zone.ID); err != nil {
		return err
	}

	// ---- 2. ensure the server requires auth (the tunnel makes ingest public) ----
	step("Checking the local Zorail server")
	serverURL, cfg, err := resolveServer(ctx, o.ServerURL, o.EnvFile)
	if err != nil {
		return err
	}
	o.ServerURL = serverURL // adopt whichever port actually answered
	okf("reachable · domain %s · version %s", firstNonEmpty(cfg.Domain, "?"), firstNonEmpty(cfg.Version, "?"))

	ingestToken, _, err := ensureServerToken(ctx, o, cfg.AuthRequired)
	if err != nil {
		return err
	}

	// ---- 3. provision Cloudflare ----
	tunnelName := "zorail-" + slug(o.Domain)
	step("Creating Cloudflare Tunnel %q", tunnelName)
	tunnel, err := cf.EnsureTunnel(ctx, zone.Account.ID, tunnelName)
	if err != nil {
		return diagnoseAccountPerm(ctx, cf, zone.Account.ID, zone.Account.Name, err, "Cloudflare Tunnel → Edit")
	}
	okf("tunnel %s", tunnel.ID)

	step("Routing %s → %s (only /api/ingest)", o.Hostname, origin)
	if err := cf.ConfigureTunnelIngress(ctx, zone.Account.ID, tunnel.ID, o.Hostname, "/api/ingest", origin); err != nil {
		return err
	}
	cname := tunnel.ID + ".cfargotunnel.com"
	proxied := true
	if err := cf.UpsertDNS(ctx, zone.ID, DNSRecord{Type: "CNAME", Name: o.Hostname, Content: cname, Proxied: &proxied, TTL: 1}); err != nil {
		return err
	}
	okf("DNS %s → %s (proxied)", o.Hostname, cname)

	worker := "zorail-ingest-" + slug(o.Domain)
	ingestURL := "https://" + o.Hostname + "/api/ingest"
	step("Deploying Email Worker %q", worker)
	if err := cf.DeployEmailWorker(ctx, zone.Account.ID, worker, workerJS, []workerVar{
		{Name: "INGEST_URL", Value: ingestURL},
		{Name: "INGEST_TOKEN", Value: ingestToken, Secret: true},
	}); err != nil {
		return err
	}
	okf("worker deployed · posts to %s", ingestURL)

	step("Enabling Email Routing on %s", o.Domain)
	// Cloudflare's Email Routing *settings* endpoints (GET /email/routing and
	// GET /email/routing/dns) reject scoped API tokens with error 10000 no
	// matter which permissions the token holds — a known API-side quirk. So the
	// status read and enable are best-effort: a failure here must not abort a
	// run that has already provisioned the tunnel, worker, and ingress.
	enabled := false
	if er, err := cf.GetEmailRouting(ctx, zone.ID); err == nil {
		enabled = er.Enabled
	}
	if !enabled {
		if err := cf.EnableEmailRouting(ctx, zone.ID); err != nil {
			warnf("couldn't enable Email Routing via API: %v", err)
			if isAuthzError(err) {
				warnf("add %s to the token (it gates Email Routing settings), recreate it, and re-run setup —", bold("Zone → Zone Settings → Edit"))
				warnf("or enable it once in the dashboard (zone %s → Email → Email Routing).", zone.Name)
			} else {
				warnf("enable it once in the dashboard (zone %s → Email → Email Routing), then mail will flow", zone.Name)
			}
		} else {
			okf("Email Routing enabled")
		}
	}
	// Write the required MX + SPF records ourselves rather than fetching them
	// from the token-rejecting GET /email/routing/dns. They are the same for
	// every zone, and UpsertDNS (DNS:Edit, which works) is idempotent.
	recs := StandardEmailRoutingDNS(o.Domain)
	for _, r := range recs {
		if err := cf.UpsertDNS(ctx, zone.ID, r); err != nil {
			return fmt.Errorf("add routing record %s %s: %w", r.Type, r.Name, err)
		}
	}
	okf("MX + SPF records for %s in place (%d)", o.Domain, len(recs))

	step("Setting catch-all: *@%s → %s", o.Domain, worker)
	if err := cf.SetCatchAllToWorker(ctx, zone.ID, worker); err != nil {
		return permHint(err, "Zone → Email Routing Rules → Edit")
	}
	okf("catch-all active")

	// ---- persist a complete .env so the server and `zorail up` just work ----
	httpAddr := ":" + portOf(o.ServerURL, "8090")
	if err := writeServerEnv(o.EnvFile, [][2]string{
		{"CLOUDFLARE_API_TOKEN", o.CFToken},
		{"ZORAIL_API_TOKEN", ingestToken},
		{"ZORAIL_DOMAIN", o.Domain},
		{"ZORAIL_ALLOWED_DOMAINS", o.Domain},
		{"ZORAIL_HTTP_ADDR", httpAddr},
		{"ZORAIL_SMTP_ADDR", ":1025"},
		{"ZORAIL_TUNNEL_TOKEN", tunnel.Token},
	}); err != nil {
		return fmt.Errorf("write %s: %w", o.EnvFile, err)
	}
	okf("wrote server config to %s", o.EnvFile)

	// ---- 4. persist state ----
	st := &State{
		Domain: o.Domain, Hostname: o.Hostname, Origin: origin,
		ZoneID: zone.ID, AccountID: zone.Account.ID, TunnelID: tunnel.ID, Worker: worker,
	}
	if err := SaveState(st); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	// ---- 5. the tunnel daemon (must run on this machine) ----
	printTunnelInstructions(o.EnvFile)
	return nil
}

// ensureServerToken returns the token the Worker should authenticate ingest
// with, provisioning a global ZORAIL_API_TOKEN when the server is in open mode
// (otherwise the public tunnel would let anyone inject mail).
func ensureServerToken(ctx context.Context, o Options, authRequired bool) (token string, restartNeeded bool, err error) {
	if authRequired {
		// Server already locked down; we need its existing admin token. Check
		// every place it might live: the client var ($ZORAIL_TOKEN), the server
		// var ($ZORAIL_API_TOKEN, in case it's exported rather than in .env), and
		// the .env file setup itself writes.
		token = firstNonEmpty(os.Getenv("ZORAIL_TOKEN"),
			firstNonEmpty(os.Getenv("ZORAIL_API_TOKEN"), readEnvValue(o.EnvFile, "ZORAIL_API_TOKEN")))
		if token == "" {
			fmt.Printf("    %s find it with: %s\n", faint("tip:"), cmd("grep ZORAIL_API_TOKEN "+o.EnvFile))
			token = promptSecret("Server requires auth — paste its ZORAIL_API_TOKEN")
		}
		if token == "" {
			return "", false, fmt.Errorf("the server requires a token; provide it via $ZORAIL_TOKEN or %s", o.EnvFile)
		}
		// Validate it works for ingest scope.
		if err := newZorail(o.ServerURL, token).check(ctx); err != nil {
			return "", false, fmt.Errorf("provided token rejected by server: %w", err)
		}
		okf("using existing server token")
		return token, false, nil
	}

	// Open mode: provision a token, write it to the env file, and require a
	// restart. Reuse a token already written by a previous run so repeated
	// setups (e.g. after fixing a permission) stay consistent.
	token = readEnvValue(o.EnvFile, "ZORAIL_API_TOKEN")
	if token == "" {
		token = newToken()
		if err := upsertEnvValue(o.EnvFile, "ZORAIL_API_TOKEN", token); err != nil {
			return "", false, fmt.Errorf("write %s: %w", o.EnvFile, err)
		}
		warnf("server is in OPEN mode — wrote ZORAIL_API_TOKEN to %s", o.EnvFile)
	} else {
		warnf("server is in OPEN mode — reusing ZORAIL_API_TOKEN already in %s", o.EnvFile)
	}
	warnf("restart the server so it requires this token before mail is exposed publicly")
	return token, true, nil
}

func printTunnelInstructions(envFile string) {
	fmt.Println(bold("\n  ✓ Cloudflare is configured. Start everything on THIS machine with one command:\n"))
	fmt.Println(cmd("       zorail up"))
	fmt.Printf("\n  That loads %s, starts the Zorail server and the Cloudflare Tunnel\n", envFile)
	fmt.Print("  together (installing cloudflared if needed), and streams their logs.\n")

	fmt.Println(bold("\n  Then, in another terminal, verify end-to-end:"))
	fmt.Println(cmd("       zorail doctor"))
	fmt.Println("\n  Once it's up and DNS has propagated (a few minutes), send a message to")
	fmt.Print("  anything@your-domain and watch it land in `zorail watch`.\n\n")

	have := exec.Command("cloudflared", "--version").Run() == nil
	if !have {
		fmt.Println(dim("  (cloudflared isn't installed yet — `zorail up` will guide you, or get it"))
		fmt.Print(dim("   from https://pkg.cloudflare.com/ · macOS: `brew install cloudflared`)\n\n"))
	}
}

// resolveServer finds the running Zorail server, tolerating the port mismatch
// between `zorail serve` (defaults to :8080) and the tooling default (:8090).
// It probes, in order: the requested URL, the port from ZORAIL_HTTP_ADDR (env
// or .env), then the two common defaults — and adopts the first that answers
// /api/config. This means the operator doesn't have to know or match the port.
func resolveServer(ctx context.Context, preferred, envFile string) (string, *zorailConfig, error) {
	candidates := []string{preferred}
	if a := firstNonEmpty(os.Getenv("ZORAIL_HTTP_ADDR"), readEnvValue(envFile, "ZORAIL_HTTP_ADDR")); a != "" {
		candidates = append(candidates, "http://"+localProbeHost(a))
	}
	candidates = append(candidates, "http://127.0.0.1:8090", "http://127.0.0.1:8080")

	seen := map[string]bool{}
	var tried []string
	for _, u := range candidates {
		u = strings.TrimRight(strings.TrimSpace(u), "/")
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		tried = append(tried, u)
		if cfg, err := newZorail(u, "").config(ctx); err == nil {
			if u != strings.TrimRight(preferred, "/") {
				okf("found server at %s", u)
			}
			return u, cfg, nil
		}
	}
	return "", nil, fmt.Errorf("no Zorail server reachable (tried %s).\n    Start it first in another terminal — %s — then re-run setup.",
		strings.Join(tried, ", "), cmd("zorail serve"))
}

// localOrigin derives the http://localhost:PORT the tunnel points at from the
// server URL (the tunnel runs on the same host, so localhost is correct).
func localOrigin(serverURL string) string {
	u, err := url.Parse(serverURL)
	if err != nil || u.Host == "" {
		return "http://localhost:8090"
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "8090"
		}
	}
	return "http://localhost:" + port
}

const cfTokenPage = "https://dash.cloudflare.com/profile/api-tokens"

// preflightPerms verifies the token can perform every operation setup needs,
// BEFORE any provisioning writes happen, and reports ALL gaps at once. This
// turns the old fail-one-error-at-a-time loop (fix token, rerun, hit the next
// missing permission, rerun again) into a single fix-and-go pass.
//
// It probes what Cloudflare lets us probe: account scope, Cloudflare Tunnel,
// Workers Scripts, and DNS. Email Routing Rules cannot be read-probed (the
// Email Routing GET endpoints reject scoped tokens regardless of permissions —
// a documented Cloudflare quirk), so it is always listed as a required manual
// add and enforced with a clear error at the catch-all step.
func preflightPerms(ctx context.Context, cf *CF, accountID, accountName, zoneID string) error {
	step("Checking token permissions")

	// If the whole account is out of the token's Account Resources scope, every
	// account-level probe fails for that one reason — report it once and stop.
	if inScope, err := cf.CanAccessAccount(ctx, accountID); err == nil && !inScope {
		return fmt.Errorf("your token cannot act on account %s — its %s scope excludes it.\n    Recreate the token with %s at %s, then re-run setup.",
			bold(firstNonEmpty(accountName, accountID)), bold("Account Resources"), bold("Account Resources → Include → All accounts"), cmd(cfTokenPage))
	}

	checks := []struct {
		label, path, hint string
	}{
		{"Zone · DNS Edit", "/zones/" + zoneID + "/dns_records?per_page=1", "Zone → DNS → Edit"},
		{"Account · Cloudflare Tunnel Edit", "/accounts/" + accountID + "/cfd_tunnel?per_page=1", "Account → Cloudflare Tunnel → Edit"},
		{"Account · Workers Scripts Edit", "/accounts/" + accountID + "/workers/scripts?per_page=1", "Account → Workers Scripts → Edit"},
		// The Email Routing settings endpoint is gated by Zone Settings (not by
		// the Email Routing Rules permission) — this probe catches the missing
		// permission that makes "enable Email Routing" fail with error 10000.
		{"Zone · Zone Settings (Email Routing)", "/zones/" + zoneID + "/email/routing", "Zone → Zone Settings → Edit"},
	}
	var missing []string
	for _, c := range checks {
		switch err := cf.Probe(ctx, c.path); {
		case err == nil:
			okf("%s", c.label)
		case isAuthzError(err):
			warnf("%s — missing", c.label)
			missing = append(missing, c.hint)
		default:
			// Network/transport hiccup, not a permission problem — don't block.
			warnf("%s — could not verify (%v)", c.label, err)
		}
	}
	if len(missing) > 0 {
		// Always append the un-probeable Email Routing Rules so the user adds
		// everything in one recreate.
		missing = append(missing, "Zone → Email Routing Rules → Edit "+faint("(cannot be auto-checked)"))
		return fmt.Errorf("the API token is missing required permissions:\n      • %s\n    Add them at %s (\"+ Add more\"), recreate the token, and re-run setup.",
			strings.Join(missing, "\n      • "), cmd(cfTokenPage))
	}
	okf("token permissions OK (add %s too if setup stops at the catch-all step)", bold("Zone → Email Routing Rules → Edit"))
	return nil
}

// permHint enriches a Cloudflare authorization failure with the exact token
// permission the step needs. Tunnel and Email Routing permissions cannot be
// pre-filled by the token-creation link, so a user who skipped the manual
// "+ Add more" step lands here — this tells them precisely what to add.
// Non-authorization errors pass through unchanged.
func permHint(err error, perm string) error {
	if err == nil || !isAuthzError(err) {
		return err
	}
	return fmt.Errorf("%w\n\n    This token is missing %s.\n    Add it at %s (\"+ Add more\"), recreate the token, and re-run setup.",
		err, bold(perm), cmd(cfTokenPage))
}

// diagnoseAccountPerm turns an authorization failure on an account-level call
// into a precise, actionable message by probing which accounts the token can
// reach. "Authentication error 10000" here means one of two distinct things,
// and we tell the user exactly which:
//   - the account is outside the token's Account Resources scope, or
//   - the account is in scope but the specific permission was never granted.
func diagnoseAccountPerm(ctx context.Context, cf *CF, accountID, accountName string, err error, perm string) error {
	if err == nil || !isAuthzError(err) {
		return err
	}
	name := firstNonEmpty(accountName, accountID)
	if inScope, cerr := cf.CanAccessAccount(ctx, accountID); cerr == nil && !inScope {
		return fmt.Errorf("%w\n\n    Your token cannot act on account %s.\n    Its \"Account Resources\" scope excludes it — recreate the token with\n    Account Resources = %s (or select that account) at %s, then re-run setup.",
			err, bold(name), bold("All accounts"), cmd(cfTokenPage))
	}
	return fmt.Errorf("%w\n\n    This token is missing %s for account %s.\n    Add it (\"+ Add more\"), recreate the token at %s, and re-run setup.",
		err, bold("Account → "+perm), bold(name), cmd(cfTokenPage))
}

// isAuthzError reports whether a Cloudflare error is an authentication/permission
// failure (as opposed to a not-found, rate-limit, or transport error).
func isAuthzError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, marker := range []string{"error 10000", "error 10001", "error 9109", "Authentication error", "Unauthorized", "not authorized", "authentication"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// resolveCFToken finds the Cloudflare token from (in order) the --cf-token
// flag, $CLOUDFLARE_API_TOKEN, the dotenv file, or a guided browser-assisted
// prompt. Setup persists it back to .env, so it is asked at most once.
func resolveCFToken(in *bufio.Reader, o Options) string {
	if strings.TrimSpace(o.CFToken) != "" {
		return strings.TrimSpace(o.CFToken)
	}
	if v := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN")); v != "" {
		return v
	}
	if v := readEnvValue(o.EnvFile, "CLOUDFLARE_API_TOKEN"); v != "" {
		okf("using Cloudflare token from %s", o.EnvFile)
		return v
	}
	return guidedToken()
}

// guidedToken walks a first-time user through creating a scoped API token. It
// opens a Cloudflare token page with all required permissions *pre-selected*
// (via a template URL), so the user only reviews and clicks Create. The
// checklist is printed too, as a safety net in case a permission key changes.
func guidedToken() string {
	tokenURL := cfTokenTemplateURL()
	fmt.Println("\n  A Cloudflare API token is needed once (it'll be saved for next time).")
	fmt.Println("  Opening the token-creation page. Cloudflare can only pre-fill some")
	fmt.Println("  permissions via a link, so you'll add the rest with " + bold(`"+ Add more"`) + ".")
	fmt.Println("  " + cmd(tokenURL))
	fmt.Println("\n  " + bold("Already selected by the link:"))
	fmt.Println("    • Account → Workers Scripts          : Edit")
	fmt.Println("    • Zone    → DNS                      : Edit")
	fmt.Println("    • Zone    → Zone                     : Read")
	fmt.Println("\n  " + bold(`Add these yourself ("+ Add more") — required:`))
	fmt.Println("    • Account → " + bold("Cloudflare Tunnel") + "        : Edit")
	fmt.Println("    • Zone    → " + bold("Email Routing Rules") + "      : Edit")
	fmt.Println("    • Zone    → " + bold("Zone Settings") + "            : Edit   " + faint("(to enable Email Routing)"))
	fmt.Println("    • Account → Email Routing Addresses  : Read   " + faint("(only for forwarding)"))
	fmt.Println("\n  Account Resources: your account · Zone Resources: All zones.")
	fmt.Println("  Then click " + bold("Create Token") + ", copy it, and paste below.")
	openBrowser(tokenURL)
	return promptSecret("Paste the API token")
}

// cfTokenTemplateURL builds a Cloudflare dashboard link that opens the
// create-token page with exactly the permissions Zorail needs pre-selected and
// the token name filled in.
//
// It targets the USER-owned token page (/profile/api-tokens), NOT an
// account-owned token. This is deliberate: Cloudflare's Tunnel (cfd_tunnel) API
// rejects account-owned tokens with "Authentication error (10000)", so an
// account token would pass the early checks and then fail mid-setup at tunnel
// creation. A user-owned token with the same permissions works across every
// endpoint setup touches (verify, zones, tunnel, workers, email routing, DNS).
// The permissionGroupKeys template mechanism is identical for both token types.
// See https://developers.cloudflare.com/fundamentals/api/how-to/create-via-multiple-accounts/
func cfTokenTemplateURL() string {
	const perms = `[` +
		`{"key":"argo_tunnel","type":"edit"},` + // Account · Cloudflare Tunnel
		`{"key":"workers_scripts","type":"edit"},` + // Account · Workers Scripts
		`{"key":"email_routing_addresses","type":"read"},` + // Account · Email Routing Addresses
		`{"key":"dns","type":"edit"},` + // Zone · DNS
		`{"key":"email_routing_rules","type":"edit"},` + // Zone · Email Routing Rules
		`{"key":"zone_settings","type":"edit"},` + // Zone · Zone Settings (gates Email Routing enable/status)
		`{"key":"zone","type":"read"}` + // Zone · Zone
		`]`
	q := url.Values{}
	q.Set("permissionGroupKeys", perms)
	q.Set("accountId", "*")
	q.Set("zoneId", "all")
	q.Set("name", "Zorail mail setup")
	return "https://dash.cloudflare.com/profile/api-tokens?" + q.Encode()
}

// pickDomain lists the account's zones and lets the user choose by number, or
// type a domain (e.g. a subdomain of a zone) directly.
func pickDomain(ctx context.Context, in *bufio.Reader, cf *CF) (string, error) {
	step("Fetching your Cloudflare domains")
	zones, err := cf.ListZones(ctx)
	if err != nil {
		return "", err
	}
	if len(zones) == 0 {
		return prompt(in, "Mail domain (e.g. example.com)", ""), nil
	}
	for i, z := range zones {
		fmt.Printf("      %d. %s\n", i+1, z.Name)
	}
	for {
		choice := prompt(in, fmt.Sprintf("Pick a domain [1-%d], or type one (e.g. mail.%s)", len(zones), zones[0].Name), "1")
		if n, err := strconv.Atoi(choice); err == nil {
			if n >= 1 && n <= len(zones) {
				return zones[n-1].Name, nil
			}
			fmt.Println("      out of range — try again")
			continue
		}
		if strings.Contains(choice, ".") {
			return choice, nil
		}
		fmt.Println("      not a number or a domain — try again")
	}
}

// openBrowser best-effort opens url in the default browser; failure is silent
// (the URL is always printed too).
func openBrowser(url string) {
	var bin string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		bin = "open"
	case "windows":
		bin, args = "cmd", []string{"/c", "start"}
	default:
		bin = "xdg-open"
	}
	_ = exec.Command(bin, append(args, url)...).Start()
}

// portOf returns the port in serverURL, or def when absent/unparseable.
func portOf(serverURL, def string) string {
	u, err := url.Parse(serverURL)
	if err != nil || u.Port() == "" {
		return def
	}
	return u.Port()
}

// ---- tiny console helpers ----

func prompt(in *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("  %s [%s]: ", label, def)
	} else {
		fmt.Printf("  %s: ", label)
	}
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func promptSecret(label string) string {
	fmt.Printf("  %s: ", label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func step(format string, a ...any)  { fmt.Printf("\n  → "+format+"\n", a...) }
func okf(format string, a ...any)   { fmt.Printf("    "+green("✓")+" "+format+"\n", a...) }
func warnf(format string, a ...any) { fmt.Printf("    "+yellow("!")+" "+format+"\n", a...) }

func bold(s string) string   { return "\x1b[1m" + s + "\x1b[0m" }
func faint(s string) string  { return "\x1b[2m" + s + "\x1b[0m" }
func green(s string) string  { return "\x1b[32m" + s + "\x1b[0m" }
func yellow(s string) string { return "\x1b[33m" + s + "\x1b[0m" }
func cmd(s string) string    { return "\x1b[36m" + s + "\x1b[0m" }

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

var _ = time.Second // reserved for future retry/backoff
