// Command zorail is the self-hosted disposable + reserved + forwarding mail
// server, bundled with its operator tooling. With no subcommand it runs the
// all-in-one server (inbound SMTP ingest, JSON API, embedded web UI, MCP server
// for agents, forwarding worker, retention sweeper). Subcommands add domain
// setup, a server+tunnel supervisor, a health check, and a terminal inbox
// viewer — see `zorail help`. All commands share one binary, one config, one
// SQLite file.
package main

import (
	"os"

	"github.com/nees/zorail/internal/cli"
)

func main() { os.Exit(cli.Main()) }
