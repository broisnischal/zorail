package model

import "time"

// AddressType is the behavior Zorail applies when mail arrives for an address.
type AddressType string

const (
	// AddrDisposable: catch-all, ephemeral, eligible for retention sweeping.
	// Disposable addresses usually have no registry row at all — they are
	// implied by the catch-all. A row exists only when one is explicitly
	// minted with a TTL.
	AddrDisposable AddressType = "disposable"
	// AddrReserved: permanent, owned by a user, never swept.
	AddrReserved AddressType = "reserved"
	// AddrForward: like reserved, but each arriving message is also enqueued
	// for delivery to ForwardTo when ForwardEnabled.
	AddrForward AddressType = "forward"
)

// Address is a registry entry that gives an inbox an owner and a behavior.
// Catch-all disposable mail needs no Address row; reserved and forwarding
// addresses must be claimed.
type Address struct {
	Address        string      `json:"address"`
	Type           AddressType `json:"type"`
	OwnerUserID    string      `json:"owner_user_id,omitempty"`
	ExpiresAt      *time.Time  `json:"expires_at,omitempty"` // nil = permanent
	ForwardTo      []string    `json:"forward_to,omitempty"`
	ForwardEnabled bool        `json:"forward_enabled"`
	CreatedAt      time.Time   `json:"created_at"`
}

// ForwardStatus tracks a queued outbound forward.
type ForwardStatus string

const (
	ForwardPending ForwardStatus = "pending"
	ForwardSent    ForwardStatus = "sent"
	ForwardFailed  ForwardStatus = "failed"
)

// ForwardJob is one queued delivery of a received message to one destination.
// Raw holds the verbatim source so the worker re-emits it unchanged (preserving
// the original DKIM signature).
type ForwardJob struct {
	ID            string        `json:"id"`
	MessageID     string        `json:"message_id"`
	SrcAddress    string        `json:"src_address"`
	Dest          string        `json:"dest"`
	Raw           []byte        `json:"-"`
	Attempts      int           `json:"attempts"`
	NextAttemptAt time.Time     `json:"next_attempt_at"`
	Status        ForwardStatus `json:"status"`
	LastError     string        `json:"last_error,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
}

// MailboxVerification records ownership proof for a forwarding destination, so
// Zorail never forwards to an unverified address (open-relay / backscatter
// protection, à la SimpleLogin).
type MailboxVerification struct {
	Dest       string     `json:"dest"`
	UserID     string     `json:"user_id"`
	Token      string     `json:"-"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}
