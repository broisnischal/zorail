// Package storage defines the persistence contract for Zorail messages.
// The SMTP ingest layer depends only on the Store interface, so alternative
// backends (Postgres, object storage for raw blobs, etc.) can be added without
// touching ingest.
package storage

import (
	"context"
	"time"

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

	// GetMessage returns a message's bodies, headers, and attachment metadata by
	// ID, or ErrNotFound. Raw source and attachment content are NOT loaded (they
	// are large BLOBs fetched lazily via GetRaw / GetAttachment).
	GetMessage(ctx context.Context, id string) (*model.Message, error)

	// GetRaw returns the verbatim RFC 5322 source for a message, or ErrNotFound.
	GetRaw(ctx context.Context, id string) ([]byte, error)

	// GetAttachment returns a single attachment (including content) by message
	// and attachment id, or ErrNotFound.
	GetAttachment(ctx context.Context, msgID, attID string) (*model.Attachment, error)

	// LatestMessageID returns the newest message id in inbox, or "" when the
	// inbox is empty or its newest id does not sort strictly after `after`. Reads
	// only the id, so long-poll waiters can cheaply detect a new arrival.
	LatestMessageID(ctx context.Context, inbox, after string) (string, error)

	// DeleteMessage removes a message and its attachments.
	DeleteMessage(ctx context.Context, id string) error

	// DeleteInbox removes every message in an inbox and returns how many were
	// deleted.
	DeleteInbox(ctx context.Context, inbox string) (int64, error)

	// --- Identity & API keys ---

	// CreateUser persists a new user; returns ErrConflict if the email exists.
	CreateUser(ctx context.Context, u *model.User) error
	GetUserByEmail(ctx context.Context, email string) (*model.User, error)
	GetUserByID(ctx context.Context, id string) (*model.User, error)
	// CountUsers reports how many accounts exist (0 ⇒ instance needs setup).
	CountUsers(ctx context.Context) (int, error)

	// --- Instance settings (key/value) ---

	// GetSetting returns a stored value, or "" (no error) when the key is unset.
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error

	// CreateAPIKey persists a key (only its hash). Secret is not stored.
	CreateAPIKey(ctx context.Context, k *model.APIKey) error
	// GetAPIKeyByHash resolves a presented key (already hashed) to its record.
	GetAPIKeyByHash(ctx context.Context, hash string) (*model.APIKey, error)
	ListAPIKeys(ctx context.Context, userID string) ([]*model.APIKey, error)
	DeleteAPIKey(ctx context.Context, id, userID string) error

	// --- Address registry ---

	// UpsertAddress reserves or updates an address row.
	UpsertAddress(ctx context.Context, a *model.Address) error
	// GetAddress returns the registry row for a normalized address, or ErrNotFound.
	GetAddress(ctx context.Context, address string) (*model.Address, error)
	ListAddresses(ctx context.Context, userID string) ([]*model.Address, error)
	DeleteAddress(ctx context.Context, address, userID string) error

	// --- Forwarding ---

	EnqueueForward(ctx context.Context, j *model.ForwardJob) error
	// ClaimForwardJobs returns up to limit pending jobs whose next_attempt_at
	// has passed, marking them in-flight to avoid double-send across workers.
	ClaimForwardJobs(ctx context.Context, now time.Time, limit int) ([]*model.ForwardJob, error)
	MarkForwardSent(ctx context.Context, id string) error
	MarkForwardRetry(ctx context.Context, id string, attempts int, next time.Time, lastErr string) error
	MarkForwardFailed(ctx context.Context, id, lastErr string) error

	// --- Mailbox verification ---

	CreateVerification(ctx context.Context, v *model.MailboxVerification) error
	GetVerificationByToken(ctx context.Context, token string) (*model.MailboxVerification, error)
	MarkVerified(ctx context.Context, dest string, when time.Time) error
	IsVerified(ctx context.Context, dest string) (bool, error)

	// --- Retention ---

	// ExpireMessages deletes messages received before cutoff whose inbox is NOT
	// a reserved/forward address, and returns how many were removed.
	ExpireMessages(ctx context.Context, cutoff time.Time) (int64, error)

	// Close releases underlying resources.
	Close() error
}
