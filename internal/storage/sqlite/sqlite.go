// Package sqlite implements storage.Store on top of a single SQLite database
// file using the pure-Go modernc.org/sqlite driver (no cgo), keeping Zorail a
// single self-contained static binary — ideal for self-hosting.
package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/storage"
)

// Store is a SQLite-backed storage.Store.
//
// It keeps two connection pools against the same WAL database: a single-writer
// pool (w) that serializes writes, and a multi-connection reader pool (r) that
// serves SELECTs concurrently. Under WAL, readers never block the writer and
// vice-versa, so a slow ingest write no longer stalls every API read. Every
// write method uses w; every read-only method uses r.
type Store struct {
	w *sql.DB // single writer (serializes writes; avoids "database is locked")
	r *sql.DB // reader pool (concurrent SELECTs under WAL)
}

// Open opens (creating if needed) the database at path and applies the schema.
func Open(path string) (*Store, error) {
	// Writer: one connection, immediate transactions so BEGIN takes the write
	// lock up front rather than upgrading mid-transaction (avoids deadlocks
	// against readers). WAL + busy_timeout + foreign_keys as before.
	wdsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)&_txlock=immediate", path)
	w, err := sql.Open("sqlite", wdsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite (writer): %w", err)
	}
	w.SetMaxOpenConns(1)

	// Reader: many connections. WAL lets these run concurrently with the writer.
	rdsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=query_only(true)", path)
	r, err := sql.Open("sqlite", rdsn)
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("open sqlite (reader): %w", err)
	}
	r.SetMaxOpenConns(max(4, runtime.NumCPU()))

	s := &Store{w: w, r: r}
	if err := s.migrate(context.Background()); err != nil {
		_ = w.Close()
		_ = r.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS messages (
	id          TEXT PRIMARY KEY,
	inbox       TEXT NOT NULL,
	env_from    TEXT,
	hdr_from    TEXT,
	hdr_to      TEXT,        -- JSON array
	subject     TEXT,
	message_id  TEXT,
	date        INTEGER,     -- unix seconds, header Date
	received_at INTEGER NOT NULL, -- unix seconds
	text_body   TEXT,
	html_body   TEXT,
	headers     TEXT,        -- JSON object
	size        INTEGER NOT NULL DEFAULT 0,
	raw         BLOB,        -- legacy inline source (old rows); new rows use raw_hash → raw_blobs
	raw_hash    TEXT         -- sha-256 of raw; references raw_blobs(hash). NULL for legacy rows.
);
-- Covering index for the newest-per-inbox lookup that the wait/list path hits.
CREATE INDEX IF NOT EXISTS idx_messages_inbox    ON messages(inbox, received_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_messages_received ON messages(received_at);
-- NB: the raw_hash column + its index are added after this batch (see migrate),
-- because CREATE TABLE IF NOT EXISTS won't add the column to a pre-existing
-- table, so indexing raw_hash inline would fail when upgrading an old database.

-- Content-addressed raw source. A message fanned out to N recipients, and any
-- forward job for it, all reference the same blob by hash — stored once.
CREATE TABLE IF NOT EXISTS raw_blobs (
	hash    TEXT PRIMARY KEY,
	content BLOB NOT NULL,
	size    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS attachments (
	id           TEXT PRIMARY KEY,
	message_id   TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	filename     TEXT,
	content_type TEXT,
	size         INTEGER NOT NULL DEFAULT 0,
	content      BLOB
);
CREATE INDEX IF NOT EXISTS idx_attachments_message ON attachments(message_id);

CREATE TABLE IF NOT EXISTS users (
	id            TEXT PRIMARY KEY,
	email         TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS api_keys (
	id           TEXT PRIMARY KEY,
	user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name         TEXT,
	key_hash     TEXT NOT NULL UNIQUE,
	scopes       TEXT NOT NULL DEFAULT 'read', -- CSV of scopes
	inbox_prefix TEXT NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys(user_id);

CREATE TABLE IF NOT EXISTS addresses (
	address         TEXT PRIMARY KEY,
	type            TEXT NOT NULL,
	owner_user_id   TEXT REFERENCES users(id) ON DELETE CASCADE,
	expires_at      INTEGER,                  -- NULL = permanent
	forward_to      TEXT NOT NULL DEFAULT '', -- CSV of destinations
	forward_enabled INTEGER NOT NULL DEFAULT 0,
	created_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_addresses_owner ON addresses(owner_user_id);

CREATE TABLE IF NOT EXISTS forward_jobs (
	id              TEXT PRIMARY KEY,
	message_id      TEXT,
	src_address     TEXT NOT NULL,
	dest            TEXT NOT NULL,
	raw             BLOB,
	attempts        INTEGER NOT NULL DEFAULT 0,
	next_attempt_at INTEGER NOT NULL,
	status          TEXT NOT NULL DEFAULT 'pending',
	last_error      TEXT,
	created_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_forward_jobs_due ON forward_jobs(status, next_attempt_at);

CREATE TABLE IF NOT EXISTS mailbox_verifications (
	dest        TEXT PRIMARY KEY,
	user_id     TEXT NOT NULL,
	token       TEXT NOT NULL UNIQUE,
	verified_at INTEGER,
	created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

-- Full-text index over the searchable columns. Kept in sync explicitly by
-- SaveMessage / delete paths (a standalone contentless-style table keyed by the
-- message id we store in an UNINDEXED column). Replaces the old LIKE '%q%' scan.
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
	id UNINDEXED, subject, hdr_from, inbox, text_body,
	tokenize = 'unicode61'
);
`
	if _, err := s.w.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Add the content-addressed raw_hash column to databases created before it
	// existed. CREATE TABLE IF NOT EXISTS above is a no-op on an existing table,
	// so the column must be added explicitly; then it is safe to index.
	if err := s.ensureColumn(ctx, "messages", "raw_hash", "raw_hash TEXT"); err != nil {
		return fmt.Errorf("migrate: add messages.raw_hash: %w", err)
	}
	if _, err := s.w.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_messages_raw_hash ON messages(raw_hash)`); err != nil {
		return fmt.Errorf("migrate: index raw_hash: %w", err)
	}

	// One-time backfill: if the FTS index is empty but messages exist (e.g. an
	// upgrade of an existing database), index what's already there.
	var ftsCount, msgCount int
	_ = s.w.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages_fts`).Scan(&ftsCount)
	_ = s.w.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages`).Scan(&msgCount)
	if ftsCount == 0 && msgCount > 0 {
		if _, err := s.w.ExecContext(ctx, `
INSERT INTO messages_fts (id, subject, hdr_from, inbox, text_body)
SELECT id, subject, hdr_from, inbox, text_body FROM messages`); err != nil {
			return fmt.Errorf("migrate: backfill fts: %w", err)
		}
	}
	return nil
}

// ensureColumn adds a column to a table if it is not already present, so old
// databases pick up columns introduced in newer schema versions. It is a no-op
// when the column already exists (idempotent across restarts).
func (s *Store) ensureColumn(ctx context.Context, table, column, ddl string) error {
	rows, err := s.w.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == column {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.w.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+ddl)
	return err
}

// SaveMessage persists a message and its attachments in one transaction. The
// raw source is stored once, content-addressed by sha-256 in raw_blobs, so a
// fan-out to several recipients (and any forward job) share a single blob.
func (s *Store) SaveMessage(ctx context.Context, m *model.Message) error {
	toJSON, _ := json.Marshal(m.To)
	hdrJSON, _ := json.Marshal(m.Headers)

	var rawHash any
	if len(m.Raw) > 0 {
		sum := sha256.Sum256(m.Raw)
		rawHash = hex.EncodeToString(sum[:])
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if rawHash != nil {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO raw_blobs (hash, content, size) VALUES (?,?,?) ON CONFLICT(hash) DO NOTHING`,
			rawHash, m.Raw, len(m.Raw)); err != nil {
			return fmt.Errorf("insert raw blob: %w", err)
		}
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO messages
	(id, inbox, env_from, hdr_from, hdr_to, subject, message_id, date, received_at, text_body, html_body, headers, size, raw, raw_hash)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,NULL,?)`,
		m.ID, m.Inbox, m.EnvFrom, m.From, string(toJSON), m.Subject, m.MessageID,
		nullableUnix(m.Date), m.ReceivedAt.Unix(), m.Text, m.HTML, string(hdrJSON), m.Size, rawHash,
	)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO messages_fts (id, subject, hdr_from, inbox, text_body) VALUES (?,?,?,?,?)`,
		m.ID, m.Subject, m.From, m.Inbox, m.Text); err != nil {
		return fmt.Errorf("index message: %w", err)
	}

	for i := range m.Attachments {
		a := &m.Attachments[i]
		if a.ID == "" {
			continue // ID must be assigned by caller; skip rather than corrupt
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO attachments (id, message_id, filename, content_type, size, content)
VALUES (?,?,?,?,?,?)`,
			a.ID, m.ID, a.Filename, a.ContentType, a.Size, a.Content,
		); err != nil {
			return fmt.Errorf("insert attachment: %w", err)
		}
	}

	return tx.Commit()
}

// ListInboxes returns one summary row per inbox that has received mail.
func (s *Store) ListInboxes(ctx context.Context) ([]model.InboxSummary, error) {
	rows, err := s.r.QueryContext(ctx, `
SELECT inbox, COUNT(*), MAX(received_at)
FROM messages
GROUP BY inbox
ORDER BY MAX(received_at) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.InboxSummary
	for rows.Next() {
		var s model.InboxSummary
		var last int64
		if err := rows.Scan(&s.Inbox, &s.MessageCount, &last); err != nil {
			return nil, err
		}
		s.LastReceived = time.Unix(last, 0).UTC()
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListMessages returns message metadata for an inbox, newest first. Bodies and
// raw source are omitted; use GetMessage for the full payload.
func (s *Store) ListMessages(ctx context.Context, inbox string, limit, offset int) ([]*model.Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.r.QueryContext(ctx, `
SELECT id, inbox, env_from, hdr_from, hdr_to, subject, message_id, date, received_at, size
FROM messages
WHERE inbox = ?
ORDER BY received_at DESC, id DESC
LIMIT ? OFFSET ?`, inbox, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMetaRows(rows)
}

// scanMetaRows materializes message-metadata rows (the shared column list used
// by ListMessages and SearchMessages). Bodies and raw are not selected.
func scanMetaRows(rows *sql.Rows) ([]*model.Message, error) {
	var out []*model.Message
	for rows.Next() {
		m := &model.Message{}
		var toJSON string
		var date sql.NullInt64
		var recv int64
		if err := rows.Scan(&m.ID, &m.Inbox, &m.EnvFrom, &m.From, &toJSON, &m.Subject, &m.MessageID, &date, &recv, &m.Size); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(toJSON), &m.To)
		if date.Valid {
			m.Date = time.Unix(date.Int64, 0).UTC()
		}
		m.ReceivedAt = time.Unix(recv, 0).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// ftsQuery turns free-form user input into a safe FTS5 MATCH expression: each
// alphanumeric token becomes a prefix term (token*) AND-ed together, so "acme
// cod" matches "Acme verification code". Returns "" when nothing is searchable.
func ftsQuery(q string) string {
	var terms []string
	for _, tok := range strings.FieldsFunc(q, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) {
		terms = append(terms, `"`+tok+`"*`)
	}
	return strings.Join(terms, " ")
}

// SearchMessages does a full-text search across subject, sender, inbox, and
// text body over every inbox, newest first, backed by the FTS5 index.
func (s *Store) SearchMessages(ctx context.Context, q string, limit int) ([]*model.Message, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []*model.Message{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	match := ftsQuery(q)
	if match == "" {
		return []*model.Message{}, nil
	}
	rows, err := s.r.QueryContext(ctx, `
SELECT m.id, m.inbox, m.env_from, m.hdr_from, m.hdr_to, m.subject, m.message_id, m.date, m.received_at, m.size
FROM messages m
WHERE m.id IN (SELECT id FROM messages_fts WHERE messages_fts MATCH ?1)
ORDER BY m.received_at DESC, m.id DESC
LIMIT ?2`, match, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMetaRows(rows)
}

// LatestMessageID returns the id of the newest message in inbox, or "" if the
// inbox is empty or its newest id does not sort strictly after `after`. It reads
// only the id (no bodies/blobs), so long-poll waiters can cheaply check for a
// new arrival before doing a full GetMessage. Relies on time-ordered ULID ids.
func (s *Store) LatestMessageID(ctx context.Context, inbox, after string) (string, error) {
	var id string
	err := s.r.QueryRowContext(ctx, `
SELECT id FROM messages WHERE inbox = ? ORDER BY received_at DESC, id DESC LIMIT 1`, inbox).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if after != "" && id <= after {
		return "", nil
	}
	return id, nil
}

// GetMessage returns a message's bodies, headers, and attachment *metadata*.
// It deliberately does NOT load the raw source or attachment content — those are
// large BLOBs fetched lazily via GetRaw / GetAttachment only when a client
// actually downloads them. This keeps the hot read/wait path cheap.
func (s *Store) GetMessage(ctx context.Context, msgID string) (*model.Message, error) {
	m := &model.Message{}
	var toJSON, hdrJSON string
	var date sql.NullInt64
	var recv int64
	err := s.r.QueryRowContext(ctx, `
SELECT id, inbox, env_from, hdr_from, hdr_to, subject, message_id, date, received_at, text_body, html_body, headers, size
FROM messages WHERE id = ?`, msgID).Scan(
		&m.ID, &m.Inbox, &m.EnvFrom, &m.From, &toJSON, &m.Subject, &m.MessageID,
		&date, &recv, &m.Text, &m.HTML, &hdrJSON, &m.Size,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(toJSON), &m.To)
	_ = json.Unmarshal([]byte(hdrJSON), &m.Headers)
	if date.Valid {
		m.Date = time.Unix(date.Int64, 0).UTC()
	}
	m.ReceivedAt = time.Unix(recv, 0).UTC()

	rows, err := s.r.QueryContext(ctx, `
SELECT id, filename, content_type, size FROM attachments WHERE message_id = ?`, msgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a model.Attachment
		if err := rows.Scan(&a.ID, &a.Filename, &a.ContentType, &a.Size); err != nil {
			return nil, err
		}
		m.Attachments = append(m.Attachments, a)
	}
	return m, rows.Err()
}

// GetRaw returns the verbatim RFC 5322 source for a message, resolving the
// content-addressed blob (new rows) or the legacy inline column (old rows).
func (s *Store) GetRaw(ctx context.Context, msgID string) ([]byte, error) {
	var raw []byte
	var hash sql.NullString
	err := s.r.QueryRowContext(ctx, `SELECT raw, raw_hash FROM messages WHERE id = ?`, msgID).Scan(&raw, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if hash.Valid && hash.String != "" {
		var content []byte
		err := s.r.QueryRowContext(ctx, `SELECT content FROM raw_blobs WHERE hash = ?`, hash.String).Scan(&content)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, storage.ErrNotFound
		}
		return content, err
	}
	return raw, nil
}

// GetAttachment loads a single attachment's content by message and attachment id.
func (s *Store) GetAttachment(ctx context.Context, msgID, attID string) (*model.Attachment, error) {
	a := &model.Attachment{}
	err := s.r.QueryRowContext(ctx, `
SELECT id, filename, content_type, size, content FROM attachments WHERE message_id = ? AND id = ?`,
		msgID, attID).Scan(&a.ID, &a.Filename, &a.ContentType, &a.Size, &a.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// DeleteMessage removes a message; attachments cascade via foreign key. The FTS
// row and any now-orphaned raw blob are cleaned up too.
func (s *Store) DeleteMessage(ctx context.Context, msgID string) error {
	res, err := s.w.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, msgID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrNotFound
	}
	_, _ = s.w.ExecContext(ctx, `DELETE FROM messages_fts WHERE id = ?`, msgID)
	s.gcRawBlobs(ctx)
	return nil
}

// DeleteInbox removes all messages for an inbox; attachments cascade.
func (s *Store) DeleteInbox(ctx context.Context, inbox string) (int64, error) {
	res, err := s.w.ExecContext(ctx, `DELETE FROM messages WHERE inbox = ?`, inbox)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	_, _ = s.w.ExecContext(ctx, `DELETE FROM messages_fts WHERE inbox = ?`, inbox)
	s.gcRawBlobs(ctx)
	return n, nil
}

// gcRawBlobs deletes content-addressed blobs no longer referenced by any
// message. Best-effort: failure only leaves reclaimable space behind.
func (s *Store) gcRawBlobs(ctx context.Context) {
	_, _ = s.w.ExecContext(ctx, `
DELETE FROM raw_blobs
WHERE hash NOT IN (SELECT raw_hash FROM messages WHERE raw_hash IS NOT NULL)`)
}

// Close closes both connection pools.
func (s *Store) Close() error {
	err := s.r.Close()
	if werr := s.w.Close(); werr != nil {
		return werr
	}
	return err
}

func nullableUnix(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

// ensure interface compliance at compile time.
var _ storage.Store = (*Store)(nil)
