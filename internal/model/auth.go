package model

import "time"

// User is an authenticated account that can reserve addresses and mint API
// keys. Authentication is local password for v1 (see internal/auth).
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"` // bcrypt; never serialized
	CreatedAt    time.Time `json:"created_at"`
}

// Scope is a coarse capability granted to an API key.
type Scope string

const (
	ScopeRead   Scope = "read"   // list/read/search messages within the key's reach
	ScopeManage Scope = "manage" // reserve/release addresses, configure forwarding, mint read keys
	ScopeAdmin  Scope = "admin"  // everything, across all users (legacy global token maps here)
)

// APIKey is a credential owned by a user. Only KeyHash is persisted; the secret
// is shown exactly once at creation.
type APIKey struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	KeyHash     string    `json:"-"`            // sha-256 hex of the secret
	Scopes      []Scope   `json:"scopes"`       // granted capabilities
	InboxPrefix string    `json:"inbox_prefix"` // "" = any address in the key owner's reach
	CreatedAt   time.Time `json:"created_at"`
	LastUsedAt  time.Time `json:"last_used_at,omitempty"`

	// Secret is populated only on the create response; never loaded from storage.
	Secret string `json:"secret,omitempty"`
}

// Has reports whether the key carries scope s (admin implies all).
func (k *APIKey) Has(s Scope) bool {
	for _, have := range k.Scopes {
		if have == ScopeAdmin || have == s {
			return true
		}
	}
	return false
}

// Allows reports whether this key may act on the given normalized inbox/address,
// honoring its inbox-prefix scope. Admin keys allow everything.
func (k *APIKey) Allows(inbox string) bool {
	if k.Has(ScopeAdmin) || k.InboxPrefix == "" {
		return true
	}
	return len(inbox) >= len(k.InboxPrefix) && inbox[:len(k.InboxPrefix)] == k.InboxPrefix
}
