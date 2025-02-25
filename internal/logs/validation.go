package logs

import (
	"fmt"
	"strings"

	"logchef/pkg/models"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validator provides validation logic for log operations
type Validator struct{}

// NewValidator creates a new logs validator
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateLogQueryRequest validates a log query request
func (v *Validator) ValidateLogQueryRequest(req *models.LogQueryRequest) error {
	if req.StartTimestamp <= 0 {
		return &ValidationError{
			Field:   "StartTimestamp",
			Message: "must be greater than 0",
		}
	}

	if req.EndTimestamp <= 0 {
		return &ValidationError{
			Field:   "EndTimestamp",
			Message: "must be greater than 0",
		}
	}

	if req.EndTimestamp < req.StartTimestamp {
		return &ValidationError{
			Field:   "EndTimestamp",
			Message: "must be greater than StartTimestamp",
		}
	}

	if req.Limit <= 0 || req.Limit > 100000 {
		return &ValidationError{
			Field:   "Limit",
			Message: "must be between 1 and 100000",
		}
	}

	if req.RawSQL == "" {
		return &ValidationError{
			Field:   "RawSQL",
			Message: "raw SQL query is required",
		}
	}

	// Basic SQL injection prevention - only allow SELECT queries
	if !strings.HasPrefix(strings.TrimSpace(strings.ToUpper(req.RawSQL)), "SELECT") {
		return &ValidationError{
			Field:   "RawSQL",
			Message: "only SELECT queries are allowed",
		}
	}

	if req.Sort != nil {
		if req.Sort.Field == "" {
			return &ValidationError{
				Field:   "Sort.Field",
				Message: "field is required when sort is specified",
			}
		}

		if req.Sort.Order != models.SortOrderAsc && req.Sort.Order != models.SortOrderDesc {
			return &ValidationError{
				Field:   "Sort.Order",
				Message: "must be either 'ASC' or 'DESC'",
			}
		}
	}

	return nil
}

// ValidateLogContextRequest validates a log context request
func (v *Validator) ValidateLogContextRequest(req *models.LogContextRequest) error {
	if req.SourceID == "" {
		return &ValidationError{
			Field:   "source_id",
			Message: "source ID is required",
		}
	}

	if req.Timestamp <= 0 {
		return &ValidationError{
			Field:   "timestamp",
			Message: "timestamp must be greater than 0",
		}
	}

	if req.BeforeLimit <= 0 && req.AfterLimit <= 0 {
		return &ValidationError{
			Field:   "limits",
			Message: "at least one of before_limit or after_limit must be greater than 0",
		}
	}

	if req.BeforeLimit < 0 {
		return &ValidationError{
			Field:   "before_limit",
			Message: "before_limit cannot be negative",
		}
	}

	if req.AfterLimit < 0 {
		return &ValidationError{
			Field:   "after_limit",
			Message: "after_limit cannot be negative",
		}
	}

	if req.BeforeLimit > 1000 {
		return &ValidationError{
			Field:   "before_limit",
			Message: "before_limit cannot be greater than 1000",
		}
	}

	if req.AfterLimit > 1000 {
		return &ValidationError{
			Field:   "after_limit",
			Message: "after_limit cannot be greater than 1000",
		}
	}

	return nil
}
