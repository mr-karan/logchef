package util

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestExtractFirstNumeric(t *testing.T) {
	tests := []struct {
		name        string
		result      *models.QueryResult
		expected    float64
		shouldError bool
	}{
		{
			name: "float64 value",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]any{{"count": float64(42.5)}},
			},
			expected:    42.5,
			shouldError: false,
		},
		{
			name: "float64 pointer",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]any{{"count": new(24.576875029)}},
			},
			expected:    24.576875029,
			shouldError: false,
		},
		{
			name: "int64 value",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]any{{"count": int64(100)}},
			},
			expected:    100.0,
			shouldError: false,
		},
		{
			name: "int64 pointer",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]any{{"count": new(int64(200))}},
			},
			expected:    200.0,
			shouldError: false,
		},
		{
			name: "string numeric value",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]any{{"count": "123.45"}},
			},
			expected:    123.45,
			shouldError: false,
		},
		{
			name: "uint64 value",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]any{{"count": uint64(999)}},
			},
			expected:    999.0,
			shouldError: false,
		},
		{
			name: "nil float64 pointer",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]any{{"count": (*float64)(nil)}},
			},
			shouldError: true,
		},
		{
			name:        "nil result returns 0",
			result:      nil,
			expected:    0,
			shouldError: false,
		},
		{
			name: "empty logs returns 0",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]any{},
			},
			expected:    0,
			shouldError: false,
		},
		{
			name: "no columns",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{},
				Logs:    []map[string]any{{"count": float64(42)}},
			},
			shouldError: true,
		},
		{
			name: "invalid string value",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]any{{"count": "not-a-number"}},
			},
			shouldError: true,
		},
		{
			name: "unsupported type",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]any{{"count": []byte("hello")}},
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractFirstNumeric(tt.result)
			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestExtractFirstNumericDriverTypes(t *testing.T) {
	values := []any{
		int(12), int8(12), int16(12), int32(12), int64(12),
		uint(12), uint8(12), uint16(12), uint32(12), uint64(12),
		float32(12), float64(12), json.Number("12"), *big.NewInt(12),
		decimal.NewFromInt(12), decimal.NullDecimal{Decimal: decimal.NewFromInt(12), Valid: true},
	}
	for _, value := range values {
		for _, raw := range []any{value, numericPointer(value)} {
			t.Run(fmt.Sprintf("%T", raw), func(t *testing.T) {
				result := &models.QueryResult{Columns: []models.ColumnInfo{{Name: "value"}}, Logs: []map[string]any{{"value": raw}}}
				got, err := ExtractFirstNumeric(result)
				if err != nil || got != 12 {
					t.Fatalf("ExtractFirstNumeric(%T) = %v, %v; want 12", raw, got, err)
				}
			})
		}
	}
	for _, raw := range []any{nil, (*int16)(nil), false, struct{}{}, decimal.NullDecimal{}, math.NaN(), math.Inf(1), "NaN", "Inf"} {
		t.Run(fmt.Sprintf("reject %T %v", raw, raw), func(t *testing.T) {
			_, err := ExtractFirstNumeric(&models.QueryResult{Columns: []models.ColumnInfo{{Name: "value"}}, Logs: []map[string]any{{"value": raw}}})
			if err == nil {
				t.Fatalf("accepted non-finite or non-numeric value %v", raw)
			}
		})
	}
}

func numericPointer(value any) any {
	pointer := reflect.New(reflect.TypeOf(value))
	pointer.Elem().Set(reflect.ValueOf(value))
	return pointer.Interface()
}

func TestExtractFirstNumericMissingColumn(t *testing.T) {
	_, err := ExtractFirstNumeric(&models.QueryResult{Columns: []models.ColumnInfo{{Name: "missing"}}, Logs: []map[string]any{{"other": 12}}})
	if err == nil {
		t.Fatal("used another column when the first column is missing")
	}
}

func TestExtractFirstNumericFractionalAndWideValues(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  float64
	}{
		{float32(-12.5), -12.5},
		{decimal.New(125, -1), 12.5},
		{decimal.New(125, -2), 1.25},
		{uint64(math.MaxUint64), 18446744073709551616},
		{*new(big.Int).Lsh(big.NewInt(1), 100), 1267650600228229401496703205376},
	} {
		t.Run(fmt.Sprintf("%T %v", tc.value, tc.value), func(t *testing.T) {
			got, err := ExtractFirstNumeric(&models.QueryResult{Columns: []models.ColumnInfo{{Name: "value"}}, Logs: []map[string]any{{"value": tc.value}}})
			if err != nil || got != tc.want {
				t.Fatalf("value = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	for _, raw := range []any{decimal.New(1, 400), new(big.Int).Lsh(big.NewInt(1), 2000)} {
		if _, err := ExtractFirstNumeric(&models.QueryResult{Columns: []models.ColumnInfo{{Name: "value"}}, Logs: []map[string]any{{"value": raw}}}); err == nil {
			t.Fatal("accepted a number exceeding float64 range")
		}
	}
}
