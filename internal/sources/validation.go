package sources

import (
	"github.com/mr-karan/logchef/internal/errors"
)

func validateSourceRequest(req CreateSourceRequest) error {
	if req.TTLDays == 0 {
		req.TTLDays = 90 // Default TTL
	}

	if req.Name == "" {
		return errors.NewValidationError("name", "name cannot be empty")
	}
	if req.SchemaType == "" {
		return errors.NewValidationError("schema_type", "schema type cannot be empty")
	}
	if req.DSN == "" {
		return errors.NewValidationError("dsn", "dsn cannot be empty")
	}

	if len(req.Name) < 4 || len(req.Name) > 30 {
		return errors.NewValidationError("name", "name must be between 4 and 30 characters")
	}

	for _, char := range req.Name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_') {
			return errors.NewValidationError("name", "name must contain only alphanumeric characters and underscores")
		}
	}

	return nil
}
