package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nees/zorail/internal/config"
	"github.com/nees/zorail/internal/id"
	zmcp "github.com/nees/zorail/internal/mcp"
	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/notify"
	"github.com/nees/zorail/internal/storage/sqlite"
)

func connect(t *testing.T) (*mcpsdk.ClientSession, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "mcp.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := &config.Config{Domain: "zorail.test", AllowedDomains: []string{"zorail.test"}}
	mgr := zmcp.NewManager(cfg, store, notify.NewHub())

	mux := http.NewServeMux()
	mux.Handle("/mcp", mgr.Handler())
	mux.Handle("/mcp/", mgr.Handler())
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(),
		&mcpsdk.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess, store
}

func decodeStructured(t *testing.T, res *mcpsdk.CallToolResult, dst any) {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatalf("decode structured: %v", err)
	}
}

func seedMsg(t *testing.T, store *sqlite.Store, inbox, subject, body string) {
	t.Helper()
	m := &model.Message{ID: id.New(), Inbox: inbox, Subject: subject, Text: body, ReceivedAt: time.Now().UTC()}
	if err := store.SaveMessage(context.Background(), m); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestMCPMintAndRead(t *testing.T) {
	sess, store := connect(t)
	ctx := context.Background()

	// create_disposable_address
	res, err := sess.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "create_disposable_address", Arguments: map[string]any{"prefix": "qa"},
	})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	var mint struct {
		Address string `json:"address"`
	}
	decodeStructured(t, res, &mint)
	if mint.Address == "" || mint.Address[len(mint.Address)-len("@zorail.test"):] != "@zorail.test" {
		t.Fatalf("bad minted address: %q", mint.Address)
	}

	// seed and read back via wait_for_message
	seedMsg(t, store, "qa-7@zorail.test", "Welcome", "Your code is 246810")
	res, err = sess.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "wait_for_message", Arguments: map[string]any{"inbox": "qa-7@zorail.test", "timeout_seconds": 5},
	})
	if err != nil {
		t.Fatalf("wait_for_message: %v", err)
	}
	var msg struct {
		Found bool     `json:"found"`
		Codes []string `json:"codes"`
	}
	decodeStructured(t, res, &msg)
	if !msg.Found {
		t.Fatal("wait_for_message did not find seeded message")
	}
	if len(msg.Codes) == 0 || msg.Codes[0] != "246810" {
		t.Fatalf("expected code 246810, got %v", msg.Codes)
	}
}
