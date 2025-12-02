package util

import (
	"fmt"
	"strconv"

	"github.com/mr-karan/logchef/pkg/models"
)

// ExtractFirstNumeric extracts a numeric value from the first column of the first row
// in a query result. It supports both value and pointer types for all numeric types.
// If the query returns no rows (e.g., no matching data for an aggregate query),
// it returns 0 instead of an error since this typically means "zero matches".
func ExtractFirstNumeric(result *models.QueryResult) (float64, error) {
	if result == nil || len(result.Logs) == 0 {
		// For aggregate queries like count(*), no rows typically means 0
		return 0, nil
	}
	row := result.Logs[0]
	if len(result.Columns) == 0 {
		return 0, fmt.Errorf("query returned no columns")
	}

	// Try the first column
	firstColumn := result.Columns[0].Name
	rawValue, ok := row[firstColumn]
	if !ok {
		// Fallback: try any value in the row
		for _, v := range row {
			rawValue = v
			ok = true
			break
		}
	}
	if !ok {
		return 0, fmt.Errorf("unable to locate numeric value in query result")
	}

	// Convert to float64, supporting both value and pointer types
	switch v := rawValue.(type) {
	case float64:
		return v, nil
	case *float64:
		if v != nil {
			return *v, nil
		}
		return 0, fmt.Errorf("nil float64 pointer in result")
	case float32:
		return float64(v), nil
	case *float32:
		if v != nil {
			return float64(*v), nil
		}
		return 0, fmt.Errorf("nil float32 pointer in result")
	case int:
		return float64(v), nil
	case *int:
		if v != nil {
			return float64(*v), nil
		}
		return 0, fmt.Errorf("nil int pointer in result")
	case int64:
		return float64(v), nil
	case *int64:
		if v != nil {
			return float64(*v), nil
		}
		return 0, fmt.Errorf("nil int64 pointer in result")
	case uint64:
		return float64(v), nil
	case *uint64:
		if v != nil {
			return float64(*v), nil
		}
		return 0, fmt.Errorf("nil uint64 pointer in result")
	case uint32:
		return float64(v), nil
	case *uint32:
		if v != nil {
			return float64(*v), nil
		}
		return 0, fmt.Errorf("nil uint32 pointer in result")
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("unable to parse numeric value %q: %w", v, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported result type %T", rawValue)
	}
}
