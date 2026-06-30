package tui

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type focus int

const (
	focusInboxes focus = iota
	focusMessages
	focusReader
)

const (
	refreshEvery = 4 * time.Second
	waitTimeout  = 25 // seconds; server caps at 120
)

// Model is the Bubble Tea state for the zmail TUI.
type Model struct {
	c  *Client
	th theme

	w, h int
	cfg  Config

	inboxes  []InboxSummary
	inbox    string // selected inbox address
	messages []MsgMeta
	current  *FullMsg
	read     map[string]bool

	focus    focus
	inboxIdx int
	msgIdx   int

	reader viewport.Model
	search textinput.Model
	mode   string // "", "search", "confirm-clear"

	// live long-poll bookkeeping
	waitGen   int
	waitAfter string
	pendWait  string // inbox awaiting its first message-list load before waiting
	live      bool

	status   string
	statusOK bool
	flash    time.Time // when set recently, the status reads as "new mail"
	showHelp bool
	ready    bool
	quitting bool
}

// New builds the initial model.
func New(c *Client) Model {
	ti := textinput.New()
	ti.Prompt = "search "
	ti.Placeholder = "this inbox + all mail…"
	ti.CharLimit = 120

	vp := viewport.New(0, 0)
	// Let j/k scroll the reader in addition to the arrow keys.
	vp.KeyMap.Down.SetKeys("down", "j")
	vp.KeyMap.Up.SetKeys("up", "k")

	return Model{
		c:      c,
		th:     newTheme(),
		read:   map[string]bool{},
		search: ti,
		reader: vp,
		focus:  focusInboxes,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetchConfig(), m.fetchInboxes(), tickCmd())
}

// ---- messages ----

type (
	errMsg      struct{ err error }
	configMsg   Config
	inboxesMsg  []InboxSummary
	messagesMsg struct {
		inbox string
		msgs  []MsgMeta
		// selection is true when this load was triggered by switching inboxes,
		// so the live wait loop should (re)start from the newest message.
		selection bool
	}
	openedMsg struct{ msg *FullMsg }
	searchMsg struct{ msgs []MsgMeta }
	waitMsg   struct {
		inbox string
		after string
		gen   int
		msg   *FullMsg
		err   error
	}
	rewaitMsg struct{ gen int }
	tickMsg   time.Time
	statusMsg struct {
		text string
		ok   bool
	}
)

func tickCmd() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ---- commands ----

func (m Model) fetchConfig() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cfg, err := m.c.Config(ctx)
		if err != nil {
			return errMsg{err}
		}
		return configMsg(cfg)
	}
}

func (m Model) fetchInboxes() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ib, err := m.c.Inboxes(ctx)
		if err != nil {
			return errMsg{err}
		}
		return inboxesMsg(ib)
	}
}

func (m Model) fetchMessages(inbox string, selection bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msgs, err := m.c.Messages(ctx, inbox)
		if err != nil {
			return errMsg{err}
		}
		return messagesMsg{inbox: inbox, msgs: msgs, selection: selection}
	}
}

func (m Model) openMessage(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msg, err := m.c.Message(ctx, id)
		if err != nil {
			return errMsg{err}
		}
		return openedMsg{msg}
	}
}

func (m Model) doSearch(q string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msgs, err := m.c.Search(ctx, q)
		if err != nil {
			return errMsg{err}
		}
		return searchMsg{msgs}
	}
}

func (m Model) waitCmd(inbox, after string, gen int) tea.Cmd {
	return func() tea.Msg {
		// Long-poll; allow a little more than the server's own timeout.
		ctx, cancel := context.WithTimeout(context.Background(), (waitTimeout+10)*time.Second)
		defer cancel()
		msg, err := m.c.Wait(ctx, inbox, after, waitTimeout)
		return waitMsg{inbox: inbox, after: after, gen: gen, msg: msg, err: err}
	}
}

func (m Model) deleteMsgCmd(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.c.DeleteMessage(ctx, id); err != nil {
			return errMsg{err}
		}
		return statusMsg{"message deleted", true}
	}
}

func (m Model) clearInboxCmd(inbox string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.c.ClearInbox(ctx, inbox); err != nil {
			return errMsg{err}
		}
		return statusMsg{"inbox cleared", true}
	}
}

var adjectives = []string{"qa", "test", "dev", "stage", "demo", "temp", "probe", "scratch", "sandbox", "check"}

func (m Model) generateAddress() string {
	dom := m.cfg.Domain
	if dom == "" {
		dom = "localhost"
	}
	return fmt.Sprintf("%s-%d@%s", adjectives[rand.Intn(len(adjectives))], 1000+rand.Intn(9000), dom)
}

// selectInbox points the UI at inbox i, loads its messages, and arms the live
// wait loop to start once that first load returns.
func (m *Model) selectInbox(i int) tea.Cmd {
	if i < 0 || i >= len(m.inboxes) {
		return nil
	}
	m.inboxIdx = i
	m.inbox = m.inboxes[i].Inbox
	m.msgIdx = 0
	m.messages = nil
	m.current = nil
	m.pendWait = m.inbox
	return m.fetchMessages(m.inbox, true)
}
