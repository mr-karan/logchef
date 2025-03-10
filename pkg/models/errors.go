package models

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

	// DatabaseErrorType indicates a database error
	DatabaseErrorType ErrorType = "DatabaseError"

	// ExternalServiceErrorType indicates an error with an external service
	ExternalServiceErrorType ErrorType = "ExternalServiceError"

	// GeneralErrorType is a fallback for general errors
	GeneralErrorType ErrorType = "GeneralError"
)

// ErrorResponse represents a standardized error response
type ErrorResponse struct {
	Message string    `json:"message"`
	Type    ErrorType `json:"error_type"`
	Details any       `json:"details,omitempty"`
}

// NewErrorResponse creates a new error response
func NewErrorResponse(message string, errorType ErrorType, details any) ErrorResponse {
	return ErrorResponse{
		Message: message,
		Type:    errorType,
		Details: details,
	}
}
