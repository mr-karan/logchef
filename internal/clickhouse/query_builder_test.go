package clickhouse

import (
	"strings"
	"testing"
)

func TestExtendedQueryBuilder(t *testing.T) {
	// Extended mode allows any SELECT query - ClickHouse permissions are the security boundary
	qb := NewExtendedQueryBuilder("mydb.logs")

	tests := []struct {
		name    string
		sql     string
		wantErr bool
		errMsg  string
	}{
		// All SELECT queries should pass in extended mode
		{
			name:    "simple SELECT",
			sql:     "SELECT * FROM mydb.logs LIMIT 100",
			wantErr: false,
		},
		{
			name:    "SELECT from different table",
			sql:     "SELECT * FROM other_db.secrets LIMIT 100",
			wantErr: false, // Extended mode allows any table
		},
		{
			name:    "CTE query",
			sql:     "WITH cte AS (SELECT * FROM mydb.logs WHERE status = 500) SELECT * FROM cte LIMIT 100",
			wantErr: false,
		},
		{
			name:    "JOIN query",
			sql:     "SELECT a.* FROM mydb.logs a JOIN mydb.logs b ON a.id = b.id LIMIT 100",
			wantErr: false,
		},
		{
			name:    "subquery in FROM",
			sql:     "SELECT * FROM (SELECT * FROM mydb.logs WHERE status = 500) AS sub LIMIT 100",
			wantErr: false,
		},
		{
			name:    "subquery in SELECT",
			sql:     "SELECT (SELECT count(*) FROM other_db.secrets) as cnt FROM mydb.logs LIMIT 100",
			wantErr: false, // Extended mode doesn't validate table refs
		},
		{
			name:    "subquery in WHERE",
			sql:     "SELECT * FROM mydb.logs WHERE id IN (SELECT id FROM other_db.secrets) LIMIT 100",
			wantErr: false, // Extended mode doesn't validate table refs
		},

		// Non-SELECT queries should fail
		{
			name:    "INSERT query should fail",
			sql:     "INSERT INTO mydb.logs (message) VALUES ('test')",
			wantErr: true,
			errMsg:  "only SELECT queries",
		},
		{
			name:    "DELETE query should fail",
			sql:     "DELETE FROM mydb.logs WHERE id = 1",
			wantErr: true,
			errMsg:  "only SELECT queries",
		},
		{
			name:    "UPDATE query should fail",
			sql:     "ALTER TABLE mydb.logs UPDATE message = 'test' WHERE id = 1",
			wantErr: true,
			errMsg:  "only SELECT queries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qb.BuildRawQuery(tt.sql, 100)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildRawQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("BuildRawQuery() error = %v, should contain %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestRestrictedQueryBuilder(t *testing.T) {
	// Restricted mode validates table reference and blocks JOINs
	qb := NewQueryBuilder("mydb.logs")

	tests := []struct {
		name    string
		sql     string
		wantErr bool
		errMsg  string
	}{
		// Valid queries - should pass
		{
			name:    "simple SELECT from correct table",
			sql:     "SELECT * FROM mydb.logs LIMIT 100",
			wantErr: false,
		},
		{
			name:    "SELECT with WHERE from correct table",
			sql:     "SELECT * FROM mydb.logs WHERE status = 200 LIMIT 100",
			wantErr: false,
		},

		// Invalid queries - wrong table
		{
			name:    "SELECT from wrong table",
			sql:     "SELECT * FROM other_db.secrets LIMIT 100",
			wantErr: true,
			errMsg:  "invalid",
		},
		{
			name:    "SELECT from wrong table in same db",
			sql:     "SELECT * FROM mydb.other_table LIMIT 100",
			wantErr: true,
			errMsg:  "invalid table reference",
		},

		// JOINs blocked in restricted mode
		{
			name:    "JOIN is blocked",
			sql:     "SELECT * FROM mydb.logs JOIN mydb.logs b ON logs.id = b.id LIMIT 100",
			wantErr: true,
			errMsg:  "JOIN",
		},

		// Non-SELECT queries should fail
		{
			name:    "INSERT query should fail",
			sql:     "INSERT INTO mydb.logs (message) VALUES ('test')",
			wantErr: true,
			errMsg:  "only SELECT queries",
		},
		{
			name:    "DELETE query should fail",
			sql:     "DELETE FROM mydb.logs WHERE id = 1",
			wantErr: true,
			errMsg:  "only SELECT queries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qb.BuildRawQuery(tt.sql, 100)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildRawQuery() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("BuildRawQuery() error = %v, should contain %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestQueryBuilderLimit(t *testing.T) {
	qb := NewExtendedQueryBuilder("mydb.logs")

	tests := []struct {
		name      string
		sql       string
		limit     int
		wantLIMIT string
	}{
		{
			name:      "adds LIMIT when missing",
			sql:       "SELECT * FROM mydb.logs",
			limit:     100,
			wantLIMIT: "LIMIT 100",
		},
		{
			name:      "replaces existing LIMIT",
			sql:       "SELECT * FROM mydb.logs LIMIT 1000",
			limit:     100,
			wantLIMIT: "LIMIT 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := qb.BuildRawQuery(tt.sql, tt.limit)
			if err != nil {
				t.Errorf("BuildRawQuery() error = %v", err)
				return
			}
			if !strings.Contains(result, tt.wantLIMIT) {
				t.Errorf("BuildRawQuery() = %q, should contain %q", result, tt.wantLIMIT)
			}
		})
	}
}
