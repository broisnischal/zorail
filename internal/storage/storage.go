// Package storage defines the persistence contract for Zorail messages.
// The SMTP ingest layer depends only on the Store interface, so alternative
// backends (Postgres, object storage for raw blobs, etc.) can be added without
// touching ingest.
package storage

import (
	"context"

	"github.com/nees/zorail/internal/model"
)

// Store persists and retrieves received messages.
type Store interface {
	// SaveMessage persists one message and its attachments atomically.
	// The message must already have its ID, Inbox, and ReceivedAt set.
	SaveMessage(ctx context.Context, m *model.Message) error

	// ListInboxes returns a summary of every inbox that has received mail.
	ListInboxes(ctx context.Context) ([]model.InboxSummary, error)

	// ListMessages returns messages for an inbox, newest first.
	// Bodies and raw source are omitted for listing efficiency.
	ListMessages(ctx context.Context, inbox string, limit, offset int) ([]*model.Message, error)

	// SearchMessages returns messages across all inboxes matching q in the
	// subject, sender, or body, newest first. Metadata only (no bodies/raw).
	SearchMessages(ctx context.Context, q string, limit int) ([]*model.Message, error)

	// GetMessage returns a single fully-populated message (bodies + attachment
	// metadata) by ID, or ErrNotFound.
	GetMessage(ctx context.Context, id string) (*model.Message, error)

	// DeleteMessage removes a message and its attachments.
	DeleteMessage(ctx context.Context, id string) error

	// DeleteInbox removes every message in an inbox and returns how many were
	// deleted.
	DeleteInbox(ctx context.Context, inbox string) (int64, error)

	// Close releases underlying resources.
	Close() error
}
