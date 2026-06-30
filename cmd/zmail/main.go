// Command zmail is the terminal companion to a running Zorail server.
//
//	zmail                 live, interactive inbox viewer (TUI)
//	zmail setup           connect a real domain's inbound mail to this server
//	                      (Cloudflare Email Routing → Worker → Tunnel → /api/ingest)
//	zmail doctor          verify the mail pipeline end-to-end
//
// It speaks the same JSON API the web dashboard uses (including the long-poll
// /wait endpoint), so it works against a local or remote server.
//
// Environment:
//
//	ZORAIL_URL            server base URL   (default http://127.0.0.1:8090)
//	ZORAIL_TOKEN          bearer API token  (if the server requires auth)
//	CLOUDFLARE_API_TOKEN  Cloudflare token  (for `setup` / `doctor`)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nees/zorail/internal/cfsetup"
	"github.com/nees/zorail/internal/tui"
)

func main() {
	// Subcommand dispatch: the first non-flag arg selects a mode; default is the TUI.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "setup":
			runSetup(os.Args[2:])
			return
		case "doctor":
			runDoctor(os.Args[2:])
			return
		case "help", "-h", "--help":
			usage()
			return
		}
	}
	runTUI(os.Args[1:])
}

func usage() {
	fmt.Print(`zmail — terminal client for a Zorail server

  zmail            live interactive inbox viewer (default)
  zmail setup      connect a real domain's mail to this server via Cloudflare
  zmail doctor     verify the inbound mail pipeline end-to-end
  zmail help       show this help

Flags (TUI):     --url <base>  --token <tok>
Flags (setup):   --domain <d>  --url <base>  --hostname <h>  --cf-token <t>  --env-file <f>
Environment:     ZORAIL_URL  ZORAIL_TOKEN  CLOUDFLARE_API_TOKEN
`)
}

func runTUI(args []string) {
	fs := flag.NewFlagSet("zmail", flag.ExitOnError)
	url := fs.String("url", envOr("ZORAIL_URL", "http://127.0.0.1:8090"), "Zorail server base URL")
	token := fs.String("token", os.Getenv("ZORAIL_TOKEN"), "bearer API token")
	_ = fs.Parse(args)

	client := tui.NewClient(*url, *token)
	p := tea.NewProgram(tui.New(client), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "zmail:", err)
		os.Exit(1)
	}
}

func runSetup(args []string) {
	fs := flag.NewFlagSet("zmail setup", flag.ExitOnError)
	o := cfsetup.Options{}
	fs.StringVar(&o.Domain, "domain", "", "mail domain (the Cloudflare zone)")
	fs.StringVar(&o.ServerURL, "url", envOr("ZORAIL_URL", "http://127.0.0.1:8090"), "local Zorail server URL")
	fs.StringVar(&o.Hostname, "hostname", "", "public ingress hostname (default ingest.<domain>)")
	fs.StringVar(&o.CFToken, "cf-token", "", "Cloudflare API token (or $CLOUDFLARE_API_TOKEN)")
	fs.StringVar(&o.EnvFile, "env-file", ".env", "server dotenv file for ZORAIL_API_TOKEN")
	_ = fs.Parse(args)

	if err := cfsetup.Run(rootCtx(), o); err != nil {
		fmt.Fprintln(os.Stderr, "\n  "+errStyle("setup failed:"), err)
		os.Exit(1)
	}
}

func runDoctor(args []string) {
	fs := flag.NewFlagSet("zmail doctor", flag.ExitOnError)
	o := cfsetup.Options{}
	fs.StringVar(&o.ServerURL, "url", envOr("ZORAIL_URL", "http://127.0.0.1:8090"), "local Zorail server URL")
	fs.StringVar(&o.CFToken, "cf-token", "", "Cloudflare API token (or $CLOUDFLARE_API_TOKEN)")
	fs.StringVar(&o.EnvFile, "env-file", ".env", "server dotenv file for ZORAIL_API_TOKEN")
	_ = fs.Parse(args)

	if err := cfsetup.Doctor(rootCtx(), o); err != nil {
		fmt.Fprintln(os.Stderr, "\n  "+errStyle("doctor:"), err)
		os.Exit(1)
	}
}

// rootCtx cancels on SIGINT/SIGTERM so long Cloudflare calls abort cleanly.
func rootCtx() context.Context {
	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	return ctx
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func errStyle(s string) string { return "\x1b[31m" + s + "\x1b[0m" }
