package models

import (
	"errors"
	"fmt"
)

// ErrorType represents the type of error that occurred
type ErrorType string

// Common error types
const (
	// ValidationErrorType indicates a validation error
	ValidationErrorType ErrorType = "ValidationError"

	// NotFoundErrorType indicates a resource was not found
	NotFoundErrorType ErrorType = "NotFoundError"

	// AuthenticationErrorType indicates an authentication error
	AuthenticationErrorType ErrorType = "AuthenticationError"

	// AuthorizationErrorType indicates an authorization error
	AuthorizationErrorType ErrorType = "AuthorizationError"

	// ConflictErrorType indicates a resource conflict (e.g., already exists)
	ConflictErrorType ErrorType = "ConflictError"

	// DatabaseErrorType indicates a database error
	DatabaseErrorType ErrorType = "DatabaseError"

	// ExternalServiceErrorType indicates an error with an external service
	ExternalServiceErrorType ErrorType = "ExternalServiceError"

	// GeneralErrorType is a fallback for general errors
	GeneralErrorType ErrorType = "GeneralError"

	// DemoInstanceErrorType indicates an operation not permitted in demo mode
	DemoInstanceErrorType ErrorType = "DEMO_INSTANCE"

	// ManagedResourceErrorType indicates a mutation attempted on a config-managed resource
	ManagedResourceErrorType ErrorType = "ManagedResourceError"
)

// ErrorResponse represents a standardized error response
type ErrorResponse struct {
	Message string    `json:"message"`
	Type    ErrorType `json:"error_type"`
	Details any       `json:"details,omitempty"`
}

// Error definitions. These are the canonical, backend-neutral sentinels that
// every store implementation (SQLite, Postgres) translates its driver errors
// into, so callers branch with errors.Is without importing a backend package.
var (
	// ErrNotFound is returned when a resource is not found.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned when a write violates a uniqueness or other
	// constraint (e.g. a duplicate key, or the one-personal-collection-per-user
	// partial index).
	ErrConflict = errors.New("conflict")
	// ErrUserNotFound is a not-found specialized for users; it wraps ErrNotFound
	// so errors.Is(err, ErrNotFound) holds.
	ErrUserNotFound = fmt.Errorf("%w: user", ErrNotFound)
	// ErrTeamNotFound is a not-found specialized for teams; it wraps ErrNotFound.
	ErrTeamNotFound = fmt.Errorf("%w: team", ErrNotFound)
)

// IsNotFound reports whether err is (or wraps) ErrNotFound. Use this instead of
// importing a backend package: every store translates its driver's no-rows
// error into ErrNotFound, so this is the one backend-neutral not-found check.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// IsConflict reports whether err is (or wraps) ErrConflict — a uniqueness or
// other constraint violation, translated identically by every store backend.
func IsConflict(err error) bool { return errors.Is(err, ErrConflict) }
