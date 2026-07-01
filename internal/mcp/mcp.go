// Package mcp exposes Zorail as a Model Context Protocol server over Streamable
// HTTP, so coding/testing agents can mint disposable addresses and — crucially
// — block until a message arrives (wait_for_message) and read the OTP/link out
// of it. It is mounted at /mcp by the API server.
//
// Auth mirrors the JSON API: a `zk_` API key (or the legacy global token) in
// the Authorization header bounds every tool by the key's scope. The package is
// self-contained (it does not import internal/api) to avoid an import cycle.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nees/zorail/internal/auth"
	"github.com/nees/zorail/internal/config"
	"github.com/nees/zorail/internal/extract"
	"github.com/nees/zorail/internal/id"
	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/notify"
	"github.com/nees/zorail/internal/storage"
)

// Manager builds per-request MCP servers bound to the calling key's scope.
type Manager struct {
	store storage.Store
	cfg   *config.Config
	hub   *notify.Hub
}

// NewManager constructs the MCP manager.
func NewManager(cfg *config.Config, store storage.Store, hub *notify.Hub) *Manager {
	return &Manager{store: store, cfg: cfg, hub: hub}
}

// caller is the authenticated identity behind an MCP request.
type caller struct {
	admin  bool
	prefix string // inbox-prefix scope ("" = any)
}

func (c *caller) allows(inbox string) bool {
	if c.admin || c.prefix == "" {
		return true
	}
	return strings.HasPrefix(inbox, c.prefix)
}

type ctxKey int

const callerKey ctxKey = 0

// Handler returns the HTTP handler to mount at /mcp. It authenticates the
// request, then delegates to a freshly-scoped MCP server.
func (m *Manager) Handler() http.Handler {
	h := mcpsdk.NewStreamableHTTPHandler(m.getServer, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := m.authenticate(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey, c)))
	})
}

func (m *Manager) authenticate(r *http.Request) (*caller, error) {
	tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if tok == "" {
		if m.cfg.APIToken == "" {
			return &caller{admin: true}, nil // open mode
		}
		return nil, errors.New("missing API token")
	}
	if m.cfg.APIToken != "" && auth.ConstantEqual(tok, m.cfg.APIToken) {
		return &caller{admin: true}, nil
	}
	if auth.LooksLikeKey(tok) {
		k, err := m.store.GetAPIKeyByHash(r.Context(), auth.HashKey(tok))
		if err != nil {
			return nil, errors.New("invalid API key")
		}
		if !k.Has(model.ScopeRead) {
			return nil, errors.New("key lacks read scope")
		}
		return &caller{admin: k.Has(model.ScopeAdmin), prefix: k.InboxPrefix}, nil
	}
	return nil, errors.New("invalid credentials")
}

func callerFrom(ctx context.Context) *caller {
	if c, ok := ctx.Value(callerKey).(*caller); ok {
		return c
	}
	return &caller{}
}

func (m *Manager) getServer(r *http.Request) *mcpsdk.Server {
	c := callerFrom(r.Context())
	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "zorail", Version: "0.3.0"}, nil)

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "create_disposable_address",
		Description: "Mint a fresh disposable inbox address at this Zorail instance's domain. Returns the full address to use as a recipient.",
	}, m.mint(c))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "list_inboxes",
		Description: "List inboxes that have received mail, with message counts. Bounded by the API key's scope.",
	}, m.listInboxes(c))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "list_messages",
		Description: "List message metadata (id, from, subject, time) for an inbox, newest first.",
	}, m.listMessages(c))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "read_message",
		Description: "Read a full message by id, including body text and extracted one-time codes and links.",
	}, m.readMessage(c))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "wait_for_message",
		Description: "Block until a message newer than `after` (a message id, optional) arrives in the inbox, or until timeout_seconds elapses. Ideal for waiting on an OTP or magic link during a signup flow.",
	}, m.waitForMessage(c))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "delete_message",
		Description: "Delete a message by id.",
	}, m.deleteMessage(c))

	return s
}

// --- tool I/O types ---

type mintIn struct {
	Prefix string `json:"prefix,omitempty" jsonschema:"optional local-part prefix for the address"`
}
type mintOut struct {
	Address string `json:"address"`
}

type inboxIn struct {
	Inbox string `json:"inbox" jsonschema:"the inbox (recipient address)"`
	Limit int    `json:"limit,omitempty" jsonschema:"max messages to return (default 50)"`
}
type idIn struct {
	ID string `json:"id" jsonschema:"the message id"`
}
type waitIn struct {
	Inbox          string `json:"inbox" jsonschema:"the inbox to watch"`
	After          string `json:"after,omitempty" jsonschema:"only return a message with an id greater than this"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"how long to block (default 25, max 120)"`
}

type msgMeta struct {
	ID         string    `json:"id"`
	Inbox      string    `json:"inbox"`
	From       string    `json:"from"`
	Subject    string    `json:"subject"`
	ReceivedAt time.Time `json:"received_at"`
	Size       int64     `json:"size"`
}
type listInboxesOut struct {
	Inboxes []model.InboxSummary `json:"inboxes"`
}
type listMessagesOut struct {
	Messages []msgMeta `json:"messages"`
}
type messageOut struct {
	Found      bool      `json:"found"`
	ID         string    `json:"id,omitempty"`
	Inbox      string    `json:"inbox,omitempty"`
	From       string    `json:"from,omitempty"`
	Subject    string    `json:"subject,omitempty"`
	Text       string    `json:"text,omitempty"`
	Codes      []string  `json:"codes,omitempty"`
	Links      []string  `json:"links,omitempty"`
	ReceivedAt time.Time `json:"received_at,omitempty"`
}
type deleteOut struct {
	Deleted bool `json:"deleted"`
}

// --- tool handlers ---

func (m *Manager) mint(c *caller) mcpsdk.ToolHandlerFor[mintIn, mintOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in mintIn) (*mcpsdk.CallToolResult, mintOut, error) {
		local := strings.ToLower(strings.TrimSpace(in.Prefix))
		// Honor the key's inbox-prefix scope so minted addresses stay in-scope.
		if c.prefix != "" && !strings.HasPrefix(local, c.prefix) {
			local = c.prefix + local
		}
		if local == "" {
			local = "u"
		}
		domain := m.cfg.Domain
		if len(m.cfg.AllowedDomains) > 0 {
			domain = m.cfg.AllowedDomains[0]
		}
		addr := fmt.Sprintf("%s-%s@%s", local, id.New()[:10], domain)
		return nil, mintOut{Address: addr}, nil
	}
}

func (m *Manager) listInboxes(c *caller) mcpsdk.ToolHandlerFor[struct{}, listInboxesOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, listInboxesOut, error) {
		all, err := m.store.ListInboxes(ctx)
		if err != nil {
			return nil, listInboxesOut{}, err
		}
		out := make([]model.InboxSummary, 0, len(all))
		for _, ix := range all {
			if c.allows(ix.Inbox) {
				out = append(out, ix)
			}
		}
		return nil, listInboxesOut{Inboxes: out}, nil
	}
}

func (m *Manager) listMessages(c *caller) mcpsdk.ToolHandlerFor[inboxIn, listMessagesOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in inboxIn) (*mcpsdk.CallToolResult, listMessagesOut, error) {
		inbox := strings.ToLower(strings.TrimSpace(in.Inbox))
		if !c.allows(inbox) {
			return nil, listMessagesOut{}, errors.New("inbox outside key scope")
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 50
		}
		msgs, err := m.store.ListMessages(ctx, inbox, limit, 0)
		if err != nil {
			return nil, listMessagesOut{}, err
		}
		out := make([]msgMeta, 0, len(msgs))
		for _, msg := range msgs {
			out = append(out, msgMeta{ID: msg.ID, Inbox: msg.Inbox, From: msg.From, Subject: msg.Subject, ReceivedAt: msg.ReceivedAt, Size: msg.Size})
		}
		return nil, listMessagesOut{Messages: out}, nil
	}
}

func (m *Manager) readMessage(c *caller) mcpsdk.ToolHandlerFor[idIn, messageOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in idIn) (*mcpsdk.CallToolResult, messageOut, error) {
		msg, err := m.store.GetMessage(ctx, in.ID)
		if errors.Is(err, storage.ErrNotFound) {
			return nil, messageOut{Found: false}, nil
		}
		if err != nil {
			return nil, messageOut{}, err
		}
		if !c.allows(msg.Inbox) {
			return nil, messageOut{}, errors.New("message outside key scope")
		}
		return nil, toMessageOut(msg), nil
	}
}

func (m *Manager) deleteMessage(c *caller) mcpsdk.ToolHandlerFor[idIn, deleteOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in idIn) (*mcpsdk.CallToolResult, deleteOut, error) {
		msg, err := m.store.GetMessage(ctx, in.ID)
		if errors.Is(err, storage.ErrNotFound) {
			return nil, deleteOut{Deleted: false}, nil
		}
		if err != nil {
			return nil, deleteOut{}, err
		}
		if !c.allows(msg.Inbox) {
			return nil, deleteOut{}, errors.New("message outside key scope")
		}
		if err := m.store.DeleteMessage(ctx, in.ID); err != nil {
			return nil, deleteOut{}, err
		}
		return nil, deleteOut{Deleted: true}, nil
	}
}

func (m *Manager) waitForMessage(c *caller) mcpsdk.ToolHandlerFor[waitIn, messageOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in waitIn) (*mcpsdk.CallToolResult, messageOut, error) {
		inbox := strings.ToLower(strings.TrimSpace(in.Inbox))
		if !c.allows(inbox) {
			return nil, messageOut{}, errors.New("inbox outside key scope")
		}
		timeout := in.TimeoutSeconds
		if timeout <= 0 {
			timeout = 25
		}
		if timeout > 120 {
			timeout = 120
		}

		var ch <-chan string
		if m.hub != nil {
			cc, cancel := m.hub.Subscribe(inbox)
			defer cancel()
			ch = cc
		}
		if msg := m.newestAfter(ctx, inbox, in.After); msg != nil {
			return nil, toMessageOut(msg), nil
		}

		deadline := time.NewTimer(time.Duration(timeout) * time.Second)
		defer deadline.Stop()
		poll := time.NewTicker(2 * time.Second)
		defer poll.Stop()
		for {
			select {
			case <-ch:
				if msg := m.newestAfter(ctx, inbox, in.After); msg != nil {
					return nil, toMessageOut(msg), nil
				}
			case <-poll.C:
				if msg := m.newestAfter(ctx, inbox, in.After); msg != nil {
					return nil, toMessageOut(msg), nil
				}
			case <-deadline.C:
				return nil, messageOut{Found: false}, nil
			case <-ctx.Done():
				return nil, messageOut{Found: false}, ctx.Err()
			}
		}
	}
}

func (m *Manager) newestAfter(ctx context.Context, inbox, after string) *model.Message {
	id, err := m.store.LatestMessageID(ctx, inbox, after)
	if err != nil || id == "" {
		return nil
	}
	full, err := m.store.GetMessage(ctx, id)
	if err != nil {
		return nil
	}
	return full
}

func toMessageOut(msg *model.Message) messageOut {
	ex := extract.From(msg.Headers, msg.Text, msg.HTML)
	text := msg.Text
	if text == "" {
		text = extract.StripHTML(msg.HTML)
	}
	return messageOut{
		Found:      true,
		ID:         msg.ID,
		Inbox:      msg.Inbox,
		From:       msg.From,
		Subject:    msg.Subject,
		Text:       text,
		Codes:      ex.Codes,
		Links:      ex.Links,
		ReceivedAt: msg.ReceivedAt,
	}
}
