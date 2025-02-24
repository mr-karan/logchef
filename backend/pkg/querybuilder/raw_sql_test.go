package querybuilder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRawSQLBuilder_Build(t *testing.T) {
	tests := []struct {
		name        string
		opts        Options
		wantErr     bool
		errContains string
	}{
		{
			name: "valid simple select",
			opts: Options{
				TableName: "default.logs",
				RawSQL:    "SELECT * FROM `default`.`logs`",
				Limit:     100,
			},
			wantErr: false,
		},
		{
			name: "invalid non-select query",
			opts: Options{
				TableName: "default.logs",
				RawSQL:    "DELETE FROM `default`.`logs`",
				Limit:     100,
			},
			wantErr:     true,
			errContains: "only SELECT queries are allowed",
		},
		{
			name: "invalid table reference",
			opts: Options{
				TableName: "default.logs",
				RawSQL:    "SELECT * FROM `other`.`logs`",
				Limit:     100,
			},
			wantErr:     true,
			errContains: "invalid table reference",
		},
		{
			name: "complex select with where",
			opts: Options{
				TableName: "default.logs",
				RawSQL:    "SELECT service_name, COUNT(*) as error_count FROM `default`.`logs` WHERE severity = 'ERROR' GROUP BY service_name",
				Limit:     100,
			},
			wantErr: false,
		},
		{
			name: "dangerous operation in where",
			opts: Options{
				TableName: "default.logs",
				RawSQL:    "SELECT * FROM `default`.`logs` WHERE severity = 'ERROR' OR EXISTS (SELECT 1 FROM system.tables)",
				Limit:     100,
			},
			wantErr:     true,
			errContains: "subqueries are not allowed",
		},
		{
			name: "select with order by and limit",
			opts: Options{
				TableName: "default.logs",
				RawSQL:    "SELECT * FROM `default`.`logs` ORDER BY timestamp DESC LIMIT 100",
				Limit:     200,
			},
			wantErr: false,
		},
		{
			name: "select with timestamp conditions",
			opts: Options{
				TableName: "default.logs",
				RawSQL:    "SELECT * FROM `default`.`logs` WHERE timestamp >= toDateTime64(1737225000.000, 3) AND timestamp <= toDateTime64(1737484200.000, 3)",
				Limit:     100,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewRawSQLBuilder(tt.opts)
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
				expectedSQL := normalizeSQL(tt.opts.RawSQL)
				actualSQL := normalizeSQL(query.SQL)
				assert.Contains(t, actualSQL, expectedSQL)
			}
		})
	}
}

func TestRawSQLBuilder_validateSelectQuery(t *testing.T) {
	tests := []struct {
		name        string
		opts        Options
		wantErr     bool
		errContains string
	}{
		{
			name: "valid select",
			opts: Options{
				TableName: "default.logs",
				RawSQL:    "SELECT * FROM `default`.`logs`",
				Limit:     100,
			},
			wantErr: false,
		},
		{
			name: "invalid syntax",
			opts: Options{
				TableName: "default.logs",
				RawSQL:    "SELECT * FRO `default`.`logs`",
				Limit:     100,
			},
			wantErr:     true,
			errContains: "syntax error",
		},
		{
			name: "system table access",
			opts: Options{
				TableName: "default.logs",
				RawSQL:    "SELECT * FROM system.tables",
				Limit:     100,
			},
			wantErr:     true,
			errContains: "invalid table reference",
		},
		{
			name: "dangerous operation",
			opts: Options{
				TableName: "default.logs",
				RawSQL:    "SELECT * FROM `default`.`logs` WHERE DROP TABLE other_logs",
				Limit:     100,
			},
			wantErr:     true,
			errContains: "invalid SQL syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewRawSQLBuilder(tt.opts)
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
