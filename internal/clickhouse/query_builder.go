package clickhouse

import (
	"fmt"
	"strconv"
	"strings"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"

	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// DefaultLimit is a conservative fallback used only when maxLimit is not configured.
	DefaultLimit = 1000
)

// QueryMode defines the validation strictness for SQL queries.
type QueryMode int

const (
	// RestrictedMode validates table reference and blocks JOINs/subqueries.
	// Used for LogchefQL-generated queries.
	RestrictedMode QueryMode = iota
	// ExtendedMode allows any SELECT query without table validation.
	// The ClickHouse connection permissions are the security boundary.
	ExtendedMode
)

// QueryBuilder assists in building and validating ClickHouse SQL queries.
type QueryBuilder struct {
	tableName    string
	mode         QueryMode
	defaultLimit int
	maxLimit     int
}

// QueryBuildResult describes the SQL produced by the query builder and the
// limit policy decisions that were applied.
type QueryBuildResult struct {
	SQL            string
	RequestedLimit int
	OriginalLimit  int
	// AppliedLimit is the most rows the caller returns. FetchLimit is the row
	// bound sent to ClickHouse: one past AppliedLimit when the server probes
	// for a cut result, so a row beyond the limit proves the truncation.
	AppliedLimit  int
	FetchLimit    int
	LimitSource   models.LimitSource
	LimitAdded    bool
	LimitCapped   bool
	ExplicitLimit bool
}

// QueryOptions returns the execution options that enforce the applied limit:
// the server-side row bound, the reader's cap and the facts the stats report.
func (r QueryBuildResult) QueryOptions(timeoutSeconds int) QueryOptions {
	return QueryOptions{
		TimeoutSeconds: &timeoutSeconds,
		Settings: map[string]any{
			"max_execution_time":   timeoutSeconds,
			"max_result_rows":      r.FetchLimit,
			"result_overflow_mode": "break",
		},
		LimitApplied: r.AppliedLimit,
		MaxRows:      r.AppliedLimit,
		LimitSource:  r.LimitSource,
		Probed:       r.FetchLimit > r.AppliedLimit,
	}
}

// NewQueryBuilder creates a new QueryBuilder for restricted mode.
// This validates that queries target the specified table and blocks JOINs/subqueries.
func NewQueryBuilder(tableName string, maxLimit int) *QueryBuilder {
	return &QueryBuilder{
		tableName:    tableName,
		mode:         RestrictedMode,
		defaultLimit: legacyDefaultLimit(maxLimit),
		maxLimit:     maxLimit,
	}
}

// NewExtendedQueryBuilder creates a QueryBuilder that allows any SELECT query.
// Only validates that the query is a SELECT statement (not INSERT/DELETE/UPDATE).
// The ClickHouse connection permissions are the real security boundary.
func NewExtendedQueryBuilder(tableName string, maxLimit int) *QueryBuilder {
	return &QueryBuilder{
		tableName:    tableName,
		mode:         ExtendedMode,
		defaultLimit: legacyDefaultLimit(maxLimit),
		maxLimit:     maxLimit,
	}
}

// BuildRawQuery parses, validates, and adds LIMIT to a SQL query.
func (qb *QueryBuilder) BuildRawQuery(rawSQL string, limit int) (string, error) {
	result, err := qb.BuildRawQueryWithLimitPolicy(rawSQL, limit, qb.defaultLimit, qb.maxLimit)
	if err != nil {
		return "", err
	}
	return result.SQL, nil
}

// FormatSelectQuery parses a single SELECT statement and renders it from the
// AST without comments. Literals and identifiers are written back verbatim, so
// the output runs as parsed only where the parser and ClickHouse lex them alike.
func FormatSelectQuery(rawSQL string) (string, error) {
	selectQuery, err := parseSelectQuery(rawSQL)
	if err != nil {
		return "", err
	}
	return formatSQL(selectQuery), nil
}

func parseSelectQuery(rawSQL string) (*clickhouseparser.SelectQuery, error) {
	// The parser lexer accepts ClickHouse's doubled apostrophes and keeps them
	// in StringLiteral.Literal. Do not preprocess raw SQL: the formatter emits
	// literal contents verbatim, while comments are intentionally omitted.
	parser := clickhouseparser.NewParser(rawSQL)
	stmts, err := parser.ParseStmts()
	if err != nil {
		return nil, fmt.Errorf("invalid SQL syntax: %w", err)
	}

	if len(stmts) == 0 {
		return nil, fmt.Errorf("no SQL statements found")
	}
	if len(stmts) > 1 {
		return nil, fmt.Errorf("multiple SQL statements are not supported")
	}

	stmt := stmts[0]
	selectQuery, ok := stmt.(*clickhouseparser.SelectQuery)
	if !ok {
		return nil, fmt.Errorf("only SELECT queries are supported: %w", ErrInvalidQuery)
	}
	if err := checkIdentifiers(rawSQL, selectQuery); err != nil {
		return nil, err
	}
	return selectQuery, nil
}

// checkIdentifiers rejects quoted identifiers that ClickHouse reads as a
// different token or a different name than the parser does, and names it
// lexes as $$ heredocs.
func checkIdentifiers(rawSQL string, query *clickhouseparser.SelectQuery) error {
	var err error
	clickhouseparser.Walk(query, func(node clickhouseparser.Expr) bool {
		ident, ok := node.(*clickhouseparser.Ident)
		if !ok {
			return true
		}
		// The parser ends a quoted token at the first matching quote, so
		// NameEnd indexes it. ClickHouse reads a doubled one as an embedded quote.
		start, closing := int(ident.NamePos)-1, int(ident.NameEnd)
		quoted := start >= 0 && closing < len(rawSQL) && rawSQL[closing] == rawSQL[start] && (rawSQL[start] == '`' || rawSQL[start] == '"')
		switch {
		case ident.QuoteType == 0 && quoted:
			// Paths such as a table function's named argument leave QuoteType
			// unset, and the formatter then writes the name without quotes.
			err = fmt.Errorf("quoted names before = in table function arguments are not supported: %s: %w", rawSQL[start:closing+1], ErrInvalidQuery)
		case ident.QuoteType == 0, ident.QuoteType == clickhouseparser.Unquoted:
			if strings.HasPrefix(ident.Name, "$") {
				err = fmt.Errorf("identifiers starting with $ are not supported: %q: %w", ident.Name, ErrInvalidQuery)
			}
		case closing+1 < len(rawSQL) && rawSQL[closing+1] == rawSQL[closing]:
			hint := "use backticks"
			if rawSQL[closing] == '`' {
				hint = "write a backtick as \\x60"
			}
			err = fmt.Errorf("doubled quotes inside quoted identifiers are not supported, %s: %s: %w", hint, rawSQL[start:closing+2], ErrInvalidQuery)
		default:
			if _, ok := decodeQuotedIdentifier(ident.Name); !ok {
				err = fmt.Errorf("quoted identifiers support only the \\\\ and \\x60 escapes: %s: %w", rawSQL[start:closing+1], ErrInvalidQuery)
			}
		}
		return err == nil
	})
	return err
}

// decodeQuotedIdentifier returns the name ClickHouse reads from the body of a
// quoted identifier. It reports false for any escape other than \\ and \x60,
// the ones queryescape writes: ClickHouse would decode it to another name or
// consume the closing quote.
func decodeQuotedIdentifier(body string) (string, bool) {
	if !strings.Contains(body, `\`) {
		return body, true
	}
	var name strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			name.WriteByte(body[i])
			continue
		}
		switch rest := body[i+1:]; {
		case strings.HasPrefix(rest, `\`):
			name.WriteByte('\\')
			i++
		case strings.HasPrefix(rest, "x60"):
			name.WriteByte('`')
			i += 3
		default:
			return "", false
		}
	}
	return name.String(), true
}

// identIs reports whether ClickHouse reads ident as name.
func identIs(ident *clickhouseparser.Ident, name string) bool {
	if ident.QuoteType == clickhouseparser.Unquoted {
		return ident.Name == name
	}
	decoded, ok := decodeQuotedIdentifier(ident.Name)
	return ok && decoded == name
}

// BuildRawQueryWithLimitPolicy parses, validates, and applies an explicit
// default/max limit policy. The default limit is used for SQL without LIMIT;
// the max limit is only a ceiling.
func (qb *QueryBuilder) BuildRawQueryWithLimitPolicy(rawSQL string, requestedLimit, defaultLimit, maxLimit int) (QueryBuildResult, error) {
	selectQuery, err := parseSelectQuery(rawSQL)
	if err != nil {
		return QueryBuildResult{}, err
	}

	// Mode-specific validation
	switch qb.mode {
	case RestrictedMode:
		// Restricted mode: validate single table, block JOINs
		if qb.tableName != "" {
			if err := qb.validateTableReference(selectQuery); err != nil {
				return QueryBuildResult{}, err
			}
			if err := qb.checkDangerousOperations(selectQuery); err != nil {
				return QueryBuildResult{}, err
			}
		}
	case ExtendedMode:
		// Extended mode: allow any SELECT query.
		// ClickHouse connection permissions are the security boundary.
		// No additional validation needed.
	}

	result := QueryBuildResult{RequestedLimit: requestedLimit}
	qb.ensureLimitWithPolicy(selectQuery, requestedLimit, defaultLimit, maxLimit, &result)
	result.SQL = formatSQL(selectQuery)

	return result, nil
}

// validateTableReference checks if the FROM clause references the expected table.
// Used in RestrictedMode for LogchefQL queries.
func (qb *QueryBuilder) validateTableReference(stmt *clickhouseparser.SelectQuery) error {
	if stmt.From == nil || stmt.From.Expr == nil {
		return fmt.Errorf("query validation failed: missing FROM clause")
	}

	expectedDB, expectedTable := "", qb.tableName
	if parts := strings.Split(qb.tableName, "."); len(parts) == 2 {
		expectedDB, expectedTable = parts[0], parts[1]
	}

	var tableID *clickhouseparser.TableIdentifier

	switch expr := stmt.From.Expr.(type) {
	case *clickhouseparser.JoinTableExpr:
		if expr.Table == nil || expr.Table.Expr == nil {
			return fmt.Errorf("query validation failed: invalid table expression in FROM clause")
		}
		switch tableExpr := expr.Table.Expr.(type) {
		case *clickhouseparser.TableIdentifier:
			tableID = tableExpr
		case *clickhouseparser.AliasExpr:
			if tid, ok := tableExpr.Expr.(*clickhouseparser.TableIdentifier); ok {
				tableID = tid
			}
		}
	case *clickhouseparser.TableExpr:
		if expr.Expr == nil {
			return fmt.Errorf("query validation failed: invalid table expression in FROM clause")
		}
		if tid, ok := expr.Expr.(*clickhouseparser.TableIdentifier); ok {
			tableID = tid
		}
	case *clickhouseparser.JoinExpr:
		return fmt.Errorf("query validation failed: JOIN clauses are not allowed in restricted mode")
	default:
		return fmt.Errorf("query validation failed: unsupported FROM clause type: %T", expr)
	}

	if tableID == nil {
		return fmt.Errorf("query validation failed: could not identify table in FROM clause")
	}

	return qb.validateTableIdentifier(tableID, expectedDB, expectedTable)
}

// formatSQL renders a parsed AST node back to SQL text. clickhouse-sql-parser
// v0.5 removed the node.String() methods in favor of a Formatter.
func formatSQL(node clickhouseparser.Expr) string {
	f := clickhouseparser.NewFormatter()
	f.WriteExpr(node)
	return f.String()
}

// validateTableIdentifier checks if a TableIdentifier matches the expected database/table.
func (qb *QueryBuilder) validateTableIdentifier(tableID *clickhouseparser.TableIdentifier, expectedDB, expectedTable string) error {
	if tableID.Table == nil {
		return fmt.Errorf("query validation failed: invalid table identifier")
	}
	tableName := tableID.Table.Name

	if tableID.Database != nil {
		dbName := tableID.Database.Name
		if expectedDB != "" && dbName != expectedDB {
			return fmt.Errorf("query validation failed: invalid database reference '%s' (expected '%s')",
				dbName, expectedDB)
		}
		if tableName != expectedTable {
			return fmt.Errorf("query validation failed: invalid table reference '%s.%s' (expected '%s.%s')",
				dbName, tableName, expectedDB, expectedTable)
		}
	} else if tableName != expectedTable {
		expectedFullName := expectedTable
		if expectedDB != "" {
			expectedFullName = expectedDB + "." + expectedTable
		}
		return fmt.Errorf("query validation failed: invalid table reference '%s' (expected '%s')",
			tableName, expectedFullName)
	}
	return nil
}

// checkDangerousOperations performs basic checks for disallowed SQL constructs in restricted mode.
func (qb *QueryBuilder) checkDangerousOperations(stmt *clickhouseparser.SelectQuery) error {
	// In restricted mode (LogchefQL), we don't allow subqueries.
	// This is a simple check - LogchefQL doesn't generate subqueries anyway.
	return nil
}

// ensureLimitWithPolicy applies the LIMIT rule to the outer statement. The
// effective limit is the smallest of the statement's own literal LIMIT, the
// request limit and the configured maximum; the default applies when neither
// the statement nor the request sets one. A clamp changes only the row count,
// so OFFSET stays, and LIMIT n BY and limits inside subqueries or CTEs are
// never touched. When a cap wins, the SQL asks for one row past it.
func (qb *QueryBuilder) ensureLimitWithPolicy(stmt *clickhouseparser.SelectQuery, requestedLimit, defaultLimit, maxLimit int, result *QueryBuildResult) {
	capLimit, capSource := 0, models.LimitSource("")
	if requestedLimit > 0 {
		capLimit, capSource = requestedLimit, models.LimitSourceRequest
	}
	if maxLimit > 0 && (capLimit == 0 || maxLimit < capLimit) {
		capLimit, capSource = maxLimit, models.LimitSourceMax
	}

	if stmt.Limit != nil && stmt.Limit.Limit != nil {
		result.ExplicitLimit = true
		literal, ok := literalLimit(stmt.Limit.Limit)
		switch {
		case !ok:
			// A LIMIT expression cannot be compared or probed. It stands, and
			// the cap bounds the result through max_result_rows and the reader.
			result.AppliedLimit, result.FetchLimit, result.LimitSource = capLimit, capLimit, capSource
			if capLimit == 0 {
				result.LimitSource = models.LimitSourceStatement
			}
		case capLimit == 0 || literal <= capLimit:
			result.OriginalLimit = literal
			result.AppliedLimit, result.FetchLimit, result.LimitSource = literal, literal, models.LimitSourceStatement
		default:
			result.OriginalLimit = literal
			result.LimitCapped = true
			result.AppliedLimit, result.FetchLimit, result.LimitSource = capLimit, capLimit+1, capSource
			stmt.Limit.Limit = numberLiteral(capLimit + 1)
		}
		return
	}

	limit, source := capLimit, capSource
	if requestedLimit <= 0 {
		if defaultLimit <= 0 {
			defaultLimit = DefaultLimit
		}
		limit, source = defaultLimit, models.LimitSourceDefault
		result.LimitAdded = true
		if maxLimit > 0 && maxLimit < limit {
			limit, source = maxLimit, models.LimitSourceMax
		}
	}
	result.LimitCapped = source == models.LimitSourceMax
	result.AppliedLimit, result.FetchLimit, result.LimitSource = limit, limit+1, source
	if stmt.Limit == nil {
		stmt.Limit = &clickhouseparser.LimitClause{}
	}
	stmt.Limit.Limit = numberLiteral(limit + 1)
}

// literalLimit returns the row count of a LIMIT written as a whole number.
func literalLimit(expr clickhouseparser.Expr) (int, bool) {
	number, ok := expr.(*clickhouseparser.NumberLiteral)
	if !ok {
		return 0, false
	}
	limit, err := strconv.Atoi(number.Literal)
	if err != nil || limit < 0 {
		return 0, false
	}
	return limit, true
}

func numberLiteral(n int) *clickhouseparser.NumberLiteral {
	return &clickhouseparser.NumberLiteral{Literal: strconv.Itoa(n)}
}

func legacyDefaultLimit(maxLimit int) int {
	if maxLimit > 0 {
		return maxLimit
	}
	return DefaultLimit
}

// RemoveLimitClause parses the SQL and removes any LIMIT clause.
func (qb *QueryBuilder) RemoveLimitClause(rawSQL string) (string, error) {
	selectQuery, err := parseSelectQuery(rawSQL)
	if err != nil {
		return "", err
	}
	selectQuery.Limit = nil
	return formatSQL(selectQuery), nil
}
