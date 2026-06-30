package cfsetup

import (
	"bufio"
	"context"
	_ "embed"
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

	// ---- 2. ensure the server requires auth (the tunnel makes ingest public) ----
	step("Checking the local Zorail server")
	z := newZorail(o.ServerURL, "")
	cfg, err := z.config(ctx)
	if err != nil {
		return fmt.Errorf("cannot reach Zorail at %s: %w", o.ServerURL, err)
	}
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
		return err
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
			warnf("enable it once in the dashboard (zone %s → Email → Email Routing), then mail will flow", zone.Name)
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
		return err
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
		// Server already locked down; we need its existing admin token.
		token = firstNonEmpty(os.Getenv("ZORAIL_TOKEN"), readEnvValue(o.EnvFile, "ZORAIL_API_TOKEN"))
		if token == "" {
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
	fmt.Println("  Opening a " + bold("pre-filled") + " token page — just review and click " + bold("Create Token") + ", then copy it.")
	fmt.Println("  " + cmd(tokenURL))
	fmt.Println("\n  Confirm these permissions are selected (the link pre-fills them):")
	fmt.Println("    • Account → Cloudflare Tunnel : Edit")
	fmt.Println("    • Account → Workers Scripts : Edit")
	fmt.Println("    • Account → Email Routing Addresses : Read")
	fmt.Println("    • Zone    → DNS : Edit")
	fmt.Println("    • Zone    → Email Routing Rules : Edit")
	fmt.Println("    • Zone    → Zone : Read")
	fmt.Println("  Zone Resources: All zones (or your specific zone).")
	openBrowser(tokenURL)
	return promptSecret("Paste the API token")
}

// cfTokenTemplateURL builds a Cloudflare dashboard link that opens the
// create-token page with exactly the permissions Zorail needs pre-selected and
// the token name filled in. See:
// https://developers.cloudflare.com/fundamentals/api/how-to/account-owned-token-template/
func cfTokenTemplateURL() string {
	const perms = `[` +
		`{"key":"argo_tunnel","type":"edit"},` + // Account · Cloudflare Tunnel
		`{"key":"workers_scripts","type":"edit"},` + // Account · Workers Scripts
		`{"key":"email_routing_addresses","type":"read"},` + // Account · Email Routing Addresses
		`{"key":"dns","type":"edit"},` + // Zone · DNS
		`{"key":"email_routing_rules","type":"edit"},` + // Zone · Email Routing Rules
		`{"key":"zone","type":"read"}` + // Zone · Zone
		`]`
	q := url.Values{}
	q.Set("to", "/:account/api-tokens")
	q.Set("permissionGroupKeys", perms)
	q.Set("accountId", "*")
	q.Set("zoneId", "all")
	q.Set("name", "Zorail mail setup")
	return "https://dash.cloudflare.com/?" + q.Encode()
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
