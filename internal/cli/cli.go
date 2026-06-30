// Package cli is the single command-line entrypoint for the `zorail` binary. It
// bundles the all-in-one server with the operator tooling and the terminal
// inbox viewer behind one set of subcommands:
//
//	zorail [serve]     run the all-in-one server (SMTP + API + UI + MCP)
//	zorail setup       connect a real domain's mail via Cloudflare
//	zorail up          run the server + Cloudflare Tunnel together
//	zorail doctor      verify the inbound mail pipeline end-to-end
//	zorail watch       live, interactive inbox viewer (TUI)
//
// With no subcommand it runs the server — everything in one process, no Docker
// required — which also keeps the container entrypoint (`zorail`) working.
package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nees/zorail/internal/cfsetup"
	"github.com/nees/zorail/internal/tui"
)

// Main parses os.Args, dispatches to a subcommand, and returns a process exit
// code.
func Main() int {
	args := os.Args[1:]

	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		usage()
		return 0
	}

	// A leading non-flag word selects a subcommand; with no args (or only
	// flags) we run the all-in-one server.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "serve", "server":
			return code(serve())
		case "setup":
			return runSetup(args[1:])
		case "up":
			return runUp(args[1:])
		case "doctor":
			return runDoctor(args[1:])
		case "watch", "tui":
			return runTUI(args[1:])
		default:
			fmt.Fprintf(os.Stderr, "zorail: unknown command %q\n\n", args[0])
			usage()
			return 2
		}
	}

	return code(serve())
}

func usage() {
	fmt.Print(`zorail — self-hosted disposable mail server + tooling

  zorail            run the all-in-one server (SMTP + JSON API + web UI + MCP)
  zorail serve      same as above, explicit
  zorail setup      connect a real domain's mail to this server via Cloudflare
  zorail up         run the server + Cloudflare Tunnel together (after setup)
  zorail doctor     verify the inbound mail pipeline end-to-end
  zorail watch      live interactive inbox viewer (TUI)
  zorail help       show this help

Flags (watch):   --url <base>  --token <tok>
Flags (setup):   --domain <d>  --url <base>  --hostname <h>  --cf-token <t>  --env-file <f>
Flags (up):      --cf-token <t>  --env-file <f>
Server config:   via environment / .env (ZORAIL_DOMAIN, ZORAIL_API_TOKEN, ZORAIL_HTTP_ADDR, …)
Environment:     ZORAIL_URL  ZORAIL_TOKEN  CLOUDFLARE_API_TOKEN
`)
}

func runTUI(args []string) int {
	fs := flag.NewFlagSet("zorail watch", flag.ExitOnError)
	url := fs.String("url", envOr("ZORAIL_URL", "http://127.0.0.1:8090"), "Zorail server base URL")
	token := fs.String("token", os.Getenv("ZORAIL_TOKEN"), "bearer API token")
	_ = fs.Parse(args)

	client := tui.NewClient(*url, *token)
	p := tea.NewProgram(tui.New(client), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "zorail watch:", err)
		return 1
	}
	return 0
}

func runSetup(args []string) int {
	fs := flag.NewFlagSet("zorail setup", flag.ExitOnError)
	o := cfsetup.Options{}
	fs.StringVar(&o.Domain, "domain", "", "mail domain (the Cloudflare zone)")
	fs.StringVar(&o.ServerURL, "url", envOr("ZORAIL_URL", "http://127.0.0.1:8090"), "local Zorail server URL")
	fs.StringVar(&o.Hostname, "hostname", "", "public ingress hostname (default ingest.<domain>)")
	fs.StringVar(&o.CFToken, "cf-token", "", "Cloudflare API token (or $CLOUDFLARE_API_TOKEN)")
	fs.StringVar(&o.EnvFile, "env-file", "", "server dotenv file (default: repo-root .env)")
	_ = fs.Parse(args)

	if err := cfsetup.Run(rootCtx(), o); err != nil {
		fmt.Fprintln(os.Stderr, "\n  "+errStyle("setup failed:"), err)
		return 1
	}
	return 0
}

func runUp(args []string) int {
	fs := flag.NewFlagSet("zorail up", flag.ExitOnError)
	o := cfsetup.Options{}
	fs.StringVar(&o.CFToken, "cf-token", "", "Cloudflare API token (only needed if the tunnel token isn't saved)")
	fs.StringVar(&o.EnvFile, "env-file", "", "dotenv file to load (default: repo-root .env)")
	_ = fs.Parse(args)

	if err := cfsetup.Up(rootCtx(), o); err != nil {
		fmt.Fprintln(os.Stderr, "\n  "+errStyle("up:"), err)
		return 1
	}
	return 0
}

func runDoctor(args []string) int {
	fs := flag.NewFlagSet("zorail doctor", flag.ExitOnError)
	o := cfsetup.Options{}
	fs.StringVar(&o.ServerURL, "url", envOr("ZORAIL_URL", "http://127.0.0.1:8090"), "local Zorail server URL")
	fs.StringVar(&o.CFToken, "cf-token", "", "Cloudflare API token (or $CLOUDFLARE_API_TOKEN)")
	fs.StringVar(&o.EnvFile, "env-file", "", "server dotenv file (default: repo-root .env)")
	_ = fs.Parse(args)

	if err := cfsetup.Doctor(rootCtx(), o); err != nil {
		fmt.Fprintln(os.Stderr, "\n  "+errStyle("doctor:"), err)
		return 1
	}
	return 0
}

// rootCtx cancels on SIGINT/SIGTERM so long Cloudflare calls abort cleanly.
func rootCtx() context.Context {
	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	return ctx
}

// code maps an error to a process exit code, printing it to stderr.
func code(err error) int {
	if err != nil {
		fmt.Fprintln(os.Stderr, "zorail:", err)
		return 1
	}
	return 0
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func errStyle(s string) string { return "\x1b[31m" + s + "\x1b[0m" }
