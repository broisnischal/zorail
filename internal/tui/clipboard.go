package tui

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/atotto/clipboard"
)

// copyToClipboard copies s to the clipboard using two mechanisms so it works
// both locally and over SSH/tmux:
//
//   - the OS clipboard via atotto/clipboard (pbcopy / xclip / wl-copy), and
//   - an OSC52 terminal escape, which reaches the *local* clipboard even when
//     the viewer runs on a remote host through SSH.
//
// OSC52 is an invisible control sequence, so writing it to stdout under the
// alt-screen does not disturb the rendered UI. It returns true if either path
// was attempted without error.
func copyToClipboard(s string) bool {
	ok := clipboard.WriteAll(s) == nil

	enc := base64.StdEncoding.EncodeToString([]byte(s))
	if _, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", enc); err == nil {
		ok = true
	}
	return ok
}
