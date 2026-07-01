package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// onKey routes a keypress based on the active mode and focused pane.
func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Modal input first.
	switch m.mode {
	case "search":
		return m.onSearchKey(msg)
	case "confirm-clear":
		switch msg.String() {
		case "y", "Y", "enter":
			m.mode = ""
			return m, m.clearInboxCmd(m.inbox)
		default:
			m.mode = ""
			m.status = "clear cancelled"
			return m, nil
		}
	}

	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case "m":
		// Toggle mouse capture. Off → the terminal's own drag-to-select and copy
		// work again; on → click and wheel-scroll work in the app. Can't have both
		// at once (a terminal limitation), so this is the switch between them.
		if m.mouseOn {
			m.mouseOn = false
			m.status, m.statusOK = "mouse off — drag to select & copy · press m to re-enable click/scroll", true
			return m, tea.DisableMouse
		}
		m.mouseOn = true
		m.status, m.statusOK = "mouse on — click to select · wheel to scroll", true
		return m, tea.EnableMouseCellMotion
	case "r":
		var cmds []tea.Cmd
		cmds = append(cmds, m.fetchInboxes())
		if m.inbox != "" {
			cmds = append(cmds, m.fetchMessages(m.inbox, false))
		}
		m.status = "refreshed"
		m.statusOK = true
		return m, tea.Batch(cmds...)
	case "g":
		addr := m.generateAddress()
		ok := copyToClipboard(addr)
		m.status = "✦ generated " + addr
		if ok {
			m.status += " · copied"
		}
		m.statusOK = true
		// It won't appear in the inbox list until it receives mail; point the
		// live watcher at it now so the first message shows instantly.
		m.inbox = addr
		m.messages = nil
		m.current = nil
		m.msgIdx = 0
		m.pendWait = addr
		m.focus = focusMessages
		return m, m.fetchMessages(addr, true)
	case "/":
		m.mode = "search"
		m.search.SetValue("")
		m.search.Focus()
		m.focus = focusMessages
		return m, nil
	case "tab":
		m.cycleFocus(1)
		return m, m.focusCmd()
	case "shift+tab":
		m.cycleFocus(-1)
		return m, m.focusCmd()
	}

	switch m.focus {
	case focusInboxes:
		return m.onInboxKey(msg)
	case focusMessages:
		return m.onMessageKey(msg)
	case focusReader:
		return m.onReaderKey(msg)
	}
	return m, nil
}

func (m *Model) cycleFocus(dir int) {
	order := []focus{focusInboxes, focusMessages}
	if m.current != nil {
		order = append(order, focusReader)
	}
	idx := 0
	for i, f := range order {
		if f == m.focus {
			idx = i
			break
		}
	}
	m.focus = order[(idx+dir+len(order))%len(order)]
}

// focusCmd loads data needed by the newly focused pane (none, currently).
func (m Model) focusCmd() tea.Cmd { return nil }

func (m Model) onInboxKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.inboxIdx < len(m.inboxes)-1 {
			return m, m.selectInbox(m.inboxIdx + 1)
		}
	case "k", "up":
		if m.inboxIdx > 0 {
			return m, m.selectInbox(m.inboxIdx - 1)
		}
	case "g", "home":
		if len(m.inboxes) > 0 {
			return m, m.selectInbox(0)
		}
	case "G", "end":
		if len(m.inboxes) > 0 {
			return m, m.selectInbox(len(m.inboxes) - 1)
		}
	case "enter", "l", "right":
		if len(m.messages) > 0 {
			m.focus = focusMessages
		}
	case "y":
		if m.inbox != "" {
			m.copyStatus(m.inbox, "address")
		}
		return m, nil
	case "D":
		if m.inbox != "" {
			m.mode = "confirm-clear"
		}
		return m, nil
	}
	return m, nil
}

func (m Model) onMessageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.msgIdx < len(m.messages)-1 {
			m.msgIdx++
		}
	case "k", "up":
		if m.msgIdx > 0 {
			m.msgIdx--
		}
	case "g", "home":
		m.msgIdx = 0
	case "G", "end":
		m.msgIdx = max(0, len(m.messages)-1)
	case "enter", "l", "right":
		if m.msgIdx < len(m.messages) {
			return m, m.openMessage(m.messages[m.msgIdx].ID)
		}
	case "h", "left", "esc":
		if m.mode == "" {
			m.focus = focusInboxes
		}
	case "d", "x":
		if m.msgIdx < len(m.messages) {
			return m, m.deleteMsgCmd(m.messages[m.msgIdx].ID)
		}
	case "y":
		if m.inbox != "" {
			m.copyStatus(m.inbox, "address")
		}
		return m, nil
	case "D":
		if m.inbox != "" {
			m.mode = "confirm-clear"
		}
		return m, nil
	}
	return m, nil
}

func (m Model) onReaderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "h", "left", "esc":
		m.focus = focusMessages
		return m, nil
	case "c":
		if m.current != nil && len(m.current.Extracted.Codes) > 0 {
			m.copyStatus(m.current.Extracted.Codes[0], "code")
		} else {
			m.status, m.statusOK = "no code detected", false
		}
		return m, nil
	case "y":
		if m.current != nil {
			_, email := parseFrom(firstNonEmpty(m.current.From, m.current.EnvFrom))
			if email != "" {
				m.copyStatus(email, "sender")
			}
		}
		return m, nil
	case "d", "x":
		if m.current != nil {
			id := m.current.ID
			m.current = nil
			m.focus = focusMessages
			return m, m.deleteMsgCmd(id)
		}
		return m, nil
	case "o":
		// Open the first (or only) link in the browser.
		if links := m.currentLinks(); len(links) > 0 {
			return m, m.openLinkCmd(links[0])
		}
		m.status, m.statusOK = "no links in this message", false
		return m, nil
	case "a":
		// Open the first attachment in the OS default app.
		if m.current != nil && len(m.current.Attachments) > 0 {
			return m, m.openAttachmentCmd(m.current.ID, m.current.Attachments[0])
		}
		m.status, m.statusOK = "no attachments", false
		return m, nil
	case "p":
		// Copy the whole message body to the clipboard — reliable copy without
		// needing to drag-select.
		if m.current != nil {
			body := m.current.Text
			if strings.TrimSpace(body) == "" && m.current.HTML != "" {
				body = htmlToText(m.current.HTML)
			}
			if copyToClipboard(strings.TrimSpace(body)) {
				m.status, m.statusOK = "message body copied", true
			} else {
				m.status, m.statusOK = "copy failed", false
			}
		}
		return m, nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Open the Nth link.
		n := int(msg.String()[0] - '0')
		links := m.currentLinks()
		if n <= len(links) {
			return m, m.openLinkCmd(links[n-1])
		}
		m.status, m.statusOK = "no link "+msg.String(), false
		return m, nil
	}
	// Everything else (up/down/pgup/pgdn/u/d-scroll) drives the reader scroll.
	var cmd tea.Cmd
	m.reader, cmd = m.reader.Update(msg)
	return m, cmd
}

func (m Model) onSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = ""
		m.search.Blur()
		m.status = ""
		// Restore the focused inbox's messages.
		if m.inbox != "" {
			return m, m.fetchMessages(m.inbox, false)
		}
		return m, nil
	case "enter":
		q := m.search.Value()
		if q == "" {
			m.mode = ""
			m.search.Blur()
			if m.inbox != "" {
				return m, m.fetchMessages(m.inbox, false)
			}
			return m, nil
		}
		return m, m.doSearch(q)
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	return m, cmd
}

// copyStatus copies v to the clipboard and reflects the result in the status bar.
func (m *Model) copyStatus(v, what string) {
	if copyToClipboard(v) {
		m.status, m.statusOK = what+" copied · "+v, true
	} else {
		m.status, m.statusOK = "copy failed · "+v, false
	}
}
