package logchefql

import (
	"strings"
	"unicode"
)

// operatorChars contains characters that can start or be part of operators
var operatorChars = map[rune]bool{
	'=': true,
	'!': true,
	'~': true,
	'>': true,
	'<': true,
}

// isKeyChar returns true if the rune can be part of a key/field name
func isKeyChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == ':' || r == '-'
}

// isWhitespace returns true if the rune is whitespace
func isWhitespace(r rune) bool {
	return unicode.IsSpace(r)
}

// TokenizeResult represents the result of tokenization
type TokenizeResult struct {
	Tokens []Token
	Errors []ParseError
}

// couldBeKey determines if a quote at the current position could be the start of a quoted key
func couldBeKey(tokens []Token, current *tokenBuilder) bool {
	if len(tokens) == 0 {
		return true
	}

	lastToken := tokens[len(tokens)-1]
	if lastToken.Type == TokenBool || lastToken.Type == TokenParen || lastToken.Type == TokenPipe {
		return true
	}

	if current == nil && lastToken.Type != TokenOperator {
		return true
	}

	return false
}

// tokenBuilder helps build tokens during tokenization
type tokenBuilder struct {
	tokenType TokenType
	value     strings.Builder
	line      int
	column    int
	quoted    bool
}

func newTokenBuilder(tt TokenType, line, column int) *tokenBuilder {
	return &tokenBuilder{
		tokenType: tt,
		line:      line,
		column:    column,
	}
}

func (tb *tokenBuilder) append(r rune) {
	tb.value.WriteRune(r)
}

func (tb *tokenBuilder) appendString(s string) {
	tb.value.WriteString(s)
}

func (tb *tokenBuilder) build() Token {
	t := Token{
		Type:     tb.tokenType,
		Value:    tb.value.String(),
		Position: Position{Line: tb.line, Column: tb.column},
	}
	if tb.quoted {
		t.Quoted = true
	}
	return t
}

// parseNestedField parses a potentially nested field name with quoted segments
func parseNestedField(input []rune, startPos int) (fieldValue string, endPos int, hasQuotedSegments bool) {
	pos := startPos
	var result strings.Builder
	inQuotedSegment := false
	var quoteChar rune

	for pos < len(input) {
		char := input[pos]

		if inQuotedSegment {
			result.WriteRune(char)
			if char == quoteChar {
				inQuotedSegment = false
				hasQuotedSegments = true
				quoteChar = 0
			}
			pos++
		} else {
			if char == '"' || char == '\'' {
				inQuotedSegment = true
				quoteChar = char
				result.WriteRune(char)
				pos++
			} else if isKeyChar(char) || char == '.' {
				result.WriteRune(char)
				pos++
			} else if isWhitespace(char) || operatorChars[char] || char == '(' || char == ')' || char == '|' {
				break
			} else {
				result.WriteRune(char)
				pos++
			}
		}
	}

	return result.String(), pos, hasQuotedSegments
}

// Tokenize converts a LogchefQL query string into a sequence of tokens
func Tokenize(input string) TokenizeResult {
	runes := []rune(input)
	var tokens []Token
	var errors []ParseError
	var current *tokenBuilder
	line := 1
	column := 1
	inEscape := false
	inString := false
	var stringDelimiter rune

	pushCurrent := func() {
		if current != nil {
			tokens = append(tokens, current.build())
			current = nil
		}
	}

	for i := 0; i < len(runes); i++ {
		char := runes[i]

		// Handle line breaks
		if char == '\n' {
			line++
			column = 1
			if !inString {
				pushCurrent()
			} else if current != nil {
				current.append(char)
			}
			continue
		}

		// Handle string literals with proper escaping
		if inString {
			if inEscape {
				if current != nil {
					current.append(char)
				}
				inEscape = false
			} else if char == '\\' {
				inEscape = true
			} else if char == stringDelimiter {
				pushCurrent()
				inString = false
				stringDelimiter = 0
			} else {
				if current != nil {
					current.append(char)
				}
			}
			column++
			continue
		}

		// Handle boolean operators FIRST (match whole words only)
		if unicode.IsLetter(char) {
			// Extract the word
			wordEnd := i
			for wordEnd < len(runes) && unicode.IsLetter(runes[wordEnd]) {
				wordEnd++
			}
			word := string(runes[i:wordEnd])
			lower := strings.ToLower(word)

			// Check word boundaries
			prevChar := rune(0)
			if i > 0 {
				prevChar = runes[i-1]
			}
			nextChar := rune(0)
			if wordEnd < len(runes) {
				nextChar = runes[wordEnd]
			}

			validPrev := prevChar == 0 || isWhitespace(prevChar) || operatorChars[prevChar] || prevChar == '(' || prevChar == ')' || prevChar == '|'
			validNext := nextChar == 0 || isWhitespace(nextChar) || operatorChars[nextChar] || nextChar == '(' || nextChar == ')' || nextChar == '|'

			if (lower == "and" || lower == "or") && validPrev && validNext {
				pushCurrent()
				tokens = append(tokens, Token{
					Type:     TokenBool,
					Value:    lower,
					Position: Position{Line: line, Column: column},
				})
				i = wordEnd - 1
				column += len(word)
				continue
			}
		}

		// Handle key characters and nested field access
		if isKeyChar(char) || ((char == '"' || char == '\'') && couldBeKey(tokens, current)) {
			if current == nil || (current.tokenType != TokenKey && current.tokenType != TokenValue) {
				pushCurrent()

				// Try to parse as nested field
				fieldValue, endPos, hasQuoted := parseNestedField(runes, i)

				if strings.Contains(fieldValue, ".") || hasQuoted || fieldValue != string(char) {
					current = newTokenBuilder(TokenKey, line, column)
					current.appendString(fieldValue)
					current.quoted = hasQuoted

					consumed := endPos - i
					i = endPos - 1
					column += consumed
					continue
				}

				current = newTokenBuilder(TokenKey, line, column)
				current.append(char)
			} else {
				current.append(char)
			}
			column++
			continue
		}

		// Start string literals
		if (char == '"' || char == '\'') && !inString {
			pushCurrent()
			inString = true
			stringDelimiter = char
			current = newTokenBuilder(TokenValue, line, column)
			current.quoted = true
			column++
			continue
		}

		// Handle whitespace
		if isWhitespace(char) {
			pushCurrent()
			column++
			continue
		}

		// Handle parentheses
		if char == '(' || char == ')' {
			pushCurrent()
			tokens = append(tokens, Token{
				Type:     TokenParen,
				Value:    string(char),
				Position: Position{Line: line, Column: column},
			})
			column++
			continue
		}

		// Handle pipe operator
		if char == '|' {
			pushCurrent()
			tokens = append(tokens, Token{
				Type:     TokenPipe,
				Value:    string(char),
				Position: Position{Line: line, Column: column},
			})
			column++
			continue
		}

		// Handle operator characters
		if operatorChars[char] {
			peek := rune(0)
			if i+1 < len(runes) {
				peek = runes[i+1]
			}

			if current != nil && current.tokenType == TokenOperator {
				current.append(char)
			} else {
				pushCurrent()
				current = newTokenBuilder(TokenOperator, line, column)
				current.append(char)
			}

			// Look ahead for compound operators
			if (char == '>' || char == '<' || char == '!' || char == '=') && peek == '=' {
				current.append('=')
				i++
				column += 2
				continue
			}

			column++
			continue
		}

		// Handle alphabetic characters as part of keys/values
		if unicode.IsLetter(char) {
			if current == nil {
				current = newTokenBuilder(TokenKey, line, column)
				current.append(char)
			} else {
				current.append(char)
			}
			column++
			continue
		}

		// Handle other characters as part of values
		if current == nil {
			current = newTokenBuilder(TokenValue, line, column)
			current.append(char)
		} else {
			current.append(char)
		}
		column++
	}

	// Push any remaining token
	pushCurrent()

	// Check for unterminated string literals
	if inString {
		currentValue := ""
		if current != nil {
			currentValue = current.value.String()
		}
		preview := currentValue
		if len(preview) > 20 {
			preview = preview[:20] + "..."
		}

		errors = append(errors, ParseError{
			Code:    ErrUnterminatedString,
			Message: "Unterminated string literal starting with " + string(stringDelimiter) + preview + string(stringDelimiter) + ". Missing closing quote.",
			Position: &Position{
				Line:   line,
				Column: column,
			},
		})

		// Recovery: push the current string token anyway
		if current != nil {
			t := current.build()
			t.Incomplete = true
			tokens = append(tokens, t)
		}
	}

	return TokenizeResult{
		Tokens: tokens,
		Errors: errors,
	}
}

