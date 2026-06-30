package store

import "errors"

// Backend-neutral sentinel errors. Each store implementation translates its
// driver/dialect-specific errors into these so callers can branch with
// errors.Is without importing (or knowing) the underlying database.
var (
	// ErrNotFound is returned when a requested row does not exist.
	ErrNotFound = errors.New("store: not found")

	// ErrConflict is returned when a write violates a uniqueness or other
	// constraint (e.g. a duplicate key, or the one-personal-collection-per-user
	// partial index).
	ErrConflict = errors.New("store: conflict")
)
