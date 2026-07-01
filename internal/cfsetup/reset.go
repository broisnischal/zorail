package cfsetup

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ResetOptions controls what `zorail reset` removes.
type ResetOptions struct {
	EnvFile string // dotenv config to remove (default: repo-root .env)
	DBPath  string // sqlite database path (default: $ZORAIL_DB_PATH or zorail.db)
	Force   bool   // skip the confirmation prompt
}

// Reset wipes local Zorail state so you can start over from a clean slate: the
// SQLite database (plus its -wal/-shm sidecars), the saved setup-state file, and
// the generated .env config. The .env is moved to .env.bak (it holds tokens you
// may want back), everything else is deleted.
//
// It deliberately does NOT touch Cloudflare — the tunnel, Worker, DNS records,
// and Email Routing rules stay in place. Remove those from the Cloudflare
// dashboard if you want a full teardown.
func Reset(in *bufio.Reader, o ResetOptions) error {
	if o.EnvFile == "" {
		o.EnvFile = RepoEnvFile()
	}
	if o.DBPath == "" {
		o.DBPath = firstNonEmpty(os.Getenv("ZORAIL_DB_PATH"),
			firstNonEmpty(readEnvValue(o.EnvFile, "ZORAIL_DB_PATH"), "zorail.db"))
	}

	type target struct{ label, path string }
	var targets []target
	for _, p := range []string{o.DBPath, o.DBPath + "-wal", o.DBPath + "-shm"} {
		if regularFileExists(p) {
			targets = append(targets, target{"database", p})
		}
	}
	if regularFileExists(o.EnvFile) {
		targets = append(targets, target{"config", o.EnvFile})
	}
	if sp, err := statePath(); err == nil && regularFileExists(sp) {
		targets = append(targets, target{"setup state", sp})
	}

	if len(targets) == 0 {
		fmt.Println("  nothing to remove — no local database, config, or setup state found.")
		return nil
	}

	fmt.Println(bold("\n  zorail reset — this will remove:"))
	for _, t := range targets {
		fmt.Printf("    • %-12s %s\n", t.label, t.path)
	}
	fmt.Println(faint("\n  Cloudflare (tunnel, Worker, DNS, Email Routing) is NOT touched."))
	fmt.Println(faint("  Stop any running server first, or it keeps using the open (deleted) database."))

	if !o.Force {
		fmt.Print("\n  Type " + bold("yes") + " to confirm: ")
		ans, _ := in.ReadString('\n')
		if strings.TrimSpace(ans) != "yes" {
			fmt.Println("  aborted — nothing removed.")
			return nil
		}
	}

	for _, t := range targets {
		if t.label == "config" {
			bak := t.path + ".bak"
			if err := os.Rename(t.path, bak); err != nil {
				return fmt.Errorf("back up %s: %w", t.path, err)
			}
			okf("config backed up to %s", bak)
			continue
		}
		if err := os.Remove(t.path); err != nil {
			return fmt.Errorf("remove %s: %w", t.path, err)
		}
		okf("removed %s", t.path)
	}
	fmt.Println(bold("\n  ✓ reset complete.") + " Run " + cmd("zorail setup") + " to start fresh.")
	fmt.Println()
	return nil
}

func regularFileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
