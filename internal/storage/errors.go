package storage

import "errors"

// ErrNotFound is returned when a requested message does not exist.
var ErrNotFound = errors.New("storage: not found")
