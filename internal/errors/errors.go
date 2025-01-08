package errors

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// NewValidationError creates a new validation error
func NewValidationError(field, message string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
	}
}

// ConflictError represents a conflict error (e.g., resource already exists)
type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string {
	return e.Message
}

// NewConflictError creates a new conflict error
func NewConflictError(message string) *ConflictError {
	return &ConflictError{
		Message: message,
	}
}

// IsValidationError checks if the error is a validation error
func IsValidationError(err error) bool {
	_, ok := err.(*ValidationError)
	return ok
}

// IsConflictError checks if the error is a conflict error
func IsConflictError(err error) bool {
	_, ok := err.(*ConflictError)
	return ok
}
