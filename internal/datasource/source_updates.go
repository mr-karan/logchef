package datasource

import (
	"strings"

	"github.com/mr-karan/logchef/pkg/models"
)

type SourceUpdateResult struct {
	Source       *models.Source
	Changed      bool
	Reinitialize bool
}

func ApplyCommonSourceUpdates(source *models.Source, req *models.UpdateSourceRequest) (bool, error) {
	if source == nil {
		return false, nil
	}
	if req == nil {
		return false, nil
	}

	changed := false

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return false, &ValidationError{Field: "name", Message: "source name is required"}
		}
		if !isValidSourceName(name) {
			return false, &ValidationError{Field: "name", Message: "source name must not exceed 50 characters and can only contain letters, numbers, spaces, hyphens, and underscores"}
		}
		if name != source.Name {
			source.Name = name
			changed = true
		}
	}

	if req.Description != nil {
		description := strings.TrimSpace(*req.Description)
		if len(description) > 500 {
			return false, &ValidationError{Field: "description", Message: "description must not exceed 500 characters"}
		}
		if description != source.Description {
			source.Description = description
			changed = true
		}
	}

	if req.TTLDays != nil {
		if *req.TTLDays < -1 {
			return false, &ValidationError{Field: "ttl_days", Message: "TTL days must be -1 (no TTL) or a non-negative number"}
		}
		if *req.TTLDays != source.TTLDays {
			source.TTLDays = *req.TTLDays
			changed = true
		}
	}

	if req.MetaTSField != nil {
		metaTSField := strings.TrimSpace(*req.MetaTSField)
		if err := validateColumnName("meta_ts_field", metaTSField); err != nil {
			return false, err
		}
		if metaTSField != source.MetaTSField {
			source.MetaTSField = metaTSField
			changed = true
		}
	}

	if req.MetaSeverityField != nil {
		metaSeverityField := strings.TrimSpace(*req.MetaSeverityField)
		if err := validateOptionalColumnName("meta_severity_field", metaSeverityField); err != nil {
			return false, err
		}
		if metaSeverityField != source.MetaSeverityField {
			source.MetaSeverityField = metaSeverityField
			changed = true
		}
	}

	return changed, nil
}
