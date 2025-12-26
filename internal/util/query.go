package util

import (
	"fmt"
	"strconv"

	"github.com/mr-karan/logchef/pkg/models"
)

// ExtractFirstNumeric extracts a numeric value from the first column of the first row
// in a query result. If the query returns no rows, it returns 0.
func ExtractFirstNumeric(result *models.QueryResult) (float64, error) {
	if result == nil || len(result.Logs) == 0 {
		return 0, nil
	}
	if len(result.Columns) == 0 {
		return 0, fmt.Errorf("query returned no columns")
	}

	rawValue, err := findFirstValue(result)
	if err != nil {
		return 0, err
	}

	return convertToFloat64(rawValue)
}

func findFirstValue(result *models.QueryResult) (any, error) {
	row := result.Logs[0]
	firstColumn := result.Columns[0].Name

	if val, ok := row[firstColumn]; ok {
		return val, nil
	}

	for _, v := range row {
		return v, nil
	}

	return nil, fmt.Errorf("unable to locate numeric value in query result")
}

func convertToFloat64(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case *float64:
		return derefFloat64(val)
	case float32:
		return float64(val), nil
	case *float32:
		return derefFloat32(val)
	case int:
		return float64(val), nil
	case *int:
		return derefInt(val)
	case int64:
		return float64(val), nil
	case *int64:
		return derefInt64(val)
	case uint64:
		return float64(val), nil
	case *uint64:
		return derefUint64(val)
	case uint32:
		return float64(val), nil
	case *uint32:
		return derefUint32(val)
	case string:
		parsed, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, fmt.Errorf("unable to parse numeric value %q: %w", val, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported result type %T", v)
	}
}

func derefFloat64(p *float64) (float64, error) {
	if p == nil {
		return 0, fmt.Errorf("nil float64 pointer in result")
	}
	return *p, nil
}

func derefFloat32(p *float32) (float64, error) {
	if p == nil {
		return 0, fmt.Errorf("nil float32 pointer in result")
	}
	return float64(*p), nil
}

func derefInt(p *int) (float64, error) {
	if p == nil {
		return 0, fmt.Errorf("nil int pointer in result")
	}
	return float64(*p), nil
}

func derefInt64(p *int64) (float64, error) {
	if p == nil {
		return 0, fmt.Errorf("nil int64 pointer in result")
	}
	return float64(*p), nil
}

func derefUint64(p *uint64) (float64, error) {
	if p == nil {
		return 0, fmt.Errorf("nil uint64 pointer in result")
	}
	return float64(*p), nil
}

func derefUint32(p *uint32) (float64, error) {
	if p == nil {
		return 0, fmt.Errorf("nil uint32 pointer in result")
	}
	return float64(*p), nil
}
