package service

import (
	"fmt"
	"strings"

	"backend-v2/pkg/models"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateLogQueryRequest validates a log query request
func ValidateLogQueryRequest(req *models.LogQueryRequest) error {
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

	if req.Limit <= 0 || req.Limit > 1000 {
		return &ValidationError{
			Field:   "Limit",
			Message: "must be between 1 and 1000",
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

	// Mode is mandatory
	if req.Mode == "" {
		return &ValidationError{
			Field:   "Mode",
			Message: "query mode is required",
		}
	}

	// Validate based on mode
	switch req.Mode {
	case models.QueryModeFilters:
		if len(req.FilterGroups) == 0 {
			return &ValidationError{
				Field:   "FilterGroups",
				Message: "must have at least one filter group in filters mode",
			}
		}
		// Validate filter groups
		for i, group := range req.FilterGroups {
			if group.Operator != models.GroupOperatorAnd && group.Operator != models.GroupOperatorOr {
				return &ValidationError{
					Field:   fmt.Sprintf("FilterGroups[%d].Operator", i),
					Message: "must be either 'AND' or 'OR'",
				}
			}

			if len(group.Conditions) == 0 {
				return &ValidationError{
					Field:   fmt.Sprintf("FilterGroups[%d].Conditions", i),
					Message: "must have at least one condition",
				}
			}

			// Validate conditions within the group
			for j, condition := range group.Conditions {
				if condition.Field == "" {
					return &ValidationError{
						Field:   fmt.Sprintf("FilterGroups[%d].Conditions[%d].Field", i, j),
						Message: "field is required",
					}
				}

				if condition.Operator == "" {
					return &ValidationError{
						Field:   fmt.Sprintf("FilterGroups[%d].Conditions[%d].Operator", i, j),
						Message: "operator is required",
					}
				}

				validOps := map[models.FilterOperator]bool{
					models.FilterOperatorEquals:        true,
					models.FilterOperatorNotEquals:     true,
					models.FilterOperatorContains:      true,
					models.FilterOperatorNotContains:   true,
					models.FilterOperatorGreaterThan:   true,
					models.FilterOperatorLessThan:      true,
					models.FilterOperatorGreaterEquals: true,
					models.FilterOperatorLessEquals:    true,
				}

				if !validOps[condition.Operator] {
					return &ValidationError{
						Field:   fmt.Sprintf("FilterGroups[%d].Conditions[%d].Operator", i, j),
						Message: "invalid operator",
					}
				}
			}
		}

	case models.QueryModeRawSQL:
		if req.RawSQL == "" {
			return &ValidationError{
				Field:   "RawSQL",
				Message: "raw SQL query is required in raw_sql mode",
			}
		}
		// Basic SQL injection prevention - only allow SELECT queries
		if !strings.HasPrefix(strings.TrimSpace(strings.ToUpper(req.RawSQL)), "SELECT") {
			return &ValidationError{
				Field:   "RawSQL",
				Message: "only SELECT queries are allowed",
			}
		}

	case models.QueryModeLogChefQL:
		if req.LogChefQL == "" {
			return &ValidationError{
				Field:   "LogChefQL",
				Message: "LogChefQL query is required in logchefql mode",
			}
		}

	default:
		return &ValidationError{
			Field:   "Mode",
			Message: "invalid query mode",
		}
	}

	return nil
}

// ValidateCreateSourceRequest validates a create source request
func ValidateCreateSourceRequest(tableName, schemaType, dsn, description string, ttlDays int) error {
	if tableName == "" {
		return &ValidationError{
			Field:   "TableName",
			Message: "table name is required",
		}
	}

	if len(tableName) < 3 {
		return &ValidationError{
			Field:   "TableName",
			Message: "table name must be at least 3 characters long",
		}
	}

	if schemaType == "" {
		return &ValidationError{
			Field:   "SchemaType",
			Message: "schema type is required",
		}
	}

	if schemaType != "managed" && schemaType != "unmanaged" {
		return &ValidationError{
			Field:   "SchemaType",
			Message: "schema type must be either 'managed' or 'unmanaged'",
		}
	}

	if dsn == "" {
		return &ValidationError{
			Field:   "DSN",
			Message: "dsn is required",
		}
	}

	if description == "" {
		return &ValidationError{
			Field:   "Description",
			Message: "description is required",
		}
	}

	if len(description) < 10 {
		return &ValidationError{
			Field:   "Description",
			Message: "description must be at least 10 characters long",
		}
	}

	if ttlDays <= 0 {
		return &ValidationError{
			Field:   "TTLDays",
			Message: "ttl days must be greater than 0",
		}
	}

	return nil
}
