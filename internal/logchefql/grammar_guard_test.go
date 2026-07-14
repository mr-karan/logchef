package logchefql

import (
	"strings"
	"testing"
)

// nestedParenQuery builds a query with n levels of paren nesting around a
// trivial comparison, e.g. n=2 -> "((a=1))".
func nestedParenQuery(n int) string {
	var b strings.Builder
	b.Grow(2*n + 8)
	for i := 0; i < n; i++ {
		b.WriteByte('(')
	}
	b.WriteString("a=1")
	for i := 0; i < n; i++ {
		b.WriteByte(')')
	}
	return b.String()
}

// TestParseLogchefQLRejectsExcessiveNesting is the core regression test for
// issue #97: a query whose paren nesting exceeds maxParenNestingDepth must be
// rejected with a clean error BEFORE the recursive-descent parser ever runs,
// not by letting the parser recurse and crash. If the guard did not exist (or
// checked after parsing), this test process would die with a fatal "stack
// overflow" error instead of failing normally -- there is no way to recover
// from that in-process, so the only valid proof is that the guard trips
// first and the test completes.
func TestParseLogchefQLRejectsExcessiveNesting(t *testing.T) {
	query := nestedParenQuery(maxParenNestingDepth + 1)

	_, err := ParseLogchefQL(query)
	if err == nil {
		t.Fatal("expected an error for nesting beyond the max depth, got nil")
	}

	parseErr, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected *ParseError, got %T: %v", err, err)
	}
	if parseErr.Code != ErrQueryTooDeeplyNested {
		t.Errorf("expected code %q, got %q (message: %s)", ErrQueryTooDeeplyNested, parseErr.Code, parseErr.Message)
	}
}

// TestParseLogchefQLAllowsBoundaryNesting proves the guard doesn't reject
// legitimate queries: nesting exactly at maxParenNestingDepth must still
// parse successfully.
func TestParseLogchefQLAllowsBoundaryNesting(t *testing.T) {
	query := nestedParenQuery(maxParenNestingDepth)

	pq, err := ParseLogchefQL(query)
	if err != nil {
		t.Fatalf("expected boundary depth (%d) to parse successfully, got error: %v", maxParenNestingDepth, err)
	}
	if pq == nil || pq.Where == nil {
		t.Fatal("expected a parsed query with a Where clause")
	}
}

// TestParseLogchefQLRejectsMassiveNestedQuery reproduces the exact shape of
// attack described in issue #97: a query with hundreds of thousands of nested
// parens (~300-600KB), well under any reasonable HTTP body limit. It must
// return a clean error, not crash the process.
func TestParseLogchefQLRejectsMassiveNestedQuery(t *testing.T) {
	const depth = 200_000
	query := nestedParenQuery(depth)

	t.Logf("query length: %d bytes", len(query))

	_, err := ParseLogchefQL(query)
	if err == nil {
		t.Fatal("expected a clean error for a massively nested query, got nil")
	}

	parseErr, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected *ParseError, got %T: %v", err, err)
	}
	if parseErr.Code != ErrQueryTooDeeplyNested && parseErr.Code != ErrQueryTooLong {
		t.Errorf("expected ErrQueryTooDeeplyNested or ErrQueryTooLong, got %q (message: %s)", parseErr.Code, parseErr.Message)
	}
}

// TestValidateRejectsExcessiveNesting confirms the guard is applied through
// Validate, the exact call used by handleLogchefQLValidate (the live-editor
// keystroke endpoint called out in issue #97), not just via the low-level
// ParseLogchefQL entry point.
func TestValidateRejectsExcessiveNesting(t *testing.T) {
	query := nestedParenQuery(maxParenNestingDepth + 1)

	result := Validate(query)
	if result.Valid {
		t.Fatal("expected Validate to report the query as invalid")
	}
	if result.Error == nil {
		t.Fatal("expected a non-nil error")
	}
	if result.Error.Code != ErrQueryTooDeeplyNested {
		t.Errorf("expected code %q, got %q", ErrQueryTooDeeplyNested, result.Error.Code)
	}
}

// TestTranslateRejectsExcessiveNesting confirms the guard is applied through
// Translate, used by handleLogchefQLTranslate/Query and the field-values
// filter build path.
func TestTranslateRejectsExcessiveNesting(t *testing.T) {
	query := nestedParenQuery(maxParenNestingDepth + 1)

	result := Translate(query, nil)
	if result.Valid {
		t.Fatal("expected Translate to report the query as invalid")
	}
	if result.Error == nil {
		t.Fatal("expected a non-nil error")
	}
	if result.Error.Code != ErrQueryTooDeeplyNested {
		t.Errorf("expected code %q, got %q", ErrQueryTooDeeplyNested, result.Error.Code)
	}
}

// TestTranslateToLogsQLRejectsExcessiveNesting confirms the guard also
// applies to the VictoriaLogs translation path (VL TranslateToLogsQL, another
// entry point named in issue #97), which funnels through the same
// ParseLogchefQL choke point.
func TestTranslateToLogsQLRejectsExcessiveNesting(t *testing.T) {
	query := nestedParenQuery(maxParenNestingDepth + 1)

	result := TranslateToLogsQL(query, nil)
	if result.Valid {
		t.Fatal("expected TranslateToLogsQL to report the query as invalid")
	}
	if result.Error == nil {
		t.Fatal("expected a non-nil error")
	}
	if result.Error.Code != ErrQueryTooDeeplyNested {
		t.Errorf("expected code %q, got %q", ErrQueryTooDeeplyNested, result.Error.Code)
	}
}

// TestParseLogchefQLRejectsExcessiveLength confirms the standalone
// query-length cap (independent of nesting) also returns a clean error.
func TestParseLogchefQLRejectsExcessiveLength(t *testing.T) {
	query := "field=\"" + strings.Repeat("x", maxQueryLength+1) + "\""

	_, err := ParseLogchefQL(query)
	if err == nil {
		t.Fatal("expected an error for an overlong query, got nil")
	}

	parseErr, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected *ParseError, got %T: %v", err, err)
	}
	if parseErr.Code != ErrQueryTooLong {
		t.Errorf("expected code %q, got %q", ErrQueryTooLong, parseErr.Code)
	}
}

// TestParenNestingInsideStringLiteralsNotCounted confirms parens that appear
// inside quoted string literals (which are not structural to the grammar) do
// not count against the nesting depth.
func TestParenNestingInsideStringLiteralsNotCounted(t *testing.T) {
	var b strings.Builder
	b.WriteString(`field="`)
	b.WriteString(strings.Repeat("(", maxParenNestingDepth*2))
	b.WriteString(`"`)
	query := b.String()

	_, err := ParseLogchefQL(query)
	if err != nil {
		t.Fatalf("expected parens inside a string literal to be ignored by the nesting guard, got error: %v", err)
	}
}
