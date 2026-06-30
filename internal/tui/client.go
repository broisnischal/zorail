// Package tui implements the interactive terminal inbox viewer for a running
// Zorail server. It talks to the same JSON API the web dashboard uses, so it
// works against a local or remote server and reuses the long-poll /wait
// endpoint for instant, push-style live updates instead of busy polling.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a thin, typed wrapper over the Zorail HTTP API.
type Client struct {
	base  string // e.g. "http://localhost:8090" (no trailing slash, no /api)
	token string // optional bearer token; sent only when non-empty
	hc    *http.Client
}

// NewClient builds a client for the given base URL and optional token. The base
// may be given with or without a scheme or a trailing /api — it is normalized.
func NewClient(base, token string) *Client {
	base = strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(base), "/"), "/api")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &Client{
		base:  base,
		token: strings.TrimSpace(token),
		// No client-level timeout: /wait long-polls for up to ~120s. Per-call
		// deadlines are applied via context instead.
		hc: &http.Client{},
	}
}

// ---- wire types (mirror the API JSON) ----

type InboxSummary struct {
	Inbox        string    `json:"inbox"`
	MessageCount int       `json:"message_count"`
	LastReceived time.Time `json:"last_received"`
}

type MsgMeta struct {
	ID         string    `json:"id"`
	Inbox      string    `json:"inbox"`
	From       string    `json:"from"`
	EnvFrom    string    `json:"env_from"`
	To         []string  `json:"to"`
	Subject    string    `json:"subject"`
	Date       time.Time `json:"date"`
	ReceivedAt time.Time `json:"received_at"`
	Size       int64     `json:"size"`
}

type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type Extracted struct {
	Codes       []string `json:"codes"`
	Links       []string `json:"links"`
	Unsubscribe []string `json:"unsubscribe"`
}

type Spam struct {
	Score   int      `json:"score"`
	Label   string   `json:"label"`
	Reasons []string `json:"reasons"`
}

type FullMsg struct {
	MsgMeta
	Text        string            `json:"text"`
	HTML        string            `json:"html"`
	Headers     map[string]string `json:"headers"`
	Attachments []Attachment      `json:"attachments"`
	Extracted   Extracted         `json:"extracted"`
	Spam        Spam              `json:"spam"`
}

type Config struct {
	Version        string   `json:"version"`
	Domain         string   `json:"domain"`
	AllowedDomains []string `json:"allowed_domains"`
	AuthRequired   bool     `json:"auth_required"`
}

// ---- requests ----

func (c *Client) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNoContent {
		return errNoContent
	}
	if res.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}
	if res.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("%s %s: %s: %s", method, path, res.Status, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// Sentinel errors callers can branch on.
var (
	errNoContent    = fmt.Errorf("no content")
	errUnauthorized = fmt.Errorf("unauthorized — set a token with --token or $ZORAIL_TOKEN")
)

func (c *Client) Config(ctx context.Context) (Config, error) {
	var cfg Config
	err := c.do(ctx, http.MethodGet, "/api/config", &cfg)
	return cfg, err
}

func (c *Client) Inboxes(ctx context.Context) ([]InboxSummary, error) {
	var out []InboxSummary
	err := c.do(ctx, http.MethodGet, "/api/inboxes", &out)
	return out, err
}

func (c *Client) Messages(ctx context.Context, inbox string) ([]MsgMeta, error) {
	var out []MsgMeta
	err := c.do(ctx, http.MethodGet, "/api/inboxes/"+url.PathEscape(inbox)+"/messages?limit=200", &out)
	return out, err
}

func (c *Client) Message(ctx context.Context, id string) (*FullMsg, error) {
	var m FullMsg
	if err := c.do(ctx, http.MethodGet, "/api/messages/"+url.PathEscape(id), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (c *Client) Search(ctx context.Context, q string) ([]MsgMeta, error) {
	var out []MsgMeta
	err := c.do(ctx, http.MethodGet, "/api/search?q="+url.QueryEscape(q)+"&limit=200", &out)
	return out, err
}

func (c *Client) DeleteMessage(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/messages/"+url.PathEscape(id), nil)
}

func (c *Client) ClearInbox(ctx context.Context, inbox string) error {
	return c.do(ctx, http.MethodDelete, "/api/inboxes/"+url.PathEscape(inbox), nil)
}

// Wait blocks (server-side long poll) until a message newer than afterID lands
// in inbox, or the server's timeout elapses. On timeout it returns
// (nil, errNoContent) so the caller can simply re-issue the wait.
func (c *Client) Wait(ctx context.Context, inbox, afterID string, timeout int) (*FullMsg, error) {
	q := url.Values{}
	if afterID != "" {
		q.Set("after", afterID)
	}
	q.Set("timeout", strconv.Itoa(timeout))
	var m FullMsg
	err := c.do(ctx, http.MethodGet, "/api/inboxes/"+url.PathEscape(inbox)+"/wait?"+q.Encode(), &m)
	if err != nil {
		return nil, err
	}
	return &m, nil
}
