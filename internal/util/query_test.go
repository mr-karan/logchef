package util

import (
	"testing"

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
				Logs:    []map[string]interface{}{{"count": float64(42.5)}},
			},
			expected:    42.5,
			shouldError: false,
		},
		{
			name: "float64 pointer",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]interface{}{{"count": ptrFloat64(24.576875029)}},
			},
			expected:    24.576875029,
			shouldError: false,
		},
		{
			name: "int64 value",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]interface{}{{"count": int64(100)}},
			},
			expected:    100.0,
			shouldError: false,
		},
		{
			name: "int64 pointer",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]interface{}{{"count": ptrInt64(200)}},
			},
			expected:    200.0,
			shouldError: false,
		},
		{
			name: "string numeric value",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]interface{}{{"count": "123.45"}},
			},
			expected:    123.45,
			shouldError: false,
		},
		{
			name: "uint64 value",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]interface{}{{"count": uint64(999)}},
			},
			expected:    999.0,
			shouldError: false,
		},
		{
			name: "nil float64 pointer",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]interface{}{{"count": (*float64)(nil)}},
			},
			shouldError: true,
		},
		{
			name:        "nil result",
			result:      nil,
			shouldError: true,
		},
		{
			name: "empty logs",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]interface{}{},
			},
			shouldError: true,
		},
		{
			name: "no columns",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{},
				Logs:    []map[string]interface{}{{"count": float64(42)}},
			},
			shouldError: true,
		},
		{
			name: "invalid string value",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]interface{}{{"count": "not-a-number"}},
			},
			shouldError: true,
		},
		{
			name: "unsupported type",
			result: &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count"}},
				Logs:    []map[string]interface{}{{"count": []byte("hello")}},
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

// Helper functions to create pointers
func ptrFloat64(v float64) *float64 {
	return &v
}

func ptrInt64(v int64) *int64 {
	return &v
}
