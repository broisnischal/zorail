package storage

import "errors"

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("storage: not found")

// ErrConflict is returned when a unique constraint would be violated (e.g. an
// email or address that is already taken).
var ErrConflict = errors.New("storage: conflict")
