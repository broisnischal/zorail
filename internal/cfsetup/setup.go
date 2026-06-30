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

	fmt.Println(bold("\n  zmail setup — connect a real domain to your localhost Zorail\n"))
	fmt.Println("  This points your domain's mail at this machine via Cloudflare Email")
	fmt.Print("  Routing → an Email Worker → a Cloudflare Tunnel → /api/ingest.\n\n")

	// ---- gather inputs ----
	if o.Domain == "" {
		o.Domain = prompt(in, "Mail domain (e.g. example.com)", "")
	}
	o.Domain = strings.ToLower(strings.TrimSpace(o.Domain))
	if o.Domain == "" {
		return fmt.Errorf("a domain is required")
	}
	if o.ServerURL == "" {
		o.ServerURL = prompt(in, "Local Zorail server URL", "http://127.0.0.1:8090")
	}
	if o.Hostname == "" {
		o.Hostname = "ingest." + o.Domain
	}
	if o.EnvFile == "" {
		o.EnvFile = ".env"
	}
	if o.CFToken == "" {
		o.CFToken = os.Getenv("CLOUDFLARE_API_TOKEN")
	}
	if o.CFToken == "" {
		o.CFToken = promptSecret("Cloudflare API token (Zone:Email Routing, Workers, DNS, Tunnel edit)")
	}
	if o.CFToken == "" {
		return fmt.Errorf("a Cloudflare API token is required")
	}

	origin := localOrigin(o.ServerURL)

	cf := NewCF(o.CFToken)

	// ---- 1. verify token + resolve zone ----
	step("Verifying Cloudflare token")
	if err := cf.VerifyToken(ctx); err != nil {
		return err
	}
	zone, err := cf.Zone(ctx, o.Domain)
	if err != nil {
		return err
	}
	// If the mail domain is a subdomain of the zone, route just that subdomain.
	subdomain := ""
	if !strings.EqualFold(o.Domain, zone.Name) {
		subdomain = strings.TrimSuffix(o.Domain, "."+zone.Name)
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

	ingestToken, restartNeeded, err := ensureServerToken(ctx, o, cfg.AuthRequired)
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
	er, err := cf.GetEmailRouting(ctx, zone.ID)
	if err != nil {
		return err
	}
	if !er.Enabled {
		if err := cf.EnableEmailRouting(ctx, zone.ID); err != nil {
			return err
		}
	}
	recs, err := cf.EmailRoutingDNS(ctx, zone.ID, subdomain)
	if err != nil {
		return err
	}
	for _, r := range recs {
		if err := cf.UpsertDNS(ctx, zone.ID, r); err != nil {
			return fmt.Errorf("add routing record %s %s: %w", r.Type, r.Name, err)
		}
	}
	if subdomain != "" {
		okf("MX + SPF records for %s in place (%d)", o.Domain, len(recs))
	} else {
		okf("MX + SPF records in place (%d)", len(recs))
	}

	step("Setting catch-all: *@%s → %s", o.Domain, worker)
	if err := cf.SetCatchAllToWorker(ctx, zone.ID, worker); err != nil {
		return err
	}
	okf("catch-all active")

	// ---- 4. persist state ----
	st := &State{
		Domain: o.Domain, Hostname: o.Hostname, Origin: origin,
		ZoneID: zone.ID, AccountID: zone.Account.ID, TunnelID: tunnel.ID, Worker: worker,
	}
	if err := SaveState(st); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	// ---- 5. the tunnel daemon (must run on this machine) ----
	printTunnelInstructions(tunnel.Token, restartNeeded, o.EnvFile)
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

func printTunnelInstructions(tunnelToken string, restartNeeded bool, envFile string) {
	fmt.Println(bold("\n  ✓ Cloudflare is configured. Two things run on THIS machine:\n"))

	n := 1
	if restartNeeded {
		fmt.Printf("  %d. Restart the Zorail server so it picks up the new token in %s:\n", n, envFile)
		fmt.Print("       (stop the current process, then `make run` or your usual start command)\n\n")
		n++
	}

	have := exec.Command("cloudflared", "--version").Run() == nil
	fmt.Printf("  %d. Run the Cloudflare Tunnel so the Worker can reach localhost.\n", n)
	if !have {
		fmt.Println("     cloudflared isn't installed — get it: https://pkg.cloudflare.com/")
		fmt.Print("     (Arch: `sudo pacman -S cloudflared`)\n\n")
	}
	fmt.Println("     Install it as an always-on service (survives reboots):")
	fmt.Println(cmd("       sudo cloudflared service install " + tunnelToken))
	fmt.Println("\n     …or run it in the foreground to test right now:")
	fmt.Println(cmd("       cloudflared tunnel run --token " + tunnelToken))

	fmt.Println(bold("\n  Then verify end-to-end:"))
	fmt.Println(cmd("       zmail doctor"))
	fmt.Println("\n  Once the tunnel is up and DNS has propagated (a few minutes), send a")
	fmt.Print("  message to anything@your-domain and watch it land in `zmail`.\n\n")
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
