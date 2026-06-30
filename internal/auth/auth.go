// Package auth holds Zorail's credential primitives: API-key generation and
// hashing, and password hashing. Storage persists only hashes; plaintext
// secrets exist just long enough to be returned to the caller once.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// KeyPrefix marks Zorail API keys so they are recognizable in logs and configs.
const KeyPrefix = "zk_"

var keyEnc = base32.NewEncoding("0123456789abcdefghjkmnpqrstvwxyz").WithPadding(base32.NoPadding)

// NewKey returns a fresh API-key secret (the plaintext, shown once) and its
// sha-256 hash (what storage keeps). The secret is `zk_` + 26 base32 chars.
func NewKey() (secret, hash string) {
	var b [16]byte
	_, _ = rand.Read(b[:])
	secret = KeyPrefix + keyEnc.EncodeToString(b[:])
	return secret, HashKey(secret)
}

// HashKey returns the lowercase hex sha-256 of a key secret. Deterministic, so
// lookups hash the presented key and match against the stored hash.
func HashKey(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

// LooksLikeKey reports whether s has the Zorail key prefix.
func LooksLikeKey(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), KeyPrefix)
}

// ConstantEqual compares two hashes without leaking timing.
func ConstantEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// HashPassword returns a bcrypt hash suitable for storage.
func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

// CheckPassword reports whether pw matches the stored bcrypt hash.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}
