package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// paneDims holds the computed outer widths of each pane for the current size
// and reading state. A pane is hidden when its width is 0.
type paneDims struct {
	leftW, msgW, readerW int
	bodyH                int
	threePane            bool
}

func (m Model) dims() paneDims {
	d := paneDims{bodyH: max(3, m.h-2)} // header + status take 2 lines

	d.leftW = 30
	if m.w < 84 {
		d.leftW = max(22, m.w/3)
	}
	rightW := m.w - d.leftW

	d.threePane = m.w >= 104 && m.current != nil
	switch {
	case d.threePane:
		d.msgW = max(38, rightW*42/100)
		d.readerW = rightW - d.msgW
	case m.current != nil && m.focus == focusReader:
		d.readerW = rightW
	default:
		d.msgW = rightW
	}
	return d
}

func (m *Model) resize() {
	d := m.dims()
	w := d.readerW - 4
	h := d.bodyH - 2
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	m.reader.Width = w
	m.reader.Height = h
	if m.current != nil {
		m.reader.SetContent(m.renderMessage(w))
	}
}

func (m *Model) setReaderContent() {
	m.resize()
	m.reader.GotoTop()
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "\n  starting…"
	}

	d := m.dims()
	innerH := d.bodyH - 2

	var panes []string
	panes = append(panes, m.box(m.th.paneFocused, m.focus == focusInboxes, d.leftW, innerH, m.renderInboxes(d.leftW-4, innerH)))

	if d.msgW > 0 {
		panes = append(panes, m.box(m.th.paneFocused, m.focus == focusMessages, d.msgW, innerH, m.renderMessages(d.msgW-4, innerH)))
	}
	if d.readerW > 0 {
		panes = append(panes, m.box(m.th.paneFocused, m.focus == focusReader, d.readerW, innerH, m.reader.View()))
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, panes...)
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderStatus())
}

// box wraps content in a rounded border, bright when focused.
//
// Lipgloss width math: Width(n).Padding(0,1).Border() renders to a total of
// n+2 columns (border sits outside Width) with an inner text area of n-2
// (horizontal padding sits inside Width). We want a total of outerW with a
// text area of outerW-4 (which is what the render* helpers fill), so the
// Width must be outerW-2. Likewise Height(h) yields h+2 total rows.
func (m Model) box(_ lipgloss.Style, focused bool, outerW, innerH int, content string) string {
	st := m.th.paneBlurred
	if focused {
		st = m.th.paneFocused
	}
	return st.Width(outerW-2).Height(innerH).Padding(0, 1).Render(content)
}

func (m Model) renderHeader() string {
	th := m.th
	brand := th.brand.Render("zorail") + " " + th.faint.Render("inbox")

	dotColor := th.subtle
	label := "connecting…"
	if m.live {
		dotColor = th.live
		label = "live"
	}
	live := lipgloss.NewStyle().Foreground(dotColor).Render("●") + " " + th.faint.Render(label)

	left := brand + th.faint.Render("  ·  ") + live
	right := th.faint.Render(fmt.Sprintf("%d inboxes", len(m.inboxes)))
	if m.cfg.Domain != "" {
		right += th.faint.Render("  ·  @" + m.cfg.Domain)
	}

	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return lipgloss.NewStyle().Padding(0, 1).Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) renderInboxes(innerW, innerH int) string {
	th := m.th
	title := th.colTitle.Render("INBOXES") + "  " + th.colCount.Render(fmt.Sprintf("%d", len(m.inboxes)))
	lines := []string{padVis(title, innerW), ""}

	listH := innerH - len(lines)
	if len(m.inboxes) == 0 {
		lines = append(lines, th.faint.Render(padVis("no inboxes yet", innerW)))
		lines = append(lines, "")
		lines = append(lines, th.faint.Render("press "+th.statusKey.Render("g")+th.help.Render(" to generate")))
		return strings.Join(padTo(lines, innerH, innerW), "\n")
	}

	blocks := make([][]string, len(m.inboxes))
	for i, ib := range m.inboxes {
		sel := i == m.inboxIdx
		blocks[i] = m.twoLineRow(innerW, sel, m.focus == focusInboxes,
			ib.Inbox, relTime(ib.LastReceived),
			fmt.Sprintf("%d msg", ib.MessageCount))
	}
	lines = append(lines, windowBlocks(blocks, m.inboxIdx, listH)...)
	return strings.Join(padTo(lines, innerH, innerW), "\n")
}

func (m Model) renderMessages(innerW, innerH int) string {
	th := m.th

	var head string
	if m.mode == "search" {
		m.search.Width = innerW - lipgloss.Width(m.search.Prompt) - 2
		head = m.search.View()
	} else if m.inbox != "" {
		head = th.colTitle.Render("INBOX") + " " + th.metaNm.Render(truncate(m.inbox, innerW-7))
	} else {
		head = th.colTitle.Render("MESSAGES")
	}
	lines := []string{padVis(head, innerW), ""}
	listH := innerH - len(lines)

	if len(m.messages) == 0 {
		empty := "this inbox is empty"
		if m.inbox == "" {
			empty = "select an inbox"
		}
		if m.mode == "search" {
			empty = "type to search · enter"
		}
		lines = append(lines, th.faint.Render(padVis(empty, innerW)))
		return strings.Join(padTo(lines, innerH, innerW), "\n")
	}

	blocks := make([][]string, len(m.messages))
	for i, msg := range m.messages {
		sel := i == m.msgIdx
		name, email := parseFrom(firstNonEmpty(msg.From, msg.EnvFrom))
		who := firstNonEmpty(name, email)
		if who == "" {
			who = "(unknown)"
		}
		sub := firstNonEmpty(msg.Subject, "(no subject)")
		// In search results, show which inbox each hit belongs to.
		if m.mode == "search" {
			sub = sub + "  " + msg.Inbox
		}
		unread := !m.read[msg.ID]
		blocks[i] = m.twoLineRowUnread(innerW, sel, m.focus == focusMessages, unread, who, relTime(msg.ReceivedAt), sub)
	}
	lines = append(lines, windowBlocks(blocks, m.msgIdx, listH)...)
	return strings.Join(padTo(lines, innerH, innerW), "\n")
}

// renderMessage builds the scrollable reader body for the open message.
func (m Model) renderMessage(w int) string {
	if m.current == nil {
		return ""
	}
	th := m.th
	msg := m.current
	wrap := lipgloss.NewStyle().Width(w)
	var b strings.Builder

	b.WriteString(wrap.Foreground(th.fg).Bold(true).Render(firstNonEmpty(msg.Subject, "(no subject)")))
	b.WriteString("\n\n")

	name, email := parseFrom(firstNonEmpty(msg.From, msg.EnvFrom))
	meta := func(k, v string) {
		b.WriteString(th.metaKey.Render(fmt.Sprintf("%-5s", k)) + " " + wrap.Foreground(th.muted).Render(v) + "\n")
	}
	from := email
	if name != "" {
		from = name + "  " + email
	}
	meta("from", from)
	meta("to", strings.Join(msg.To, ", "))
	meta("date", msg.ReceivedAt.Local().Format("Jan 2 15:04")+" · "+relTime(msg.ReceivedAt)+" ago")

	if msg.Spam.Label != "" && msg.Spam.Label != "none" && msg.Spam.Label != "clean" {
		b.WriteString(th.badgeBad.Render(fmt.Sprintf("spam %d · %s", msg.Spam.Score, msg.Spam.Label)) + "\n")
	}

	if len(msg.Extracted.Codes) > 0 {
		b.WriteString("\n" + th.colTitle.Render("CODES") + "\n")
		for _, c := range msg.Extracted.Codes {
			b.WriteString(th.code.Render(c) + " ")
		}
		b.WriteString("\n")
	}
	if len(msg.Extracted.Links) > 0 {
		b.WriteString("\n" + th.colTitle.Render("LINKS") + "\n")
		seen := map[string]bool{}
		for _, l := range msg.Extracted.Links {
			h := hostOf(l)
			if seen[h] {
				continue
			}
			seen[h] = true
			b.WriteString(th.link.Render(h) + "  ")
		}
		b.WriteString("\n")
	}

	body := msg.Text
	if strings.TrimSpace(body) == "" && msg.HTML != "" {
		body = htmlToText(msg.HTML)
	}
	if strings.TrimSpace(body) == "" {
		body = "(no text body)"
	}
	b.WriteString("\n" + th.faint.Render(strings.Repeat("─", min(w, 40))) + "\n\n")
	b.WriteString(wrap.Foreground(th.muted).Render(body))
	return b.String()
}

func (m Model) renderStatus() string {
	th := m.th
	if m.showHelp {
		return th.help.Padding(0, 1).Render(m.helpText())
	}

	left := ""
	switch m.mode {
	case "confirm-clear":
		left = th.badgeBad.Render("clear " + m.inbox + "?  y/n")
	default:
		if m.status != "" {
			st := th.status
			if m.statusOK {
				st = th.statusOK
			} else if m.status != "refreshed" {
				st = lipgloss.NewStyle().Foreground(th.danger)
			}
			left = st.Render(truncate(m.status, m.w/2))
		}
	}

	right := m.contextHints()
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return lipgloss.NewStyle().Padding(0, 1).Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) contextHints() string {
	th := m.th
	h := func(k, l string) string { return th.kbd(k, l) }
	parts := []string{}
	switch m.focus {
	case focusInboxes:
		parts = []string{h("j/k", "move"), h("→", "messages"), h("g", "new"), h("y", "copy"), h("/", "search")}
	case focusMessages:
		parts = []string{h("j/k", "move"), h("↵", "read"), h("d", "del"), h("←", "back")}
	case focusReader:
		parts = []string{h("c", "code"), h("y", "sender"), h("d", "del"), h("←", "back")}
	}
	parts = append(parts, h("?", "help"))
	return strings.Join(parts, th.help.Render("   "))
}

func (m Model) helpText() string {
	th := m.th
	rows := []string{
		th.kbd("tab", "switch pane") + "   " + th.kbd("j/k ↑/↓", "move") + "   " + th.kbd("↵", "open / drill in") + "   " + th.kbd("← esc", "back"),
		th.kbd("g", "generate address") + "   " + th.kbd("y", "copy address/sender") + "   " + th.kbd("c", "copy code") + "   " + th.kbd("/", "search"),
		th.kbd("d/x", "delete message") + "   " + th.kbd("D", "clear inbox") + "   " + th.kbd("r", "refresh") + "   " + th.kbd("?", "close help") + "   " + th.kbd("q", "quit"),
	}
	return strings.Join(rows, "\n")
}

// ---- row + padding helpers ----

// twoLineRow renders a 2-line list item: line 1 = key + right-aligned time,
// line 2 = a muted sub. A left bar marks the selected row.
func (m Model) twoLineRow(w int, selected, focused bool, key, t, sub string) []string {
	return m.twoLineRowUnread(w, selected, focused, false, key, t, sub)
}

func (m Model) twoLineRowUnread(w int, selected, focused, unread bool, key, t, sub string) []string {
	th := m.th
	bar := "  "
	if selected {
		if focused {
			bar = th.rowSelBar.Render("▎") + " "
		} else {
			bar = th.faint.Render("▎") + " "
		}
	}
	avail := w - 2 // bar
	timeStr := th.rowTime.Render(t)
	keyW := avail - lipgloss.Width(timeStr) - 1
	if keyW < 1 {
		keyW = 1
	}

	keySt := th.rowKey
	if unread {
		keySt = th.rowUnread
	}
	if selected {
		keySt = th.rowSel
		if unread {
			keySt = th.rowUnread
		}
	}
	keyTxt := keySt.Render(truncate(key, keyW))
	line1 := bar + keyTxt + strings.Repeat(" ", max(1, avail-lipgloss.Width(keyTxt)-lipgloss.Width(timeStr))) + timeStr

	subSt := th.rowSub
	if unread {
		subSt = th.rowKey
	}
	line2 := "  " + subSt.Render(truncate(sub, avail))
	return []string{padVis(line1, w), padVis(line2, w)}
}

// windowBlocks flattens uniform 2-line blocks into at most maxLines lines,
// scrolled so the cursor's block stays visible.
func windowBlocks(blocks [][]string, cursor, maxLines int) []string {
	if maxLines < 1 || len(blocks) == 0 {
		return nil
	}
	per := 2
	vis := maxLines / per
	if vis < 1 {
		vis = 1
	}
	start := 0
	if cursor >= vis {
		start = cursor - vis + 1
	}
	end := min(len(blocks), start+vis)
	var out []string
	for i := start; i < end; i++ {
		out = append(out, blocks[i]...)
	}
	return out
}

// padVis pads a (possibly styled) line with trailing spaces to visible width w,
// truncating only when the *visible* width already exceeds w.
func padVis(s string, w int) string {
	vw := lipgloss.Width(s)
	if vw >= w {
		return s
	}
	return s + strings.Repeat(" ", w-vw)
}

// padTo ensures exactly n lines, each padded to width w.
func padTo(lines []string, n, w int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if i < len(lines) {
			out = append(out, padVis(lines[i], w))
		} else {
			out = append(out, strings.Repeat(" ", w))
		}
	}
	return out
}
