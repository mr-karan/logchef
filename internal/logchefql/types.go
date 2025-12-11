// Package logchefql provides parsing and SQL generation for the LogchefQL query language.
// LogchefQL is a simple query language for filtering logs, designed to be user-friendly
// while translating to efficient ClickHouse SQL queries.
package logchefql

// Operator represents comparison operators in LogchefQL
type Operator string

const (
	OpEquals    Operator = "="
	OpNotEquals Operator = "!="
	OpRegex     Operator = "~"
	OpNotRegex  Operator = "!~"
	OpGT        Operator = ">"
	OpLT        Operator = "<"
	OpGTE       Operator = ">="
	OpLTE       Operator = "<="
)

// BoolOperator represents boolean operators for combining conditions
type BoolOperator string

const (
	BoolAnd BoolOperator = "AND"
	BoolOr  BoolOperator = "OR"
)

// TokenType represents the type of a lexical token
type TokenType string

const (
	TokenKey      TokenType = "key"
	TokenOperator TokenType = "operator"
	TokenValue    TokenType = "value"
	TokenParen    TokenType = "paren"
	TokenBool     TokenType = "bool"
	TokenPipe     TokenType = "pipe"
)

// Position represents a position in the source query string
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Token represents a lexical token from the tokenizer
type Token struct {
	Type       TokenType `json:"type"`
	Value      string    `json:"value"`
	Position   Position  `json:"position"`
	Quoted     bool      `json:"quoted,omitempty"`
	Incomplete bool      `json:"incomplete,omitempty"`
}

// NestedField represents a field with nested path access (e.g., log.level or attributes["key"])
type NestedField struct {
	Base string   `json:"base"`
	Path []string `json:"path"`
}

// SelectField represents a field selection with optional alias
type SelectField struct {
	Field interface{} `json:"field"` // string or NestedField
	Alias string      `json:"alias,omitempty"`
}

// ASTNode is the interface for all AST node types
type ASTNode interface {
	nodeType() string
}

// ExpressionNode represents a single comparison expression (e.g., field="value")
type ExpressionNode struct {
	Key      interface{} `json:"key"` // string or NestedField
	Operator Operator    `json:"operator"`
	Value    interface{} `json:"value"` // string, number, bool, or nil
	Quoted   bool        `json:"quoted,omitempty"`
}

func (e *ExpressionNode) nodeType() string { return "expression" }

// LogicalNode represents a logical combination of nodes (AND/OR)
type LogicalNode struct {
	Operator BoolOperator `json:"operator"`
	Children []ASTNode    `json:"children"`
}

func (l *LogicalNode) nodeType() string { return "logical" }

// GroupNode represents a parenthesized group of expressions
type GroupNode struct {
	Children []ASTNode `json:"children"`
}

func (g *GroupNode) nodeType() string { return "group" }

// QueryNode represents the top-level query with optional WHERE and SELECT
type QueryNode struct {
	Where  ASTNode       `json:"where,omitempty"`
	Select []SelectField `json:"select,omitempty"`
}

func (q *QueryNode) nodeType() string { return "query" }

// ParseError represents an error encountered during parsing
type ParseError struct {
	Code     string    `json:"code"`
	Message  string    `json:"message"`
	Position *Position `json:"position,omitempty"`
}

func (e *ParseError) Error() string {
	return e.Message
}

// Error codes for parse errors
const (
	ErrUnterminatedString     = "UNTERMINATED_STRING"
	ErrUnexpectedEnd          = "UNEXPECTED_END"
	ErrUnexpectedToken        = "UNEXPECTED_TOKEN"
	ErrExpectedOperator       = "EXPECTED_OPERATOR"
	ErrExpectedValue          = "EXPECTED_VALUE"
	ErrExpectedClosingParen   = "EXPECTED_CLOSING_PAREN"
	ErrUnknownOperator        = "UNKNOWN_OPERATOR"
	ErrUnknownBooleanOperator = "UNKNOWN_BOOLEAN_OPERATOR"
	ErrInvalidTokenType       = "INVALID_TOKEN_TYPE"
	ErrMissingBooleanOperator = "MISSING_BOOLEAN_OPERATOR"
)

// ColumnInfo represents column metadata from the schema
type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Schema represents table schema information for type-aware SQL generation
type Schema struct {
	Columns []ColumnInfo `json:"columns"`
}

// FilterCondition represents a single filter condition extracted from the query
// This is used for the field sidebar feature
type FilterCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
	IsRegex  bool   `json:"is_regex"`
}

// TranslateResult represents the result of translating a LogchefQL query
type TranslateResult struct {
	SQL          string            `json:"sql"`                     // WHERE clause conditions only
	SelectClause string            `json:"select_clause,omitempty"` // Custom SELECT clause if pipe operator used
	Valid        bool              `json:"valid"`
	Error        *ParseError       `json:"error,omitempty"`
	Conditions   []FilterCondition `json:"conditions"`
	FieldsUsed   []string          `json:"fields_used"`
}

// ValidateResult represents the result of validating a LogchefQL query
type ValidateResult struct {
	Valid bool        `json:"valid"`
	Error *ParseError `json:"error,omitempty"`
}

// ParseOperator converts a string to an Operator, returning ok=false if invalid
func ParseOperator(s string) (Operator, bool) {
	switch s {
	case "=":
		return OpEquals, true
	case "!=":
		return OpNotEquals, true
	case "~":
		return OpRegex, true
	case "!~":
		return OpNotRegex, true
	case ">":
		return OpGT, true
	case "<":
		return OpLT, true
	case ">=":
		return OpGTE, true
	case "<=":
		return OpLTE, true
	default:
		return "", false
	}
}

// ParseBoolOperator converts a string to a BoolOperator, returning ok=false if invalid
func ParseBoolOperator(s string) (BoolOperator, bool) {
	switch s {
	case "and", "AND":
		return BoolAnd, true
	case "or", "OR":
		return BoolOr, true
	default:
		return "", false
	}
}
