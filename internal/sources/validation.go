package sources

import (
	"fmt"
)

func validateSourceRequest(req CreateSourceRequest) error {
	if req.TTLDays == 0 {
		req.TTLDays = 90 // Default TTL
	}

	if req.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if req.SchemaType == "" {
		return fmt.Errorf("schema type cannot be empty")
	}
	if req.DSN == "" {
		return fmt.Errorf("dsn cannot be empty")
	}

	if len(req.Name) < 4 || len(req.Name) > 30 {
		return fmt.Errorf("name must be between 4 and 30 characters")
	}

	for _, char := range req.Name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_') {
			return fmt.Errorf("name must contain only alphanumeric characters and underscores")
		}
	}

	return nil
}
