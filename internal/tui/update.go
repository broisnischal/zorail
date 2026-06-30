package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.ready = true
		m.resize()
		return m, nil

	case tea.KeyMsg:
		return m.onKey(msg)

	case configMsg:
		m.cfg = Config(msg)
		return m, nil

	case inboxesMsg:
		m.live = true // a successful fetch means we're connected
		prev := m.inbox
		m.inboxes = msg
		// Auto-select the first inbox on first load so the UI is never empty.
		if prev == "" && len(m.inboxes) > 0 {
			return m, m.selectInbox(0)
		}
		// Keep the cursor pinned to the selected inbox as the list reorders.
		for i, ib := range m.inboxes {
			if ib.Inbox == m.inbox {
				m.inboxIdx = i
				break
			}
		}
		return m, nil

	case messagesMsg:
		if msg.inbox != m.inbox {
			return m, nil // a stale load for an inbox we've since left
		}
		m.messages = msg.msgs
		if m.msgIdx >= len(m.messages) {
			m.msgIdx = max(0, len(m.messages)-1)
		}
		var cmds []tea.Cmd
		// Arm the live wait loop once, when the selection's first load returns.
		if msg.selection && m.pendWait == msg.inbox {
			m.pendWait = ""
			m.waitGen++
			m.waitAfter = ""
			if len(m.messages) > 0 {
				m.waitAfter = m.messages[0].ID
			}
			cmds = append(cmds, m.waitCmd(m.inbox, m.waitAfter, m.waitGen))
		}
		return m, tea.Batch(cmds...)

	case openedMsg:
		m.current = msg.msg
		m.read[msg.msg.ID] = true
		m.focus = focusReader
		m.setReaderContent()
		return m, nil

	case searchMsg:
		m.messages = msg.msgs
		m.msgIdx = 0
		return m, nil

	case waitMsg:
		return m.onWait(msg)

	case rewaitMsg:
		if msg.gen == m.waitGen {
			return m, m.waitCmd(m.inbox, m.waitAfter, m.waitGen)
		}
		return m, nil

	case tickMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, m.fetchInboxes(), tickCmd())
		if m.inbox != "" && m.mode != "search" {
			cmds = append(cmds, m.fetchMessages(m.inbox, false))
		}
		return m, tea.Batch(cmds...)

	case statusMsg:
		m.status, m.statusOK = msg.text, msg.ok
		var cmds []tea.Cmd
		// Refresh after a mutating action.
		cmds = append(cmds, m.fetchInboxes())
		if m.inbox != "" {
			cmds = append(cmds, m.fetchMessages(m.inbox, false))
		}
		return m, tea.Batch(cmds...)

	case errMsg:
		m.live = false
		m.status, m.statusOK = msg.err.Error(), false
		return m, nil
	}

	// Forward anything else (e.g. internal viewport msgs) to the reader.
	if m.focus == focusReader {
		var cmd tea.Cmd
		m.reader, cmd = m.reader.Update(msg)
		return m, cmd
	}
	return m, nil
}

// onWait handles the result of a long-poll: instant new-mail push, timeout
// re-arm, or error back-off — all keyed to the current generation so stale
// goroutines from a previous inbox are ignored.
func (m Model) onWait(msg waitMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.waitGen {
		return m, nil // stale: we've switched inboxes since this wait started
	}
	if msg.err != nil {
		m.live = false
		// Back off, then re-arm the same wait.
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return rewaitMsg{msg.gen} })
	}
	m.live = true

	if msg.msg == nil {
		// Server-side timeout with nothing new — immediately re-arm.
		return m, m.waitCmd(m.inbox, m.waitAfter, m.waitGen)
	}

	// New mail arrived in the watched inbox.
	m.waitAfter = msg.msg.ID
	m.status = "✦ new mail · " + firstNonEmpty(msg.msg.Subject, "(no subject)")
	m.statusOK = true
	m.flash = time.Now()
	cmds := []tea.Cmd{
		m.fetchMessages(m.inbox, false),
		m.fetchInboxes(),
		m.waitCmd(m.inbox, m.waitAfter, m.waitGen),
	}
	return m, tea.Batch(cmds...)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
