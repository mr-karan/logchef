package dsl

import (
	"backend-v2/pkg/models"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseQuery(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		wantErr       bool
		errorContains string
		validate      func(t *testing.T, groups []models.FilterGroup)
	}{
		{
			name:    "simple equals condition",
			query:   `severity = "ERROR"`,
			wantErr: false,
			validate: func(t *testing.T, groups []models.FilterGroup) {
				assert.Len(t, groups, 1)
				assert.Len(t, groups[0].Conditions, 1)
				assert.Equal(t, "severity", groups[0].Conditions[0].Field)
				assert.Equal(t, models.FilterOperatorEquals, groups[0].Conditions[0].Operator)
				assert.Equal(t, "ERROR", groups[0].Conditions[0].Value)
			},
		},
		{
			name:    "multiple AND conditions",
			query:   `severity = "ERROR" AND duration_ms > 1000`,
			wantErr: false,
			validate: func(t *testing.T, groups []models.FilterGroup) {
				assert.Len(t, groups, 1)
				assert.Len(t, groups[0].Conditions, 2)
				assert.Equal(t, models.GroupOperatorAnd, groups[0].Operator)
				assert.Equal(t, "duration_ms", groups[0].Conditions[1].Field)
				assert.Equal(t, models.FilterOperatorGreaterThan, groups[0].Conditions[1].Operator)
				assert.Equal(t, "1000", groups[0].Conditions[1].Value)
			},
		},
		{
			name:    "multiple OR groups",
			query:   `(severity = "ERROR") OR (status_code >= 500)`,
			wantErr: false,
			validate: func(t *testing.T, groups []models.FilterGroup) {
				assert.Len(t, groups, 2)
				assert.Equal(t, "severity", groups[0].Conditions[0].Field)
				assert.Equal(t, "status_code", groups[1].Conditions[0].Field)
				assert.Equal(t, models.FilterOperatorGreaterEquals, groups[1].Conditions[0].Operator)
			},
		},
		{
			name:    "pattern matching",
			query:   `message =~ "timeout"`,
			wantErr: false,
			validate: func(t *testing.T, groups []models.FilterGroup) {
				assert.Len(t, groups, 1)
				assert.Equal(t, models.FilterOperatorContains, groups[0].Conditions[0].Operator)
				assert.Equal(t, "timeout", groups[0].Conditions[0].Value)
			},
		},
		{
			name:    "complex query with pattern",
			query:   `(severity = "ERROR" AND message =~ "timeout") OR (status_code >= 500)`,
			wantErr: false,
			validate: func(t *testing.T, groups []models.FilterGroup) {
				assert.Len(t, groups, 2)
				assert.Equal(t, models.FilterOperatorContains, groups[0].Conditions[1].Operator)
				assert.Equal(t, models.FilterOperatorGreaterEquals, groups[1].Conditions[0].Operator)
			},
		},
		{
			name:          "invalid operator",
			query:         `severity >>> "ERROR"`,
			wantErr:       true,
			errorContains: "unexpected token",
		},
		{
			name:          "unclosed parenthesis",
			query:         `(severity = "ERROR"`,
			wantErr:       true,
			errorContains: "unexpected token \"<EOF>\"",
		},
		{
			name:    "numeric comparison",
			query:   `duration_ms > 1000 AND status_code = 500`,
			wantErr: false,
			validate: func(t *testing.T, groups []models.FilterGroup) {
				assert.Equal(t, "1000", groups[0].Conditions[0].Value)
				assert.Equal(t, "500", groups[0].Conditions[1].Value)
			},
		},
		{
			name:    "mixed quotes in values",
			query:   `service = "auth-service" AND message =~ "error in process"`,
			wantErr: false,
			validate: func(t *testing.T, groups []models.FilterGroup) {
				if assert.Len(t, groups, 1) {
					assert.Equal(t, "error in process", groups[0].Conditions[1].Value)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups, err := ParseQuery(tt.query)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, groups)

			if tt.validate != nil {
				tt.validate(t, groups)
			}
		})
	}
}

// TestParseQueryEdgeCases tests edge cases and error conditions
func TestParseQueryEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		wantErr       bool
		errorContains string
	}{
		{
			name:          "empty query",
			query:         "",
			wantErr:       true,
			errorContains: "must match at least once",
		},
		{
			name:          "invalid field name",
			query:         `123field = "value"`,
			wantErr:       true,
			errorContains: "must match at least once",
		},
		{
			name:          "missing value",
			query:         `field =`,
			wantErr:       true,
			errorContains: "expected Value",
		},
		{
			name:          "invalid boolean operator",
			query:         `field = "value" XOR other = "value"`,
			wantErr:       true,
			errorContains: "unexpected token",
		},
		{
			name:          "unmatched quotes",
			query:         `field = "unclosed`,
			wantErr:       true,
			errorContains: "invalid input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseQuery(tt.query)
			assert.Error(t, err)
			if tt.errorContains != "" {
				assert.Contains(t, err.Error(), tt.errorContains)
			}
		})
	}
}
