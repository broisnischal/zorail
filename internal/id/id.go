// Package id generates Zorail's identifiers: lexicographically sortable,
// URL-safe, and collision-resistant without an external dependency.
package id

import (
	"crypto/rand"
	"encoding/base32"
	"time"
)

// crockford base32 without padding, lowercased — safe in URLs and shell.
var enc = base32.NewEncoding("0123456789abcdefghjkmnpqrstvwxyz").WithPadding(base32.NoPadding)

// New returns a 26-char identifier whose first bytes encode the creation time
// in milliseconds, so string ordering matches chronological ordering. The
// remaining bytes are random. This is a ULID-compatible layout built on the
// standard library only.
func New() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	// 48-bit big-endian timestamp in the leading 6 bytes.
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	// 80 bits of randomness.
	_, _ = rand.Read(b[6:])
	return enc.EncodeToString(b[:])
}
