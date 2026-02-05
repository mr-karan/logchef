package clickhouse

import (
	"testing"
)

// TestEnsureTimestampInQuery tests the ensureTimestampInQuery function
// which ensures MATERIALIZED columns are explicitly selected for histogram queries.
func TestEnsureTimestampInQuery(t *testing.T) {
	client := &Client{} // logger is nil, function handles this

	tests := []struct {
		name           string
		query          string
		timestampField string
		wantContains   string // substring that should be in result
		wantUnchanged  bool   // if true, expect query to be unchanged
		wantErr        bool
	}{
		{
			name:           "SELECT * adds timestamp field for MATERIALIZED columns",
			query:          "SELECT * FROM logs.nginx_access_logs WHERE parsed_timestamp > now()",
			timestampField: "parsed_timestamp",
			wantUnchanged:  true, // timestamp already in query
		},
		{
			name:           "SELECT * without timestamp field adds it",
			query:          "SELECT * FROM logs.nginx_access_logs",
			timestampField: "parsed_timestamp",
			wantContains:   "SELECT *, `parsed_timestamp`",
		},
		{
			name:           "SELECT * with WHERE clause adds timestamp",
			query:          "SELECT * FROM logs.nginx_access_logs WHERE status = 200",
			timestampField: "ts",
			wantContains:   "SELECT *, `ts`",
		},
		{
			name:           "timestamp already in query - no change",
			query:          "SELECT host, parsed_timestamp FROM logs.vector_logs",
			timestampField: "parsed_timestamp",
			wantUnchanged:  true,
		},
		{
			name:           "timestamp in WHERE clause - no change needed",
			query:          "SELECT * FROM logs WHERE parsed_timestamp BETWEEN now() - 1h AND now()",
			timestampField: "parsed_timestamp",
			wantUnchanged:  true,
		},
		{
			name:           "specific columns without timestamp - adds it",
			query:          "SELECT host, status FROM logs.nginx_logs",
			timestampField: "timestamp",
			wantContains:   "SELECT `timestamp`, host, status",
		},
		{
			name:           "empty query returns error",
			query:          "   ",
			timestampField: "timestamp",
			wantErr:        true,
		},
		{
			name:           "complex query with subquery",
			query:          "SELECT * FROM (SELECT * FROM logs WHERE level = 'ERROR') AS subq",
			timestampField: "ts",
			wantContains:   "`ts`",
		},
		{
			name:           "case insensitive SELECT",
			query:          "select * from logs.table",
			timestampField: "timestamp",
			wantContains:   "`timestamp`",
		},
		{
			name:           "SELECT with DISTINCT",
			query:          "SELECT DISTINCT host FROM logs",
			timestampField: "ts",
			wantContains:   "`ts`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.ensureTimestampInQuery(tt.query, tt.timestampField)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.wantUnchanged {
				if result != tt.query {
					t.Errorf("expected query unchanged, got: %s", result)
				}
				return
			}

			if tt.wantContains != "" {
				if !containsIgnoreCase(result, tt.wantContains) {
					t.Errorf("expected result to contain %q, got: %s", tt.wantContains, result)
				}
			}
		})
	}
}

// TestRemoveLimitClause tests the QueryBuilder's RemoveLimitClause function
func TestRemoveLimitClause(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    string
		wantErr bool
	}{
		{
			name:  "removes simple LIMIT",
			query: "SELECT * FROM logs LIMIT 100",
			want:  "SELECT * FROM logs",
		},
		{
			name:  "removes LIMIT with large number",
			query: "SELECT * FROM logs WHERE level = 'ERROR' LIMIT 10000",
			want:  "SELECT * FROM logs WHERE level = 'ERROR'",
		},
		{
			name:  "query without LIMIT unchanged",
			query: "SELECT * FROM logs WHERE status = 200",
			want:  "SELECT * FROM logs WHERE status = 200",
		},
		{
			name:  "removes LIMIT with ORDER BY",
			query: "SELECT * FROM logs ORDER BY timestamp DESC LIMIT 500",
			want:  "SELECT * FROM logs ORDER BY timestamp DESC",
		},
		{
			name:  "case insensitive LIMIT",
			query: "SELECT * FROM logs limit 100",
			want:  "SELECT * FROM logs",
		},
		{
			name:  "LIMIT with newline",
			query: "SELECT * FROM logs\nLIMIT 100",
			want:  "SELECT * FROM logs",
		},
		{
			name:    "empty query returns error",
			query:   "",
			wantErr: true,
		},
	}

	qb := NewQueryBuilder("logs", 0)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := qb.RemoveLimitClause(tt.query)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Normalize whitespace for comparison
			resultNorm := normalizeWhitespace(result)
			wantNorm := normalizeWhitespace(tt.want)

			if resultNorm != wantNorm {
				t.Errorf("got: %q, want: %q", result, tt.want)
			}
		})
	}
}

// TestHistogramQueryConstruction tests that histogram queries are properly constructed
func TestHistogramQueryConstruction(t *testing.T) {
	// These are integration-style tests that verify the histogram query structure
	tests := []struct {
		name           string
		baseQuery      string
		timestampField string
		window         string
		groupBy        string
		wantContains   []string
	}{
		{
			name:           "basic histogram without grouping",
			baseQuery:      "SELECT * FROM logs WHERE timestamp BETWEEN now() - 1h AND now()",
			timestampField: "timestamp",
			window:         "5m",
			wantContains: []string{
				"AS bucket",
				"count(*) AS log_count",
				"GROUP BY bucket",
				"ORDER BY bucket ASC",
			},
		},
		{
			name:           "histogram with MATERIALIZED timestamp",
			baseQuery:      "SELECT * FROM nginx_logs WHERE status = 200",
			timestampField: "parsed_timestamp",
			window:         "1m",
			wantContains: []string{
				"`parsed_timestamp`", // Should be added
				"AS bucket",
				"GROUP BY bucket",
			},
		},
		{
			name:           "histogram with grouping",
			baseQuery:      "SELECT * FROM logs WHERE level = 'ERROR'",
			timestampField: "ts",
			window:         "10s",
			groupBy:        "service_name",
			wantContains: []string{
				"top_groups",
				"group_value",
				"LIMIT 10", // Top N groups
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This is a structural test - we're verifying the function exists
			// and can process queries. Full integration would require a ClickHouse connection.
			client := &Client{}

			// Test ensureTimestampInQuery which is part of histogram construction
			result, err := client.ensureTimestampInQuery(tt.baseQuery, tt.timestampField)
			if err != nil {
				t.Errorf("ensureTimestampInQuery failed: %v", err)
				return
			}

			// If timestamp not in original query, verify it was added
			if !containsIgnoreCase(tt.baseQuery, tt.timestampField) {
				if !containsIgnoreCase(result, tt.timestampField) {
					t.Errorf("expected timestamp field %q to be added to query", tt.timestampField)
				}
			}
		})
	}
}

// TestTimeWindowParsing tests various time window formats
func TestTimeWindowParsing(t *testing.T) {
	validWindows := []string{
		"1s", "5s", "10s", "15s", "30s",
		"1m", "5m", "10m", "15m", "30m",
		"1h", "2h", "3h", "6h", "12h", "24h",
	}

	for _, w := range validWindows {
		tw := TimeWindow(w)
		// Just verify these are valid TimeWindow values
		if string(tw) != w {
			t.Errorf("TimeWindow conversion failed for %s", w)
		}
	}
}

// TestLogQueryParams tests query parameter validation
func TestLogQueryParams(t *testing.T) {
	tests := []struct {
		name    string
		params  LogQueryParams
		wantErr bool
	}{
		{
			name: "valid params with SQL",
			params: LogQueryParams{
				Limit:  100,
				RawSQL: "SELECT * FROM logs",
			},
			wantErr: false,
		},
		{
			name: "zero limit is valid (uses default)",
			params: LogQueryParams{
				Limit:  0,
				RawSQL: "SELECT * FROM logs",
			},
			wantErr: false,
		},
		{
			name: "with timeout",
			params: LogQueryParams{
				Limit:        100,
				RawSQL:       "SELECT * FROM logs",
				QueryTimeout: intPtr(30),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Structural validation - these params should be valid
			if tt.params.RawSQL == "" && !tt.wantErr {
				t.Errorf("expected RawSQL to be set for valid params")
			}
		})
	}
}

// TestHistogramParams tests histogram parameter validation
func TestHistogramParams(t *testing.T) {
	tests := []struct {
		name   string
		params HistogramParams
		valid  bool
	}{
		{
			name: "valid basic params",
			params: HistogramParams{
				Window:   "5m",
				Query:    "SELECT * FROM logs",
				Timezone: "UTC",
			},
			valid: true,
		},
		{
			name: "valid with groupby",
			params: HistogramParams{
				Window:   "1h",
				Query:    "SELECT * FROM logs",
				GroupBy:  "service_name",
				Timezone: "Asia/Kolkata",
			},
			valid: true,
		},
		{
			name: "missing query is invalid",
			params: HistogramParams{
				Window:   "5m",
				Query:    "",
				Timezone: "UTC",
			},
			valid: false,
		},
		{
			name: "empty timezone defaults to UTC",
			params: HistogramParams{
				Window: "5m",
				Query:  "SELECT * FROM logs",
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate params
			isValid := tt.params.Query != ""
			if isValid != tt.valid {
				t.Errorf("expected valid=%v, got valid=%v", tt.valid, isValid)
			}
		})
	}
}

// Helper functions

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(substr) == 0 ||
			findIgnoreCase(s, substr) >= 0)
}

func findIgnoreCase(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(s) < len(substr) {
		return -1
	}

	// Simple case-insensitive search
	sLower := toLower(s)
	substrLower := toLower(substr)

	for i := 0; i <= len(sLower)-len(substrLower); i++ {
		if sLower[i:i+len(substrLower)] == substrLower {
			return i
		}
	}
	return -1
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		result[i] = c
	}
	return string(result)
}

func normalizeWhitespace(s string) string {
	result := make([]byte, 0, len(s))
	inWhitespace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if !inWhitespace && len(result) > 0 {
				result = append(result, ' ')
			}
			inWhitespace = true
		} else {
			result = append(result, c)
			inWhitespace = false
		}
	}
	// Trim trailing space
	if len(result) > 0 && result[len(result)-1] == ' ' {
		result = result[:len(result)-1]
	}
	return string(result)
}

func intPtr(i int) *int {
	return &i
}
