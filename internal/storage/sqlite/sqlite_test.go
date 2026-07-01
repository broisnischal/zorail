package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/storage"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func save(t *testing.T, s *Store, id, inbox, subject, text string, raw []byte, atts ...model.Attachment) {
	t.Helper()
	m := &model.Message{
		ID: id, Inbox: inbox, From: "sender@example.com",
		Subject: subject, Text: text, Raw: raw, Size: int64(len(raw)),
		ReceivedAt: time.Now().UTC(), Attachments: atts,
	}
	if err := s.SaveMessage(context.Background(), m); err != nil {
		t.Fatalf("save %s: %v", id, err)
	}
}

// rawBlobCount reports how many distinct raw blobs are stored.
func (s *Store) rawBlobCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := s.r.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM raw_blobs`).Scan(&n); err != nil {
		t.Fatalf("count blobs: %v", err)
	}
	return n
}

func TestRawDedupAndLazyLoad(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	raw := []byte("From: a@b.c\r\nSubject: Hi\r\n\r\nsame bytes")

	// Two messages (fan-out) with identical raw → one blob.
	save(t, s, "01A", "a@z.dev", "Hi", "body one", raw)
	save(t, s, "01B", "b@z.dev", "Hi", "body two", raw)
	if got := s.rawBlobCount(t); got != 1 {
		t.Fatalf("expected 1 deduped blob, got %d", got)
	}

	// GetMessage must NOT carry raw bytes (lazy).
	m, err := s.GetMessage(ctx, "01A")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Raw) != 0 {
		t.Fatalf("GetMessage should not load raw, got %d bytes", len(m.Raw))
	}
	// GetRaw resolves the blob.
	gotRaw, err := s.GetRaw(ctx, "01A")
	if err != nil || string(gotRaw) != string(raw) {
		t.Fatalf("GetRaw = %q, %v", gotRaw, err)
	}

	// Deleting one message keeps the shared blob (still referenced by 01B).
	if err := s.DeleteMessage(ctx, "01A"); err != nil {
		t.Fatal(err)
	}
	if got := s.rawBlobCount(t); got != 1 {
		t.Fatalf("blob GC'd too early, count=%d", got)
	}
	// Deleting the last referrer GCs the blob.
	if err := s.DeleteMessage(ctx, "01B"); err != nil {
		t.Fatal(err)
	}
	if got := s.rawBlobCount(t); got != 0 {
		t.Fatalf("orphan blob not GC'd, count=%d", got)
	}
}

func TestLazyAttachment(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	att := model.Attachment{ID: "att1", Filename: "f.txt", ContentType: "text/plain", Size: 3, Content: []byte("abc")}
	save(t, s, "02A", "a@z.dev", "S", "b", []byte("raw"), att)

	m, err := s.GetMessage(ctx, "02A")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].ID != "att1" {
		t.Fatalf("attachment metadata missing: %+v", m.Attachments)
	}
	if len(m.Attachments[0].Content) != 0 {
		t.Fatal("GetMessage should not load attachment content")
	}
	got, err := s.GetAttachment(ctx, "02A", "att1")
	if err != nil || string(got.Content) != "abc" {
		t.Fatalf("GetAttachment = %+v, %v", got, err)
	}
	if _, err := s.GetAttachment(ctx, "02A", "nope"); err != storage.ErrNotFound {
		t.Fatalf("missing attachment should be ErrNotFound, got %v", err)
	}
}

func TestFTSSearch(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	save(t, s, "03A", "qa@z.dev", "Your Acme verification code", "Use 123456 to sign in", []byte("r1"))
	save(t, s, "03B", "qa@z.dev", "Newsletter", "unrelated content here", []byte("r2"))

	// Prefix token match across subject.
	res, err := s.SearchMessages(ctx, "acme cod", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != "03A" {
		t.Fatalf("search 'acme cod' = %+v", res)
	}
	// Body match.
	res, _ = s.SearchMessages(ctx, "unrelated", 10)
	if len(res) != 1 || res[0].ID != "03B" {
		t.Fatalf("search 'unrelated' = %+v", res)
	}
	// Non-alphanumeric junk must not error.
	if _, err := s.SearchMessages(ctx, "!!! @#$", 10); err != nil {
		t.Fatalf("junk query errored: %v", err)
	}
	// Deleting removes it from the index.
	if err := s.DeleteMessage(ctx, "03A"); err != nil {
		t.Fatal(err)
	}
	res, _ = s.SearchMessages(ctx, "acme", 10)
	if len(res) != 0 {
		t.Fatalf("deleted message still searchable: %+v", res)
	}
}

// TestUpgradeLegacyDatabase reproduces opening a database created before the
// raw_hash column / raw_blobs / FTS existed: migrate must add the column, index
// it, backfill FTS, and keep legacy rows (raw stored inline) readable via GetRaw.
func TestUpgradeLegacyDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	// Build a minimal pre-raw_hash schema and insert one legacy row.
	legacy, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, `
CREATE TABLE messages (
	id TEXT PRIMARY KEY, inbox TEXT NOT NULL, env_from TEXT, hdr_from TEXT,
	hdr_to TEXT, subject TEXT, message_id TEXT, date INTEGER,
	received_at INTEGER NOT NULL, text_body TEXT, html_body TEXT, headers TEXT,
	size INTEGER NOT NULL DEFAULT 0, raw BLOB
);
INSERT INTO messages (id, inbox, env_from, hdr_from, hdr_to, subject, message_id, text_body, html_body, headers, received_at, raw, size)
VALUES ('legacy1', 'old@z.dev', '', '', '[]', 'Old subject', '', 'legacy body', '', '{}', 1000, 'legacy raw bytes', 16);`); err != nil {
		t.Fatal(err)
	}
	_ = legacy.Close()

	// Opening via the Store must migrate cleanly (this used to fail with
	// "no such column: raw_hash").
	s, err := Open(path)
	if err != nil {
		t.Fatalf("migrate legacy db: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Legacy row: raw came from the inline column (raw_hash is NULL).
	raw, err := s.GetRaw(ctx, "legacy1")
	if err != nil || string(raw) != "legacy raw bytes" {
		t.Fatalf("legacy GetRaw = %q, %v", raw, err)
	}
	// Legacy row got indexed into FTS by the backfill.
	res, err := s.SearchMessages(ctx, "old", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != "legacy1" {
		t.Fatalf("legacy row not searchable after backfill: %+v", res)
	}
	// New writes work against the upgraded schema (raw_hash column present).
	save(t, s, "new1", "new@z.dev", "New", "new body", []byte("new raw"))
	if got := s.rawBlobCount(t); got != 1 {
		t.Fatalf("new write should create one raw blob, got %d", got)
	}
}

func TestLatestMessageID(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	if id, _ := s.LatestMessageID(ctx, "empty@z.dev", ""); id != "" {
		t.Fatalf("empty inbox should yield \"\", got %q", id)
	}
	save(t, s, "04A", "a@z.dev", "s", "t", []byte("r"))
	save(t, s, "04B", "a@z.dev", "s", "t", []byte("r2"))
	if id, _ := s.LatestMessageID(ctx, "a@z.dev", ""); id != "04B" {
		t.Fatalf("latest = %q, want 04B", id)
	}
	// after the newest → nothing new.
	if id, _ := s.LatestMessageID(ctx, "a@z.dev", "04B"); id != "" {
		t.Fatalf("after-newest should yield \"\", got %q", id)
	}
	// after an older id → the newest.
	if id, _ := s.LatestMessageID(ctx, "a@z.dev", "04A"); id != "04B" {
		t.Fatalf("after 04A = %q, want 04B", id)
	}
}
