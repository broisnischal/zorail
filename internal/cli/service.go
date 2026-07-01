package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// runService prints a ready-to-use systemd unit (and the exact install
// commands) that runs Zorail as a background daemon which starts on boot and
// restarts on failure. It only prints — it never touches system files — so it
// is safe to run and easy to review before installing.
func runService(args []string) int {
	fs := flag.NewFlagSet("zorail service", flag.ExitOnError)
	mode := fs.String("mode", "up", `what to run: "up" (server + Cloudflare Tunnel) or "serve" (server only)`)
	user := fs.Bool("user", false, "generate a per-user service (no sudo, ~/.config/systemd/user) instead of system-wide")
	_ = fs.Parse(args)

	if *mode != "up" && *mode != "serve" {
		fmt.Fprintf(os.Stderr, "zorail service: --mode must be \"up\" or \"serve\", got %q\n", *mode)
		return 2
	}

	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "/usr/local/bin/zorail"
	} else if abs, aerr := filepath.Abs(exe); aerr == nil {
		exe = abs
	}
	wd, _ := os.Getwd()
	if wd == "" {
		wd = filepath.Dir(exe)
	}

	fmt.Print(serviceUnit(exe, wd, *mode, *user))
	return 0
}

// serviceUnit renders the unit file plus install instructions for the given
// binary path, working directory (where .env lives), run mode, and scope.
func serviceUnit(exe, workdir, mode string, user bool) string {
	// A system service can bind privileged ports (25) via a capability and starts
	// at boot; a user service needs no sudo but only runs while the user is
	// logged in (unless lingering is enabled) and cannot bind port 25.
	unit := "[Unit]\n" +
		"Description=Zorail self-hosted disposable mail server\n" +
		"After=network-online.target\n" +
		"Wants=network-online.target\n\n" +
		"[Service]\n" +
		"Type=simple\n" +
		"WorkingDirectory=" + workdir + "\n" +
		"ExecStart=" + exe + " " + mode + "\n" +
		"Restart=on-failure\n" +
		"RestartSec=3\n"
	if !user {
		// Run as the invoking user, and allow binding low ports (e.g. SMTP :25)
		// without full root.
		u := os.Getenv("USER")
		if u == "" {
			u = "youruser"
		}
		unit += "User=" + u + "\n" +
			"AmbientCapabilities=CAP_NET_BIND_SERVICE\n"
	}
	unit += "\n[Install]\nWantedBy=" + wantedBy(user) + "\n"

	var b string
	b += "# ---- zorail systemd service (" + scopeLabel(user) + ", mode: " + mode + ") ----\n"
	if mode == "up" {
		b += "# NOTE: `up` also starts the Cloudflare Tunnel, so cloudflared must be\n" +
			"#       installed and `zorail setup` must have been run (it needs the saved\n" +
			"#       tunnel token). For the local server only, use --mode serve.\n"
	}
	b += "\n" + unit + "\n"

	b += "# ---- install (copy-paste) ----\n"
	if user {
		b += "mkdir -p ~/.config/systemd/user\n" +
			exe + " service --user --mode " + mode + " > ~/.config/systemd/user/zorail.service\n" +
			"systemctl --user daemon-reload\n" +
			"systemctl --user enable --now zorail\n" +
			"loginctl enable-linger \"$USER\"   # keep running after logout / across reboots\n" +
			"\n# logs:    journalctl --user -u zorail -f\n" +
			"# stop:    systemctl --user stop zorail\n"
	} else {
		b += exe + " service --mode " + mode + " | sudo tee /etc/systemd/system/zorail.service >/dev/null\n" +
			"sudo systemctl daemon-reload\n" +
			"sudo systemctl enable --now zorail\n" +
			"\n# logs:    journalctl -u zorail -f\n" +
			"# stop:    sudo systemctl stop zorail\n"
	}
	return b
}

func wantedBy(user bool) string {
	if user {
		return "default.target"
	}
	return "multi-user.target"
}

func scopeLabel(user bool) string {
	if user {
		return "user service"
	}
	return "system service"
}
