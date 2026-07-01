package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// openURL opens a URL or local file path in the user's default handler
// (browser, image viewer, etc.). It returns quickly — the child process is
// started detached, not waited on.
func openURL(target string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default: // linux, *bsd
		name = "xdg-open"
	}
	args = append(args, target)
	return exec.Command(name, args...).Start()
}

// saveTemp writes b to a temp file named after filename (sanitized) and returns
// its path, so an attachment can be handed to the OS's default app.
func saveTemp(filename string, b []byte) (string, error) {
	name := filepath.Base(strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(filename))
	if name == "" || name == "." {
		name = "attachment"
	}
	dir, err := os.MkdirTemp("", "zorail-att-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
