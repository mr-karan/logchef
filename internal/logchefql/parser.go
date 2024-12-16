package logchefql

import (
	"github.com/alecthomas/participle/v2"
)

var parser = participle.MustBuild[Query](
	participle.Lexer(LogchefLexer),
	participle.Elide("whitespace"),
	participle.UseLookahead(4),
	participle.Unquote("String"),
)

// Parse parses a LogchefQL query string and returns an AST
func Parse(query string) (*Query, error) {
	return parser.ParseString("", query)
}

// MustParse is like Parse but panics on error
func MustParse(query string) *Query {
	ast, err := Parse(query)
	if err != nil {
		panic(err)
	}
	return ast
}
