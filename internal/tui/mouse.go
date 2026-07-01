package tui

import tea "github.com/charmbracelet/bubbletea"

// onMouse handles wheel scrolling and click-to-focus, routed by the pane the
// cursor is over. The wheel scrolls the reader when it's over the reader,
// otherwise it moves the selection in the list under the cursor — so the mouse
// "just works" the way people expect in a three-pane UI.
func (m Model) onMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode == "search" {
		return m, nil
	}
	d := m.dims()
	pane := m.paneAt(msg.X, d)

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.wheel(pane, true)
	case tea.MouseButtonWheelDown:
		return m.wheel(pane, false)
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		innerH := d.bodyH - 2
		switch pane {
		case focusInboxes:
			m.focus = focusInboxes
			if i := rowIndexAt(msg.Y, innerH, m.inboxIdx); i >= 0 && i < len(m.inboxes) && i != m.inboxIdx {
				return m, m.selectInbox(i) // click an inbox → open it
			}
		case focusMessages:
			if d.msgW == 0 {
				return m, nil
			}
			m.focus = focusMessages
			if i := rowIndexAt(msg.Y, innerH, m.msgIdx); i >= 0 && i < len(m.messages) {
				m.msgIdx = i
				return m, m.openMessage(m.messages[i].ID) // click a message → read it
			}
		case focusReader:
			if m.current != nil {
				m.focus = focusReader
			}
		}
		return m, nil
	}
	return m, nil
}

// rowIndexAt maps a click's screen row y to the list-item index in a pane, or
// -1 if the click isn't on a row. It mirrors the layout: 1 header line + 1 box
// border above the content, then 2 header lines (title + blank) inside, then
// 2-line rows, windowed to keep the cursor visible (see windowBlocks).
func rowIndexAt(y, innerH, cursor int) int {
	content := y - 2 // subtract header line + box top border
	if content < 0 || content >= innerH {
		return -1
	}
	listRow := content - 2 // subtract the title + blank line
	if listRow < 0 {
		return -1
	}
	vis := (innerH - 2) / 2
	if vis < 1 {
		vis = 1
	}
	start := 0
	if cursor >= vis {
		start = cursor - vis + 1
	}
	return start + listRow/2
}

func (m Model) wheel(pane focus, up bool) (tea.Model, tea.Cmd) {
	switch pane {
	case focusReader:
		if m.current == nil {
			return m, nil
		}
		if up {
			m.reader.ScrollUp(3)
		} else {
			m.reader.ScrollDown(3)
		}
		return m, nil
	case focusMessages:
		if up && m.msgIdx > 0 {
			m.msgIdx--
		} else if !up && m.msgIdx < len(m.messages)-1 {
			m.msgIdx++
		}
		return m, nil
	case focusInboxes:
		if up && m.inboxIdx > 0 {
			return m, m.selectInbox(m.inboxIdx - 1)
		}
		if !up && m.inboxIdx < len(m.inboxes)-1 {
			return m, m.selectInbox(m.inboxIdx + 1)
		}
	}
	return m, nil
}

// paneAt maps an x column to the pane occupying it, given the current layout.
func (m Model) paneAt(x int, d paneDims) focus {
	if x < d.leftW {
		return focusInboxes
	}
	if d.msgW > 0 && x < d.leftW+d.msgW {
		return focusMessages
	}
	if d.readerW > 0 {
		return focusReader
	}
	return focusMessages
}
