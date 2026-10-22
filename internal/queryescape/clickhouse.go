// Package queryescape encodes data for ClickHouse SQL literals and identifiers.
package queryescape

import "strings"

var stringEscapes = strings.NewReplacer("\\", "\\\\", "'", "''", "\x00", "\\0", "\r", "\\r", "\n", "\\n")

// Hex escapes keep embedded backticks inside one token in the SQL parser.
var identifierEscapes = strings.NewReplacer("\\", "\\\\", "`", "\\x60")

// ClickHouseString escapes the contents of a single-quoted string literal.
func ClickHouseString(value string) string {
	return stringEscapes.Replace(value)
}

// ClickHouseIdentifier quotes a raw identifier as one backtick-delimited token.
func ClickHouseIdentifier(name string) string {
	return "`" + identifierEscapes.Replace(name) + "`"
}
