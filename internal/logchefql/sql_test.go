package logchefql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSQLGeneration(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantSQL  string
		wantArgs []interface{}
	}{
		{
			name:     "simple field comparison",
			query:    "service_name='api';severity_text='error'",
			wantSQL:  "SELECT * FROM logs WHERE service_name = ? AND severity_text = ?",
			wantArgs: []interface{}{"api", "error"},
		},
		{
			name:     "nested json fields",
			query:    "p.error.code=500;p.error.message~'timeout'",
			wantSQL:  "SELECT * FROM logs WHERE JSONExtractString(p, ?) = ? AND JSONExtractString(p, ?) ILIKE ?",
			wantArgs: []interface{}{"error.code", "500", "error.message", "%timeout%"},
		},
		{
			name:     "timestamp and patterns",
			query:    "timestamp>-1h;body~'connection refused';p.retries>3",
			wantSQL:  "SELECT * FROM logs WHERE timestamp > now() - INTERVAL ? AND body ILIKE ? AND JSONExtractString(p, ?) > ?",
			wantArgs: []interface{}{"-1h", "%connection refused%", "retries", "3"},
		},
		{
			name:     "multiple json paths from different columns",
			query:    "attributes.user.id='123';resource.service.name='auth';p.metadata.version~'1.0'",
			wantSQL:  "SELECT * FROM logs WHERE JSONExtractString(attributes, ?) = ? AND JSONExtractString(resource, ?) = ? AND JSONExtractString(p, ?) ILIKE ?",
			wantArgs: []interface{}{"user.id", "123", "service.name", "auth", "metadata.version", "%1.0%"},
		},
		{
			name:     "complex time intervals",
			query:    "timestamp>-24h;timestamp<-1h;attributes.timestamp>-30m",
			wantSQL:  "SELECT * FROM logs WHERE timestamp > now() - INTERVAL ? AND timestamp < now() - INTERVAL ? AND JSONExtractString(attributes, ?) > now() - INTERVAL ?",
			wantArgs: []interface{}{"-24h", "-1h", "timestamp", "-30m"},
		},
		{
			name:     "mixed operators",
			query:    "status_code>=500;p.response.time>1000;body!~'success';p.error.type~'timeout'",
			wantSQL:  "SELECT * FROM logs WHERE status_code >= ? AND JSONExtractString(p, ?) > ? AND body NOT ILIKE ? AND JSONExtractString(p, ?) ILIKE ?",
			wantArgs: []interface{}{"500", "response.time", "1000", "%success%", "error.type", "%timeout%"},
		},
		{
			name:     "deep json nesting",
			query:    "trace.parent.span.id='abc123';p.request.headers.x_forwarded_for~'10.0.0'",
			wantSQL:  "SELECT * FROM logs WHERE JSONExtractString(trace, ?) = ? AND JSONExtractString(p, ?) ILIKE ?",
			wantArgs: []interface{}{"parent.span.id", "abc123", "request.headers.x_forwarded_for", "%10.0.0%"},
		},
		{
			name:     "multiple conditions on same json field",
			query:    "p.user.id='123';p.user.role='admin';p.user.last_login>-7d",
			wantSQL:  "SELECT * FROM logs WHERE JSONExtractString(p, ?) = ? AND JSONExtractString(p, ?) = ? AND JSONExtractString(p, ?) > now() - INTERVAL ?",
			wantArgs: []interface{}{"user.id", "123", "user.role", "admin", "user.last_login", "-7d"},
		},
		{
			name:     "inequality operators",
			query:    "status_code!=200;p.response.size>=1024;p.retry_count<5",
			wantSQL:  "SELECT * FROM logs WHERE status_code != ? AND JSONExtractString(p, ?) >= ? AND JSONExtractString(p, ?) < ?",
			wantArgs: []interface{}{"200", "response.size", "1024", "retry_count", "5"},
		},
		{
			name:     "pattern matching with special characters",
			query:    "user_agent~'Mozilla/5.0';p.error.stack_trace~'panic:';body!~'[DEBUG]'",
			wantSQL:  "SELECT * FROM logs WHERE user_agent ILIKE ? AND JSONExtractString(p, ?) ILIKE ? AND body NOT ILIKE ?",
			wantArgs: []interface{}{"%Mozilla/5.0%", "error.stack_trace", "%panic:%", "%[DEBUG]%"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := Parse(tt.query)
			require.NoError(t, err)

			sql, args := ast.ToSQL("logs")
			require.Equal(t, tt.wantSQL, sql)
			require.Equal(t, tt.wantArgs, args)
		})
	}
}

// TestSQLGenerationErrors tests error cases in SQL generation
func TestSQLGenerationErrors(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantSQL  string
		wantArgs []interface{}
	}{
		{
			name:     "invalid time interval",
			query:    "timestamp>-1x", // invalid unit 'x'
			wantSQL:  "",              // expect empty SQL due to error
			wantArgs: nil,
		},
		{
			name:     "zero value time interval",
			query:    "timestamp>-0h",
			wantSQL:  "",
			wantArgs: nil,
		},
		{
			name:     "negative value in non-time field",
			query:    "status_code>-500", // negative values only valid in time intervals
			wantSQL:  "SELECT * FROM logs WHERE status_code > ?",
			wantArgs: []interface{}{"-500"},
		},
		{
			name:     "empty pattern match",
			query:    "service_name~''",
			wantSQL:  "SELECT * FROM logs WHERE service_name ILIKE ?",
			wantArgs: []interface{}{"%%"},
		},
		{
			name:     "json path with empty field",
			query:    "attributes..id='123'", // double dot
			wantSQL:  "",
			wantArgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := Parse(tt.query)
			if err != nil {
				// If parsing fails, that's expected for some error cases
				return
			}

			sql, args := ast.ToSQL("logs")
			require.Equal(t, tt.wantSQL, sql)
			require.Equal(t, tt.wantArgs, args)
		})
	}
}

func TestParserErrors(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{
			name:    "invalid operator",
			query:   "status_code>>500",
			wantErr: "unexpected token",
		},
		{
			name:    "missing value",
			query:   "status_code=",
			wantErr: "expected Value",
		},
		{
			name:    "invalid time format",
			query:   "timestamp>1h", // missing minus
			wantErr: "unexpected token",
		},
		{
			name:    "invalid field name",
			query:   "123field='value'",
			wantErr: "unexpected token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.query)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
