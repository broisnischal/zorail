// Package sqlite implements storage.Store on top of a single SQLite database
// file using the pure-Go modernc.org/sqlite driver (no cgo), keeping Zorail a
// single self-contained static binary — ideal for self-hosting.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/storage"
)

// Store is a SQLite-backed storage.Store.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and applies the schema.
func Open(path string) (*Store, error) {
	// Pragmas: WAL for concurrent reads during ingest writes; busy_timeout so
	// concurrent writers wait rather than failing; foreign_keys for cascade.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single write connection avoids "database is locked" under WAL while
	// still allowing the driver's internal read handling.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
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
	raw         BLOB
);
CREATE INDEX IF NOT EXISTS idx_messages_inbox    ON messages(inbox);
CREATE INDEX IF NOT EXISTS idx_messages_received ON messages(received_at);

CREATE TABLE IF NOT EXISTS attachments (
	id           TEXT PRIMARY KEY,
	message_id   TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	filename     TEXT,
	content_type TEXT,
	size         INTEGER NOT NULL DEFAULT 0,
	content      BLOB
);
CREATE INDEX IF NOT EXISTS idx_attachments_message ON attachments(message_id);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// SaveMessage persists a message and its attachments in one transaction.
func (s *Store) SaveMessage(ctx context.Context, m *model.Message) error {
	toJSON, _ := json.Marshal(m.To)
	hdrJSON, _ := json.Marshal(m.Headers)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
INSERT INTO messages
	(id, inbox, env_from, hdr_from, hdr_to, subject, message_id, date, received_at, text_body, html_body, headers, size, raw)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.Inbox, m.EnvFrom, m.From, string(toJSON), m.Subject, m.MessageID,
		nullableUnix(m.Date), m.ReceivedAt.Unix(), m.Text, m.HTML, string(hdrJSON), m.Size, m.Raw,
	)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
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
	rows, err := s.db.QueryContext(ctx, `
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
	rows, err := s.db.QueryContext(ctx, `
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

// escapeLike escapes LIKE wildcards so user input is matched literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// SearchMessages does a substring search across subject, sender, and text body
// over every inbox, newest first.
func (s *Store) SearchMessages(ctx context.Context, q string, limit int) ([]*model.Message, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []*model.Message{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	like := "%" + escapeLike(q) + "%"
	rows, err := s.db.QueryContext(ctx, `
SELECT id, inbox, env_from, hdr_from, hdr_to, subject, message_id, date, received_at, size
FROM messages
WHERE subject LIKE ?1 ESCAPE '\'
   OR hdr_from LIKE ?1 ESCAPE '\'
   OR inbox LIKE ?1 ESCAPE '\'
   OR text_body LIKE ?1 ESCAPE '\'
ORDER BY received_at DESC, id DESC
LIMIT ?2`, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMetaRows(rows)
}

// GetMessage returns a fully-populated message including bodies, headers, raw,
// and attachment metadata (attachment content is loaded too for MVP).
func (s *Store) GetMessage(ctx context.Context, msgID string) (*model.Message, error) {
	m := &model.Message{}
	var toJSON, hdrJSON string
	var date sql.NullInt64
	var recv int64
	err := s.db.QueryRowContext(ctx, `
SELECT id, inbox, env_from, hdr_from, hdr_to, subject, message_id, date, received_at, text_body, html_body, headers, size, raw
FROM messages WHERE id = ?`, msgID).Scan(
		&m.ID, &m.Inbox, &m.EnvFrom, &m.From, &toJSON, &m.Subject, &m.MessageID,
		&date, &recv, &m.Text, &m.HTML, &hdrJSON, &m.Size, &m.Raw,
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

	rows, err := s.db.QueryContext(ctx, `
SELECT id, filename, content_type, size, content FROM attachments WHERE message_id = ?`, msgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a model.Attachment
		if err := rows.Scan(&a.ID, &a.Filename, &a.ContentType, &a.Size, &a.Content); err != nil {
			return nil, err
		}
		m.Attachments = append(m.Attachments, a)
	}
	return m, rows.Err()
}

// DeleteMessage removes a message; attachments cascade via foreign key.
func (s *Store) DeleteMessage(ctx context.Context, msgID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, msgID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// DeleteInbox removes all messages for an inbox; attachments cascade.
func (s *Store) DeleteInbox(ctx context.Context, inbox string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE inbox = ?`, inbox)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func nullableUnix(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

// ensure interface compliance at compile time.
var _ storage.Store = (*Store)(nil)
