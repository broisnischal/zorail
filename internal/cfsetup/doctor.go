package cfsetup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// Doctor verifies an existing setup end-to-end and reports what's healthy and
// what still needs attention.
func Doctor(ctx context.Context, o Options) error {
	st, err := LoadState()
	if err != nil {
		return fmt.Errorf("no saved setup found — run `zorail setup` first (%v)", err)
	}
	fmt.Printf("%s\n  domain %s · ingress %s · worker %s\n\n",
		bold("  zorail doctor"), st.Domain, st.Hostname, st.Worker)

	if o.EnvFile == "" {
		o.EnvFile = RepoEnvFile()
	}
	if o.ServerURL == "" {
		o.ServerURL = "http://127.0.0.1:8090"
	}
	if o.CFToken == "" {
		o.CFToken = os.Getenv("CLOUDFLARE_API_TOKEN")
	}
	if o.CFToken == "" {
		o.CFToken = promptSecret("Cloudflare API token")
	}

	var problems int
	chk := func(name string, fn func() (string, error)) {
		detail, err := fn()
		if err != nil {
			problems++
			fmt.Printf("    %s %s — %s\n", red("✗"), name, err)
			return
		}
		fmt.Printf("    %s %s%s\n", green("✓"), name, dim(detail))
	}

	cf := NewCF(o.CFToken)

	chk("Cloudflare token", func() (string, error) { return "", cf.VerifyToken(ctx) })

	chk("Email Routing enabled", func() (string, error) {
		if er, err := cf.GetEmailRouting(ctx, st.ZoneID); err == nil {
			if !er.Enabled {
				return "", fmt.Errorf("not enabled (status %q)", er.Status)
			}
			return "  status " + er.Status, nil
		}
		// GET /email/routing rejects scoped API tokens (error 10000), so fall
		// back to confirming the Cloudflare MX records exist — proof that
		// routing DNS is in place for the domain.
		recs, err := cf.ListDNS(ctx, st.ZoneID)
		if err != nil {
			return "", err
		}
		for _, r := range recs {
			if strings.EqualFold(r.Type, "MX") && strings.EqualFold(r.Name, st.Domain) &&
				strings.HasSuffix(r.Content, ".mx.cloudflare.net") {
				return "  MX records present (status flag unreadable via API token)", nil
			}
		}
		return "", fmt.Errorf("no Cloudflare MX records for %s — enable Email Routing in the dashboard", st.Domain)
	})

	chk("Catch-all → Worker", func() (string, error) {
		w, enabled, err := cf.CatchAllWorker(ctx, st.ZoneID)
		if err != nil {
			return "", err
		}
		if w == "" {
			return "", fmt.Errorf("catch-all does not route to a Worker")
		}
		if !enabled {
			return "", fmt.Errorf("catch-all rule is disabled (→ %s)", w)
		}
		return "  → " + w, nil
	})

	chk("Worker deployed", func() (string, error) {
		if !cf.WorkerExists(ctx, st.AccountID, st.Worker) {
			return "", fmt.Errorf("script %q not found", st.Worker)
		}
		return "  " + st.Worker, nil
	})

	chk("Tunnel running", func() (string, error) {
		healthy, err := cf.TunnelHealthy(ctx, st.AccountID, st.TunnelID)
		if err != nil {
			return "", err
		}
		if !healthy {
			return "", fmt.Errorf("no active connections — start cloudflared (see `zorail setup`)")
		}
		return "  active", nil
	})

	z := newZorail(o.ServerURL, "")
	cfg, cfgErr := z.config(ctx)
	chk("Local server reachable", func() (string, error) {
		if cfgErr != nil {
			return "", cfgErr
		}
		if !cfg.AuthRequired {
			return "", fmt.Errorf("server is in OPEN mode — set ZORAIL_API_TOKEN and restart")
		}
		return "  " + o.ServerURL, nil
	})

	// End-to-end: push a unique probe through the public ingress and confirm it
	// lands locally. This exercises Cloudflare edge → Tunnel → localhost → ingest.
	token := firstNonEmpty(os.Getenv("ZORAIL_TOKEN"), readEnvValue(o.EnvFile, "ZORAIL_API_TOKEN"))
	chk("End-to-end ingest probe", func() (string, error) {
		if token == "" {
			return "", fmt.Errorf("no server token available ($ZORAIL_TOKEN / %s) to authenticate the probe", o.EnvFile)
		}
		probe := fmt.Sprintf("zorail-doctor-%d@%s", time.Now().Unix(), st.Domain)
		before, _ := z2(o.ServerURL, token).messageCount(ctx, probe)
		raw := buildProbe(probe)
		ingestURL := "https://" + st.Hostname + "/api/ingest"
		if err := postIngest(ctx, ingestURL, token, probe, "doctor@"+st.Domain, raw); err != nil {
			return "", fmt.Errorf("POST %s failed: %w", ingestURL, err)
		}
		// Give the store a moment.
		var after int
		for i := 0; i < 10; i++ {
			after, _ = z2(o.ServerURL, token).messageCount(ctx, probe)
			if after > before {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
		if after <= before {
			return "", fmt.Errorf("probe accepted by ingress but did not appear in %s", probe)
		}
		return "  delivered to " + probe, nil
	})

	fmt.Println()
	if problems == 0 {
		fmt.Printf("  %s everything is healthy. Mail to *@%s will land in your inbox.\n\n", green("✓"), st.Domain)
		return nil
	}
	fmt.Printf("  %s %d check(s) need attention (see above).\n\n", yellow("!"), problems)
	return fmt.Errorf("%d checks failed", problems)
}

func z2(base, token string) *zorail { return newZorail(base, token) }

func buildProbe(rcpt string) []byte {
	return []byte("From: zorail doctor <doctor@localhost>\r\n" +
		"To: " + rcpt + "\r\n" +
		"Subject: zorail doctor probe\r\n" +
		"Message-ID: <" + fmt.Sprint(time.Now().UnixNano()) + "@zorail.doctor>\r\n" +
		"\r\n" +
		"This is an automated connectivity probe from `zorail doctor`.\r\n")
}

func red(s string) string { return "\x1b[31m" + s + "\x1b[0m" }
func dim(s string) string {
	if s == "" {
		return ""
	}
	return "\x1b[2m" + s + "\x1b[0m"
}
