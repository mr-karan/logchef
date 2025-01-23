package querybuilder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRawSQLBuilder_Build(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		tableRef    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid simple select",
			sql:      "SELECT * FROM `default`.`logs`",
			tableRef: "default.logs",
			wantErr:  false,
		},
		{
			name:        "invalid non-select query",
			sql:         "DELETE FROM `default`.`logs`",
			tableRef:    "default.logs",
			wantErr:     true,
			errContains: "only SELECT queries are allowed",
		},
		{
			name:        "invalid table reference",
			sql:         "SELECT * FROM `other`.`logs`",
			tableRef:    "default.logs",
			wantErr:     true,
			errContains: "invalid table reference",
		},
		{
			name:     "complex select with where",
			sql:      "SELECT service_name, COUNT(*) as error_count FROM `default`.`logs` WHERE severity = 'ERROR' GROUP BY service_name",
			tableRef: "default.logs",
			wantErr:  false,
		},
		{
			name:        "dangerous operation in where",
			sql:         "SELECT * FROM `default`.`logs` WHERE severity = 'ERROR' OR EXISTS (SELECT 1 FROM system.tables)",
			tableRef:    "default.logs",
			wantErr:     true,
			errContains: "dangerous operations detected",
		},
		{
			name:     "select with order by and limit",
			sql:      "SELECT * FROM `default`.`logs` ORDER BY timestamp DESC LIMIT 100",
			tableRef: "default.logs",
			wantErr:  false,
		},
		{
			name:     "select with timestamp conditions",
			sql:      "SELECT * FROM `default`.`logs` WHERE timestamp >= toDateTime64(1737225000.000, 3) AND timestamp <= toDateTime64(1737484200.000, 3)",
			tableRef: "default.logs",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewRawSQLBuilder(tt.tableRef, tt.sql)
			query, err := builder.Build()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			assert.NoError(t, err)
			if assert.NotNil(t, query) {
				// Normalize SQL for comparison
				normalizeSQL := func(sql string) string {
					sql = strings.ToLower(sql)
					sql = strings.ReplaceAll(sql, "  ", " ")
					sql = strings.ReplaceAll(sql, "`", "")
					return sql
				}
				expectedSQL := normalizeSQL(tt.sql)
				actualSQL := normalizeSQL(query.SQL)
				assert.Contains(t, actualSQL, expectedSQL)
			}
		})
	}
}

func TestRawSQLBuilder_validateSelectQuery(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		tableRef    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid select",
			sql:      "SELECT * FROM `default`.`logs`",
			tableRef: "default.logs",
			wantErr:  false,
		},
		{
			name:        "invalid syntax",
			sql:         "SELECT * FRO `default`.`logs`",
			tableRef:    "default.logs",
			wantErr:     true,
			errContains: "syntax error",
		},
		{
			name:        "system table access",
			sql:         "SELECT * FROM system.tables",
			tableRef:    "default.logs",
			wantErr:     true,
			errContains: "invalid table reference",
		},
		{
			name:        "dangerous operation",
			sql:         "SELECT * FROM `default`.`logs` WHERE DROP TABLE other_logs",
			tableRef:    "default.logs",
			wantErr:     true,
			errContains: "invalid SQL syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewRawSQLBuilder(tt.tableRef, tt.sql)
			err := builder.validateSelectQuery()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			assert.NoError(t, err)
		})
	}
}

func Test_containsDangerousOperations(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{
			name: "safe query",
			sql:  "severity = 'ERROR' AND service_name = 'api'",
			want: false,
		},
		{
			name: "contains drop",
			sql:  "severity = 'ERROR' OR DROP TABLE logs",
			want: true,
		},
		{
			name: "contains system",
			sql:  "EXISTS (SELECT 1 FROM system.tables)",
			want: true,
		},
		{
			name: "contains settings",
			sql:  "SELECT * FROM logs WHERE settings = 'value'",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsDangerousOperations(tt.sql)
			assert.Equal(t, tt.want, got)
		})
	}
}
