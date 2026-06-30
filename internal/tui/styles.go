package tui

import "github.com/charmbracelet/lipgloss"

// theme holds the colors and reusable styles. It stays near-monochrome — a
// neutral gray scale with a single quiet "live" green and a red reserved for
// danger — mirroring the web dashboard's restrained palette. Colors are
// AdaptiveColor so the TUI reads well on both light and dark terminals.
type theme struct {
	fg, muted, subtle lipgloss.AdaptiveColor
	border, borderHi  lipgloss.AdaptiveColor
	accent, onAccent  lipgloss.AdaptiveColor
	live, danger      lipgloss.AdaptiveColor

	// component styles
	brand, alpha                      lipgloss.Style
	colTitle, colCount                lipgloss.Style
	rowKey, rowSub, rowTime           lipgloss.Style
	rowSel, rowSelBar                 lipgloss.Style
	rowUnread, dot                    lipgloss.Style
	paneFocused, paneBlurred          lipgloss.Style
	subject, metaKey, metaVal, metaNm lipgloss.Style
	code, link, badge, badgeBad       lipgloss.Style
	status, statusKey, statusOK, help lipgloss.Style
	primary, faint                    lipgloss.Style
}

func newTheme() theme {
	t := theme{
		fg:       lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#fafafa"},
		muted:    lipgloss.AdaptiveColor{Light: "#6b6b6b", Dark: "#a1a1a1"},
		subtle:   lipgloss.AdaptiveColor{Light: "#999999", Dark: "#6e6e6e"},
		border:   lipgloss.AdaptiveColor{Light: "#d4d4d4", Dark: "#333333"},
		borderHi: lipgloss.AdaptiveColor{Light: "#a3a3a3", Dark: "#525252"},
		accent:   lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#fafafa"},
		onAccent: lipgloss.AdaptiveColor{Light: "#fafafa", Dark: "#171717"},
		live:     lipgloss.AdaptiveColor{Light: "#16a34a", Dark: "#4ade80"},
		danger:   lipgloss.AdaptiveColor{Light: "#dc2626", Dark: "#f87171"},
	}

	t.brand = lipgloss.NewStyle().Foreground(t.fg).Bold(true)
	t.alpha = lipgloss.NewStyle().Foreground(t.subtle).
		Border(lipgloss.NormalBorder(), false).Padding(0, 1).
		Background(lipgloss.AdaptiveColor{Light: "#eee", Dark: "#262626"})
	t.faint = lipgloss.NewStyle().Foreground(t.subtle)

	t.colTitle = lipgloss.NewStyle().Foreground(t.subtle).Bold(true)
	t.colCount = lipgloss.NewStyle().Foreground(t.subtle)

	t.rowKey = lipgloss.NewStyle().Foreground(t.muted)
	t.rowSub = lipgloss.NewStyle().Foreground(t.subtle)
	t.rowTime = lipgloss.NewStyle().Foreground(t.subtle)
	t.rowSel = lipgloss.NewStyle().Foreground(t.fg)
	t.rowSelBar = lipgloss.NewStyle().Foreground(t.fg)
	t.rowUnread = lipgloss.NewStyle().Foreground(t.fg).Bold(true)
	t.dot = lipgloss.NewStyle().Foreground(t.live)

	t.paneFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.borderHi)
	t.paneBlurred = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.border)

	t.subject = lipgloss.NewStyle().Foreground(t.fg).Bold(true)
	t.metaKey = lipgloss.NewStyle().Foreground(t.subtle)
	t.metaVal = lipgloss.NewStyle().Foreground(t.muted)
	t.metaNm = lipgloss.NewStyle().Foreground(t.fg)
	t.code = lipgloss.NewStyle().Foreground(t.onAccent).Background(t.accent).Bold(true).Padding(0, 1)
	t.link = lipgloss.NewStyle().Foreground(t.muted).Underline(true)
	t.badge = lipgloss.NewStyle().Foreground(t.muted).Border(lipgloss.RoundedBorder()).Padding(0, 1)
	t.badgeBad = lipgloss.NewStyle().Foreground(t.danger).Border(lipgloss.RoundedBorder()).Padding(0, 1)

	t.status = lipgloss.NewStyle().Foreground(t.muted)
	t.statusKey = lipgloss.NewStyle().Foreground(t.fg).Bold(true)
	t.statusOK = lipgloss.NewStyle().Foreground(t.live)
	t.help = lipgloss.NewStyle().Foreground(t.subtle)

	t.primary = lipgloss.NewStyle().Foreground(t.onAccent).Background(t.accent).Bold(true).Padding(0, 1)
	return t
}

// kbd renders a "key label" help hint, e.g. "j/k move".
func (t theme) kbd(key, label string) string {
	return t.statusKey.Render(key) + " " + t.help.Render(label)
}
