package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nees/zorail/internal/api"
	"github.com/nees/zorail/internal/config"
	"github.com/nees/zorail/internal/ingest"
	"github.com/nees/zorail/internal/notify"
	"github.com/nees/zorail/internal/storage/sqlite"
)

// fullServer builds an API server wired with ingest + notify deps, so the
// ingest, wait, and forward paths can be exercised.
func fullServer(t *testing.T) (*httptest.Server, *sqlite.Store, *config.Config) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "mt.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := &config.Config{
		HTTPAddr:        "127.0.0.1:0",
		Domain:          "zorail.test",
		AllowedDomains:  []string{"zorail.test"},
		MaxMessageBytes: 1 << 20,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := notify.NewHub()
	ing := ingest.New(cfg, store, log, hub)
	srv, err := api.New(cfg, store, log, &api.Deps{Ingest: ing, Hub: hub})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, store, cfg
}

func postJSON(t *testing.T, url, token string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

// setupAdmin runs first-run setup and returns the admin session token.
func setupAdmin(t *testing.T, base, email string) string {
	t.Helper()
	resp := postJSON(t, base+"/api/setup", "", map[string]string{"organization": "Acme", "email": email, "password": "supersecret"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: got %d", resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Token == "" {
		t.Fatal("setup returned no token")
	}
	return out.Token
}

// TestSetupFlow covers first-run setup, re-setup rejection, and closed registration.
func TestSetupFlow(t *testing.T) {
	ts, _, _ := fullServer(t)

	// before setup: needs_setup is true
	r, _ := http.Get(ts.URL + "/api/setup")
	var st struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	_ = json.NewDecoder(r.Body).Decode(&st)
	r.Body.Close()
	if !st.NeedsSetup {
		t.Fatal("fresh instance should need setup")
	}

	token := setupAdmin(t, ts.URL, "admin@acme.test")

	// setup again → 409
	resp := postJSON(t, ts.URL+"/api/setup", "", map[string]string{"organization": "X", "email": "b@b.com", "password": "supersecret"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second setup: got %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()

	// registration closed → 403
	resp = postJSON(t, ts.URL+"/api/auth/register", "", map[string]string{"email": "c@b.com", "password": "supersecret"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("register after setup: got %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	// the admin token can mint keys (proves it carries manage/admin scope)
	resp = postJSON(t, ts.URL+"/api/keys", token, map[string]any{"name": "ci", "scopes": []string{"read"}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("admin create key: got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestIdentityFlow covers setup → mint scoped key → scope enforcement.
func TestIdentityFlow(t *testing.T) {
	ts, _, _ := fullServer(t)
	adminToken := setupAdmin(t, ts.URL, "a@b.com")
	login := struct{ Token string }{Token: adminToken}

	// mint a scoped read key
	resp := postJSON(t, ts.URL+"/api/keys", login.Token, map[string]any{"name": "ci", "scopes": []string{"read"}, "inbox_prefix": "qa-"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create key: got %d", resp.StatusCode)
	}
	var key struct {
		Secret string `json:"secret"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&key)
	resp.Body.Close()
	if key.Secret == "" {
		t.Fatal("created key has no secret")
	}

	// the scoped read key may read qa-* but not other-*
	for inbox, want := range map[string]int{"qa-1@zorail.test": http.StatusOK, "other@zorail.test": http.StatusForbidden} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/inboxes/"+inbox+"/messages", nil)
		req.Header.Set("Authorization", "Bearer "+key.Secret)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if r.StatusCode != want {
			t.Fatalf("scope check %s: got %d want %d", inbox, r.StatusCode, want)
		}
		r.Body.Close()
	}
}

// TestIngestAndWait pushes a message over the HTTP ingest endpoint and reads it
// back via the long-poll wait endpoint.
func TestIngestAndWait(t *testing.T) {
	ts, _, _ := fullServer(t)

	raw := "From: noreply@app.test\r\nSubject: Code\r\nTo: qa-7@zorail.test\r\n\r\nYour code is 314159\r\n"
	resp := postJSON(t, ts.URL+"/api/ingest", "", map[string]any{
		"raw":      raw,
		"env_from": "noreply@app.test",
		"rcpts":    []string{"qa-7@zorail.test"},
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("ingest: got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// wait should return the message immediately (after="")
	r, err := http.Get(ts.URL + "/api/inboxes/qa-7%40zorail.test/wait?timeout=5")
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusOK {
		t.Fatalf("wait: got %d", r.StatusCode)
	}
	var got struct {
		Subject   string `json:"subject"`
		Extracted struct {
			Codes []string `json:"codes"`
		} `json:"extracted"`
	}
	_ = json.NewDecoder(r.Body).Decode(&got)
	r.Body.Close()
	if got.Subject != "Code" {
		t.Fatalf("wait subject: got %q", got.Subject)
	}
	if len(got.Extracted.Codes) == 0 || got.Extracted.Codes[0] != "314159" {
		t.Fatalf("expected extracted code 314159, got %v", got.Extracted.Codes)
	}
}

// TestWaitBlocksUntilArrival verifies the long-poll wakes on a later arrival.
func TestWaitBlocksUntilArrival(t *testing.T) {
	ts, _, _ := fullServer(t)

	done := make(chan int, 1)
	go func() {
		r, err := http.Get(ts.URL + "/api/inboxes/qa-9%40zorail.test/wait?timeout=10")
		if err != nil {
			done <- -1
			return
		}
		done <- r.StatusCode
		r.Body.Close()
	}()

	// Give the waiter time to subscribe, then ingest.
	time.Sleep(200 * time.Millisecond)
	resp := postJSON(t, ts.URL+"/api/ingest", "", map[string]any{
		"raw":   "Subject: Hi\r\nTo: qa-9@zorail.test\r\n\r\nhello\r\n",
		"rcpts": []string{"qa-9@zorail.test"},
	})
	resp.Body.Close()

	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("blocking wait: got %d", code)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("wait did not return")
	}
}

// TestForwardEnqueue checks that ingest enqueues a forward job for a forwarding
// address with a verified destination.
func TestForwardEnqueue(t *testing.T) {
	ts, store, _ := fullServer(t)
	ctx := context.Background()

	login := struct{ Token string }{Token: setupAdmin(t, ts.URL, "u@b.com")}

	// reserve a forwarding address
	resp := postJSON(t, ts.URL+"/api/addresses", login.Token, map[string]any{
		"address": "fwd@zorail.test", "type": "forward", "forward_to": []string{"me@gmail.com"},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reserve forward: got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// request verification (no mailer → returns confirm_url), then confirm
	resp = postJSON(t, ts.URL+"/api/verify/request", login.Token, map[string]string{"dest": "me@gmail.com"})
	var vr struct {
		ConfirmURL string `json:"confirm_url"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&vr)
	resp.Body.Close()
	if vr.ConfirmURL == "" {
		t.Fatal("expected confirm_url when no mailer configured")
	}
	if r, err := http.Get(vr.ConfirmURL); err != nil {
		t.Fatal(err)
	} else {
		r.Body.Close()
	}

	// ingest a message to the forwarding address
	resp = postJSON(t, ts.URL+"/api/ingest", login.Token, map[string]any{
		"raw":   "Subject: Forward me\r\nTo: fwd@zorail.test\r\n\r\nbody\r\n",
		"rcpts": []string{"fwd@zorail.test"},
	})
	resp.Body.Close()

	jobs, err := store.ClaimForwardJobs(ctx, time.Now().Add(time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 forward job, got %d", len(jobs))
	}
	if jobs[0].Dest != "me@gmail.com" {
		t.Fatalf("forward dest: got %q", jobs[0].Dest)
	}
}
