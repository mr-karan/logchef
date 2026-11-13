package clickhouse

import (
	"errors"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestExtendedQueryBuilder(t *testing.T) {
	// Extended mode allows any SELECT query - ClickHouse permissions are the security boundary
	qb := NewExtendedQueryBuilder("mydb.logs", 10000)

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
	qb := NewQueryBuilder("mydb.logs", 10000)

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

// TestLimitRule covers the LIMIT rule: the smallest of the statement's own
// literal LIMIT, the request limit and the configured maximum wins, the
// default applies when neither the statement nor the request sets one, a
// clamp keeps OFFSET, and a cap asks for one extra row.
func TestLimitRule(t *testing.T) {
	qb := NewExtendedQueryBuilder("mydb.logs", 100000)
	for _, tc := range []struct {
		name                   string
		sql                    string
		request, def, max      int
		wantSQL                string
		wantApplied, wantFetch int
		wantSource             models.LimitSource
		wantCapped, wantAdded  bool
	}{
		{
			name: "statement below the request wins without a probe",
			sql:  "SELECT * FROM mydb.logs ORDER BY x DESC LIMIT 20", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM mydb.logs ORDER BY x DESC LIMIT 20", wantApplied: 20, wantFetch: 20, wantSource: models.LimitSourceStatement,
		},
		{
			name: "statement equal to the request is the author's",
			sql:  "SELECT * FROM mydb.logs LIMIT 100", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 100", wantApplied: 100, wantFetch: 100, wantSource: models.LimitSourceStatement,
		},
		{
			name: "request below the statement clamps and keeps OFFSET",
			sql:  "SELECT * FROM mydb.logs LIMIT 5000 OFFSET 200", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 101 OFFSET 200", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceRequest, wantCapped: true,
		},
		{
			name: "comma offset form keeps its offset",
			sql:  "SELECT * FROM mydb.logs LIMIT 200, 5000", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 101 OFFSET 200", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceRequest, wantCapped: true,
		},
		{
			name: "maximum below the statement without a request",
			sql:  "SELECT * FROM mydb.logs LIMIT 5000", request: 0, def: 1000, max: 100,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 101", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceMax, wantCapped: true,
		},
		{
			name: "maximum below the request",
			sql:  "SELECT * FROM mydb.logs", request: 500, def: 1000, max: 100,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 101", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceMax, wantCapped: true,
		},
		{
			name: "request without a statement limit probes",
			sql:  "SELECT * FROM mydb.logs", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 101", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceRequest,
		},
		{
			name: "default without a statement or request limit",
			sql:  "SELECT * FROM mydb.logs", request: 0, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 1001", wantApplied: 1000, wantFetch: 1001, wantSource: models.LimitSourceDefault, wantAdded: true,
		},
		{
			name: "default above the maximum",
			sql:  "SELECT * FROM mydb.logs", request: 0, def: 1000, max: 100,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 101", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceMax, wantCapped: true, wantAdded: true,
		},
		{
			name: "no ceiling and no default falls back",
			sql:  "SELECT * FROM mydb.logs", request: 0, def: 0, max: 0,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 1001", wantApplied: 1000, wantFetch: 1001, wantSource: models.LimitSourceDefault, wantAdded: true,
		},
		{
			name: "offset without a limit keeps the offset",
			sql:  "SELECT * FROM mydb.logs OFFSET 10", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 101 OFFSET 10", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceRequest,
		},
		{
			name: "LIMIT n BY is a separate clause",
			sql:  "SELECT * FROM mydb.logs ORDER BY a LIMIT 1 BY a LIMIT 500", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM mydb.logs ORDER BY a LIMIT 1 BY a LIMIT 101", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceRequest, wantCapped: true,
		},
		{
			name: "LIMIT n BY alone gets the request limit",
			sql:  "SELECT * FROM mydb.logs LIMIT 1 BY a", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 1 BY a LIMIT 101", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceRequest,
		},
		{
			name: "a LIMIT inside a CTE is untouched",
			sql:  "WITH c AS (SELECT * FROM mydb.logs LIMIT 5) SELECT * FROM c", request: 100, def: 1000, max: 100000,
			wantSQL: "WITH c AS (SELECT * FROM mydb.logs LIMIT 5) SELECT * FROM c LIMIT 101", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceRequest,
		},
		{
			name: "a LIMIT inside a subquery is untouched while the outer one clamps",
			sql:  "SELECT * FROM (SELECT * FROM mydb.logs LIMIT 5000) LIMIT 7000 OFFSET 3", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM (SELECT * FROM mydb.logs LIMIT 5000) LIMIT 101 OFFSET 3", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceRequest, wantCapped: true,
		},
		{
			name: "a LIMIT expression stands and the request caps without a probe",
			sql:  "SELECT * FROM mydb.logs LIMIT 5 * 2", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 5 * 2", wantApplied: 100, wantFetch: 100, wantSource: models.LimitSourceRequest,
		},
		{
			name: "a LIMIT expression without a request is capped by the maximum",
			sql:  "SELECT * FROM mydb.logs LIMIT 5 * 2", request: 0, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 5 * 2", wantApplied: 100000, wantFetch: 100000, wantSource: models.LimitSourceMax,
		},
		{
			name: "a LIMIT expression without a ceiling is unbounded",
			sql:  "SELECT * FROM mydb.logs LIMIT 5 * 2", request: 0, def: 1000, max: 0,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 5 * 2", wantApplied: 0, wantFetch: 0, wantSource: models.LimitSourceStatement,
		},
		{
			name: "UNION branch limits are their own and the request caps the whole",
			sql:  "SELECT * FROM mydb.logs LIMIT 2 UNION ALL SELECT * FROM mydb.logs LIMIT 3", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM (SELECT * FROM mydb.logs LIMIT 2 UNION ALL SELECT * FROM mydb.logs LIMIT 3) LIMIT 101", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceRequest,
		},
		{
			name: "parenthesized UNION branches keep their limits",
			sql:  "(SELECT * FROM mydb.logs LIMIT 2) UNION ALL (SELECT * FROM mydb.logs LIMIT 3)", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM ((SELECT * FROM mydb.logs LIMIT 2) UNION ALL (SELECT * FROM mydb.logs LIMIT 3)) LIMIT 101", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceRequest,
		},
		{
			name: "a set operation's trailing SETTINGS moves outside the cap",
			sql:  "SELECT a FROM mydb.logs UNION DISTINCT SELECT b FROM mydb.logs SETTINGS max_threads = 1", request: 0, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM (SELECT a FROM mydb.logs UNION DISTINCT SELECT b FROM mydb.logs) LIMIT 1001 SETTINGS max_threads=1", wantApplied: 1000, wantFetch: 1001, wantSource: models.LimitSourceDefault, wantAdded: true,
		},
		{
			name: "EXCEPT and INTERSECT are capped as a whole",
			sql:  "(SELECT a FROM mydb.logs LIMIT 9) EXCEPT (SELECT a FROM mydb.logs LIMIT 1) INTERSECT SELECT a FROM mydb.logs", request: 500, def: 1000, max: 100,
			wantSQL: "SELECT * FROM ((SELECT a FROM mydb.logs LIMIT 9) EXCEPT (SELECT a FROM mydb.logs LIMIT 1) INTERSECT SELECT a FROM mydb.logs) LIMIT 101", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceMax, wantCapped: true,
		},
		{
			name: "a parenthesized set operation is capped outside its parentheses",
			sql:  "(SELECT a FROM mydb.logs UNION ALL SELECT b FROM mydb.logs LIMIT 3) SETTINGS max_threads = 1", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM ((SELECT a FROM mydb.logs UNION ALL SELECT b FROM mydb.logs LIMIT 3)) LIMIT 101 SETTINGS max_threads=1", wantApplied: 100, wantFetch: 101, wantSource: models.LimitSourceRequest,
		},
		{
			name: "a parenthesized statement keeps its own limit",
			sql:  "(SELECT * FROM mydb.logs LIMIT 20) SETTINGS max_threads = 1", request: 100, def: 1000, max: 100000,
			wantSQL: "(SELECT * FROM mydb.logs LIMIT 20) SETTINGS max_threads=1", wantApplied: 20, wantFetch: 20, wantSource: models.LimitSourceStatement,
		},
		{
			name: "LIMIT 0 is the author's",
			sql:  "SELECT * FROM mydb.logs LIMIT 0", request: 100, def: 1000, max: 100000,
			wantSQL: "SELECT * FROM mydb.logs LIMIT 0", wantApplied: 0, wantFetch: 0, wantSource: models.LimitSourceStatement,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := qb.BuildRawQueryWithLimitPolicy(tc.sql, tc.request, tc.def, tc.max)
			if err != nil {
				t.Fatalf("BuildRawQueryWithLimitPolicy() error = %v", err)
			}
			want := QueryBuildResult{
				SQL: tc.wantSQL, RequestedLimit: tc.request, OriginalLimit: got.OriginalLimit, ExplicitLimit: got.ExplicitLimit,
				AppliedLimit: tc.wantApplied, FetchLimit: tc.wantFetch, LimitSource: tc.wantSource, LimitCapped: tc.wantCapped, LimitAdded: tc.wantAdded,
			}
			if got != want {
				t.Fatalf("BuildRawQueryWithLimitPolicy() = %+v, want %+v", got, want)
			}
			opts := got.QueryOptions(30)
			if opts.MaxRows != tc.wantApplied || opts.Settings["max_result_rows"] != tc.wantFetch || opts.Probed != (tc.wantFetch > tc.wantApplied) {
				t.Fatalf("QueryOptions() = %+v, want MaxRows %d, max_result_rows %d", opts, tc.wantApplied, tc.wantFetch)
			}
		})
	}
}

// TestLimitRuleCountMode keeps count mode stripping the outer LIMIT before the
// histogram wraps the query, and leaving a set operation's branch limits.
func TestLimitRuleCountMode(t *testing.T) {
	qb := NewQueryBuilder("mydb.logs", 0)
	for sql, want := range map[string]string{
		"SELECT * FROM mydb.logs ORDER BY x DESC LIMIT 20 OFFSET 5":                     "SELECT * FROM mydb.logs ORDER BY x DESC",
		"(SELECT * FROM mydb.logs LIMIT 20)":                                            "(SELECT * FROM mydb.logs)",
		"SELECT * FROM mydb.logs LIMIT 2 UNION ALL SELECT * FROM mydb.logs LIMIT 3":     "SELECT * FROM mydb.logs LIMIT 2 UNION ALL SELECT * FROM mydb.logs LIMIT 3",
		"(SELECT * FROM mydb.logs LIMIT 2) UNION ALL (SELECT * FROM mydb.logs LIMIT 3)": "(SELECT * FROM mydb.logs LIMIT 2) UNION ALL (SELECT * FROM mydb.logs LIMIT 3)",
	} {
		got, err := qb.RemoveLimitClause(sql)
		if err != nil || got != want {
			t.Fatalf("RemoveLimitClause(%q) = %q, %v, want %q", sql, got, err, want)
		}
	}
}

func TestQueryBuilderDefaultLimitFromMaxLimit(t *testing.T) {
	qb := NewExtendedQueryBuilder("mydb.logs", 1000000)

	result, err := qb.BuildRawQuery("SELECT * FROM mydb.logs", 0)
	if err != nil {
		t.Fatalf("BuildRawQuery() error = %v", err)
	}

	if !strings.Contains(result, "LIMIT 1000001") {
		t.Fatalf("BuildRawQuery() = %q, should contain %q", result, "LIMIT 1000001")
	}
}

func TestQueryBuilderDefaultLimitFallback(t *testing.T) {
	qb := NewExtendedQueryBuilder("mydb.logs", 0)

	result, err := qb.BuildRawQuery("SELECT * FROM mydb.logs", 0)
	if err != nil {
		t.Fatalf("BuildRawQuery() error = %v", err)
	}

	if !strings.Contains(result, "LIMIT 1001") {
		t.Fatalf("BuildRawQuery() = %q, should contain %q", result, "LIMIT 1001")
	}
}

func TestQueryBuilderPreservesSQLLiteralsAndIdentifiers(t *testing.T) {
	qb := NewExtendedQueryBuilder("mydb.logs", 10000)

	tests := []struct {
		name       string
		sql        string
		wantParts  []string
		wantAbsent []string
	}{
		{
			name:      "placeholder text in a string literal",
			sql:       "SELECT '___ESCAPED_QUOTE___' AS value FROM mydb.logs",
			wantParts: []string{"'___ESCAPED_QUOTE___'"},
		},
		{
			name:      "escaped quote in a string literal",
			sql:       "SELECT 'it''s' AS value FROM mydb.logs",
			wantParts: []string{"'it''s'"},
		},
		{
			name:      "empty string literal",
			sql:       "SELECT '' AS value FROM mydb.logs",
			wantParts: []string{"''"},
		},
		{
			name:      "placeholder text in a quoted identifier",
			sql:       "SELECT `___ESCAPED_QUOTE___` FROM mydb.logs",
			wantParts: []string{"`___ESCAPED_QUOTE___`"},
		},
		{
			name:      "placeholder text in a comment is not parsed as SQL",
			sql:       "SELECT 'value' AS value FROM mydb.logs -- ___ESCAPED_QUOTE___",
			wantParts: []string{"'value'"},
			// The formatter intentionally omits comments. It must not turn the
			// comment marker text into a quote in the formatted SQL.
			wantAbsent: []string{"'___ESCAPED_QUOTE___'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := qb.BuildRawQuery(tt.sql, 0)
			if err != nil {
				t.Fatalf("BuildRawQuery() error = %v", err)
			}
			for _, part := range tt.wantParts {
				if !strings.Contains(result, part) {
					t.Errorf("BuildRawQuery() = %q, want substring %q", result, part)
				}
			}
			for _, part := range tt.wantAbsent {
				if strings.Contains(result, part) {
					t.Errorf("BuildRawQuery() = %q, must not contain %q", result, part)
				}
			}
		})
	}
}

func TestFormatSelectQuery(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  string
	}{
		{query: "SELECT count() AS value FROM logs -- trailing", want: "SELECT count() AS value FROM logs"},
		{query: "/* /* */ SELECT '*/ SELECT 42 AS value -- '", want: "SELECT '*/ SELECT 42 AS value -- '"},
		{
			query: "SELECT count(*) as value\nFROM logs\nWHERE severity = 'ERROR'\n  AND `timestamp` >= now() - toIntervalSecond(300)",
			want:  "SELECT count(*) AS value FROM logs WHERE severity = 'ERROR' AND `timestamp` >= now() - toIntervalSecond(300)",
		},
		{query: `SELECT "service.name", sum(t.bytes$total) AS value FROM db.logs AS t WHERE msg = 'a\\b'`, want: `SELECT "service.name", sum(t.bytes$total) AS value FROM db.logs AS t WHERE msg = 'a\\b'`},
	} {
		got, err := FormatSelectQuery(tc.query)
		if err != nil || got != tc.want {
			t.Errorf("FormatSelectQuery(%q) = %q, %v; want %q", tc.query, got, err, tc.want)
		}
	}

	for _, query := range []string{
		"",
		"DROP TABLE x",
		"INSERT INTO x SELECT 1",
		"/* c */ DROP TABLE x",
		"SYSTEM SHUTDOWN",
		"OPTIMIZE TABLE x",
		"KILL QUERY WHERE 1",
		"SELECT 1; DROP TABLE x",
		// ClickHouse reads an escaped backtick or a heredoc string here, so the
		// formatted text would run the hidden 42 AS value.
		"SELECT 'n' AS `\\`, '`, 42 AS value -- '",
		`SELECT 'n' AS "\", '", 42 AS value -- '`,
		"SELECT $$ AS a, '$$, 42 AS value -- '",
		"SELECT count() FROM logs WHERE `a\\b` = 1",
	} {
		if got, err := FormatSelectQuery(query); err == nil {
			t.Errorf("FormatSelectQuery(%q) = %q, want error", query, got)
		}
	}
}

// acceptedQuotedIdentifiers maps quoted identifiers to the name ClickHouse reads.
var acceptedQuotedIdentifiers = []struct {
	quote, body, name string
}{
	{"`", `a\\b`, `a\b`},
	{"`", `a\x60b`, "a`b"},
	{"`", `a\\\x60b`, "a\\`b"},
	{"`", `a\\`, `a\`},
	{"`", `\x60`, "`"},
	{"`", `a\x60`, "a`"},
	{"`", `a\\x60`, `a\x60`},
	{"`", `a\x600`, "a`0"},
	{"`", `\\\\`, `\\`},
	{`"`, `a\x60b`, "a`b"},
	{`"`, `a\\b`, `a\b`},
	{`"`, "a`b", "a`b"},
	{`"`, "a``b", "a``b"},
	{"`", `a"b`, `a"b`},
}

// rejectedQuotedIdentifiers lists column references ClickHouse reads as a
// different token or a different name than the parser does.
var rejectedQuotedIdentifiers = []string{
	// ClickHouse reads past the parser's closing quote.
	"`a\\`",
	"`a\\\\\\`",
	"`a\\x6`",
	"`a\\x`",
	`"a\"`,
	`"a\x6"`,
	// ClickHouse reads a doubled quote as one embedded quote.
	"`a``b`",
	`"a""b"`,
	"````",
	"`a\\x60``b`",
	"`a\\\\``b`",
	`"a\x60""b"`,
	`"a\\""b"`,
	"`t`.`a``b`",
	// Escapes outside the allowlist. ClickHouse decodes most to another name.
	"`a\\x6gb`",
	"`a\\X60b`",
	"`a\\x41b`",
	"`a\\\"b`",
	"`a\\'b`",
	"`a\\nb`",
	"`a\\0b`",
	"`a\\é`",
}

// acceptedNamedArguments pass a table function's named argument as a bare name.
var acceptedNamedArguments = []string{
	"WITH 5 AS n SELECT count() FROM numbers(n = 5)",
	"WITH 5 AS n SELECT count() FROM numbers(n  = 5)",
	"WITH 5 AS n SELECT number FROM numbers(10, n = 5)",
	"WITH 2 AS n SELECT number FROM numbers(n = 2, 3)",
}

// rejectedNamedArguments quote a table function's named argument. The parser
// leaves its quote type unset, so the formatter would drop the quotes.
var rejectedNamedArguments = []string{
	"SELECT number FROM numbers(`5) UNION ALL SELECT number FROM numbers(100000) --` = 10)",
	"SELECT * FROM numbers(`1` = 10)",
	`SELECT * FROM numbers("1" = 10)`,
}

func TestQuotedIdentifierEscapes(t *testing.T) {
	qb := NewExtendedQueryBuilder("", 0)
	for _, tc := range acceptedQuotedIdentifiers {
		ident := tc.quote + tc.body + tc.quote
		t.Run(ident, func(t *testing.T) {
			got, err := qb.BuildRawQuery("SELECT 1 AS "+ident+", 2 AS marker", 0)
			if err != nil || !strings.Contains(got, "1 AS "+ident+", 2 AS marker") {
				t.Fatalf("BuildRawQuery = %q, %v; want %s kept verbatim", got, err, ident)
			}
		})
	}
	for _, ident := range rejectedQuotedIdentifiers {
		t.Run(ident, func(t *testing.T) {
			got, err := qb.BuildRawQuery("SELECT "+ident+" FROM logs", 0)
			if !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf("BuildRawQuery = %q, %v; want ErrInvalidQuery", got, err)
			}
		})
	}
	for _, sql := range append(acceptedNamedArguments, "SELECT * FROM numbers(n  = 10)") {
		t.Run(sql, func(t *testing.T) {
			if got, err := qb.BuildRawQuery(sql, 0); err != nil {
				t.Fatalf("BuildRawQuery = %q, %v; want no error", got, err)
			}
		})
	}
	for _, sql := range append(rejectedNamedArguments, "WITH 5 AS n SELECT count() FROM numbers(`n` = 5)") {
		t.Run(sql, func(t *testing.T) {
			got, err := qb.BuildRawQuery(sql, 0)
			if !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf("BuildRawQuery = %q, %v; want ErrInvalidQuery", got, err)
			}
		})
	}
}
