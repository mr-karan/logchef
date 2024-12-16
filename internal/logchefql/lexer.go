package logchefql

import "github.com/alecthomas/participle/v2/lexer"

var LogchefLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "whitespace", Pattern: `\s+`},
	{Name: "String", Pattern: `'[^']*'|"[^"]*"`},
	{Name: "RelativeTime", Pattern: `-\d+[smhdy]`},
	{Name: "Number", Pattern: `\d+`},
	{Name: "Operator", Pattern: `>=|<=|=|!=|~|!~|>|<`},
	{Name: "Punct", Pattern: `[.;]`},
	{Name: "Ident", Pattern: `[a-zA-Z_][a-zA-Z0-9_]*`},
})
