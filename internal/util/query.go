package util

import (
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/mr-karan/logchef/pkg/models"
)

// ExtractFirstNumeric reads the first column of the first row, or zero for no rows.
func ExtractFirstNumeric(result *models.QueryResult) (float64, error) {
	if result == nil || len(result.Logs) == 0 {
		return 0, nil
	}
	if len(result.Columns) == 0 {
		return 0, fmt.Errorf("query returned no columns")
	}
	column := result.Columns[0].Name
	raw, ok := result.Logs[0][column]
	if !ok {
		return 0, fmt.Errorf("query result is missing first column %q", column)
	}
	value, err := convertToFloat64(raw)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("query result must be a finite number")
	}
	return value, nil
}

// Alert thresholds use float64, so integers wider than 53 bits can lose precision.
func convertToFloat64(raw any) (float64, error) {
	value := reflect.ValueOf(raw)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, fmt.Errorf("nil numeric pointer in result (%T)", raw)
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return 0, fmt.Errorf("query result is null, expected a number")
	}
	switch number := value.Interface().(type) {
	case decimal.Decimal:
		result, _ := number.Float64()
		return result, nil
	case decimal.NullDecimal:
		if !number.Valid {
			return 0, fmt.Errorf("query result is null, expected a number")
		}
		result, _ := number.Decimal.Float64()
		return result, nil
	case big.Int:
		result, _ := number.Float64()
		return result, nil
	}
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(value.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(value.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return value.Float(), nil
	case reflect.String:
		parsed, err := strconv.ParseFloat(value.String(), 64)
		if err != nil {
			return 0, fmt.Errorf("unable to parse numeric value %q: %w", value.String(), err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported numeric result type %T", raw)
	}
}
