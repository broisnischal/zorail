package api_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nees/zorail/internal/api"
	"github.com/nees/zorail/internal/config"
	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/storage/sqlite"
)

func newTestServer(t *testing.T, token string) (*httptest.Server, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := &config.Config{HTTPAddr: "127.0.0.1:0", APIToken: token}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := api.New(cfg, store, log, nil)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, store
}

func seed(t *testing.T, store *sqlite.Store, inbox, subject, body string) string {
	t.Helper()
	m := &model.Message{
		ID:         "msg_" + subject,
		Inbox:      inbox,
		From:       "app@external.com",
		To:         []string{inbox},
		Subject:    subject,
		Text:       body,
		HTML:       "<p>" + body + "</p>",
		ReceivedAt: time.Now().UTC(),
		Size:       int64(len(body)),
		Raw:        []byte("Subject: " + subject + "\r\n\r\n" + body),
	}
	if err := store.SaveMessage(context.Background(), m); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return m.ID
}

func TestAPIFlow(t *testing.T) {
	ts, store := newTestServer(t, "")
	seed(t, store, "qa-1@zorail.test", "Welcome", "Your code is 123456")

	// SPA shell is served at root.
	if body := get(t, ts.URL+"/", ""); !strings.Contains(body, `id="__nuxt"`) {
		t.Error("SPA index not served at /")
	}
	// A deep/unknown path falls back to the SPA entry (client-side routing).
	if body := get(t, ts.URL+"/some/client/route", ""); !strings.Contains(body, `id="__nuxt"`) {
		t.Error("SPA fallback not served for unknown route")
	}

	// Inboxes.
	var inboxes []model.InboxSummary
	getJSON(t, ts.URL+"/api/inboxes", "", &inboxes)
	if len(inboxes) != 1 || inboxes[0].Inbox != "qa-1@zorail.test" || inboxes[0].MessageCount != 1 {
		t.Fatalf("inboxes = %+v", inboxes)
	}

	// Messages in inbox.
	var msgs []map[string]any
	getJSON(t, ts.URL+"/api/inboxes/qa-1%40zorail.test/messages", "", &msgs)
	if len(msgs) != 1 || msgs[0]["subject"] != "Welcome" {
		t.Fatalf("messages = %+v", msgs)
	}
	id := msgs[0]["id"].(string)

	// Full message.
	var full map[string]any
	getJSON(t, ts.URL+"/api/messages/"+id, "", &full)
	if !strings.Contains(full["text"].(string), "123456") {
		t.Errorf("message text = %v", full["text"])
	}

	// Delete.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/messages/"+id, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %v err=%v", res.StatusCode, err)
	}
	getJSON(t, ts.URL+"/api/inboxes", "", &inboxes)
	if len(inboxes) != 0 {
		t.Errorf("inbox should be empty after delete, got %+v", inboxes)
	}
}

func TestConfigSearchAndEnrichment(t *testing.T) {
	store, err := sqlite.Open(t.TempDir() + "/c.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := &config.Config{HTTPAddr: "127.0.0.1:0", Domain: "localhost", AllowedDomains: []string{"mail.test"}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, _ := api.New(cfg, store, log, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	seed(t, store, "qa@mail.test", "Verify", "Your code is 998877")

	// config exposes the catch-all domain + auth flag
	var conf map[string]any
	getJSON(t, ts.URL+"/api/config", "", &conf)
	if conf["domain"] != "mail.test" || conf["auth_required"] != false {
		t.Errorf("config = %+v", conf)
	}

	// search finds by subject
	var hits []map[string]any
	getJSON(t, ts.URL+"/api/search?q=Verify", "", &hits)
	if len(hits) != 1 {
		t.Fatalf("search hits = %d, want 1", len(hits))
	}

	// full message carries server-computed extracted + spam
	id := hits[0]["id"].(string)
	var full map[string]any
	getJSON(t, ts.URL+"/api/messages/"+id, "", &full)
	ext, ok := full["extracted"].(map[string]any)
	if !ok {
		t.Fatalf("no extracted field: %+v", full)
	}
	codes, _ := ext["codes"].([]any)
	if len(codes) == 0 || codes[0] != "998877" {
		t.Errorf("extracted codes = %v", ext["codes"])
	}
	if _, ok := full["spam"].(map[string]any); !ok {
		t.Errorf("no spam field: %+v", full)
	}
}

func TestAPIAuth(t *testing.T) {
	ts, store := newTestServer(t, "s3cret")
	seed(t, store, "qa-2@zorail.test", "Hi", "body")

	// No token -> 401.
	res, _ := http.Get(ts.URL + "/api/inboxes")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("no-token status = %d, want 401", res.StatusCode)
	}
	// Wrong token -> 401.
	if code := statusWithToken(t, ts.URL+"/api/inboxes", "nope"); code != http.StatusUnauthorized {
		t.Errorf("bad-token status = %d, want 401", code)
	}
	// Correct token -> 200.
	if code := statusWithToken(t, ts.URL+"/api/inboxes", "s3cret"); code != http.StatusOK {
		t.Errorf("good-token status = %d, want 200", code)
	}
	// Health is always open.
	res, _ = http.Get(ts.URL + "/api/health")
	if res.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want 200", res.StatusCode)
	}
}

func get(t *testing.T, url, token string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

func getJSON(t *testing.T, url, token string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(get(t, url, token)), v); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func statusWithToken(t *testing.T, url, token string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	return res.StatusCode
}
