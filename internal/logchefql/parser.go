package logchefql

import (
	"strings"
)

// Parser converts tokens into an AST
type Parser struct {
	tokens   []Token
	position int
	errors   []ParseError
}

// NewParser creates a new parser for the given tokens
func NewParser(tokens []Token) *Parser {
	return &Parser{
		tokens:   tokens,
		position: 0,
		errors:   nil,
	}
}

// ParseResult represents the result of parsing
type ParseResult struct {
	AST    ASTNode
	Errors []ParseError
}

// Parse parses the tokens into an AST
func (p *Parser) Parse() ParseResult {
	if len(p.tokens) == 0 {
		return ParseResult{AST: nil, Errors: nil}
	}

	// Check if this is a query with pipe operator (field selection)
	pipeIndex := -1
	for i, token := range p.tokens {
		if token.Type == TokenPipe {
			pipeIndex = i
			break
		}
	}

	if pipeIndex != -1 {
		// Parse query with field selection: WHERE | SELECT
		whereTokens := p.tokens[:pipeIndex]
		selectTokens := p.tokens[pipeIndex+1:]

		var whereClause ASTNode
		if len(whereTokens) > 0 {
			whereParser := NewParser(whereTokens)
			whereResult := whereParser.Parse()
			if len(whereResult.Errors) > 0 {
				p.errors = append(p.errors, whereResult.Errors...)
			}
			whereClause = whereResult.AST
		}

		selectFields := p.parseSelectFields(selectTokens)

		if len(p.errors) > 0 {
			return ParseResult{AST: nil, Errors: p.errors}
		}

		return ParseResult{
			AST: &QueryNode{
				Where:  whereClause,
				Select: selectFields,
			},
			Errors: p.errors,
		}
	}

	// Regular expression parsing
	ast := p.parseExpression()

	if ast == nil || len(p.errors) > 0 {
		return ParseResult{AST: nil, Errors: p.errors}
	}

	return ParseResult{AST: ast, Errors: p.errors}
}

func (p *Parser) peek(offset int) *Token {
	idx := p.position + offset
	if idx >= 0 && idx < len(p.tokens) {
		return &p.tokens[idx]
	}
	return nil
}

func (p *Parser) consume() *Token {
	if p.position >= len(p.tokens) {
		var pos Position
		if p.position > 0 && len(p.tokens) > 0 {
			lastToken := p.tokens[len(p.tokens)-1]
			pos = Position{
				Line:   lastToken.Position.Line,
				Column: lastToken.Position.Column + len(lastToken.Value),
			}
		} else {
			pos = Position{Line: 1, Column: 1}
		}
		p.errors = append(p.errors, ParseError{
			Code:     ErrUnexpectedEnd,
			Message:  "Unexpected end of query",
			Position: &pos,
		})
		return nil
	}
	token := &p.tokens[p.position]
	p.position++
	return token
}

func (p *Parser) expect(tokenType TokenType, value string) *Token {
	token := p.consume()
	if token == nil {
		return nil
	}

	if token.Type != tokenType || (value != "" && token.Value != value) {
		p.errors = append(p.errors, ParseError{
			Code:     ErrUnexpectedToken,
			Message:  "Unexpected token: " + token.Value,
			Position: &token.Position,
		})
		return nil
	}

	return token
}

func (p *Parser) parseExpression() ASTNode {
	left := p.parsePrimary()
	if left == nil {
		return nil
	}

	return p.parseBinaryExpression(left, 0)
}

func (p *Parser) parseBinaryExpression(left ASTNode, minPrecedence int) ASTNode {
	for {
		token := p.peek(0)
		if token == nil || token.Type != TokenBool {
			break
		}

		precedence := p.getOperatorPrecedence(token.Value)
		if precedence < minPrecedence {
			break
		}

		p.consume() // consume the boolean operator
		boolOp, _ := ParseBoolOperator(token.Value)

		right := p.parsePrimary()
		if right == nil {
			return nil
		}

		// Look ahead for higher precedence operators
		nextToken := p.peek(0)
		for nextToken != nil && nextToken.Type == TokenBool {
			nextPrecedence := p.getOperatorPrecedence(nextToken.Value)
			if nextPrecedence <= precedence {
				break
			}
			right = p.parseBinaryExpression(right, nextPrecedence)
			if right == nil {
				return nil
			}
			nextToken = p.peek(0)
		}

		// Combine into logical node
		left = &LogicalNode{
			Operator: boolOp,
			Children: []ASTNode{left, right},
		}
	}

	return left
}

func (p *Parser) getOperatorPrecedence(op string) int {
	switch strings.ToLower(op) {
	case "or":
		return 1
	case "and":
		return 2
	default:
		return 0
	}
}

func (p *Parser) parsePrimary() ASTNode {
	token := p.peek(0)
	if token == nil {
		return nil
	}

	// Handle parenthesized expressions
	if token.Type == TokenParen && token.Value == "(" {
		return p.parseGroup()
	}

	// Handle key=value expressions
	if token.Type == TokenKey || token.Type == TokenValue {
		return p.parseComparison()
	}

	p.errors = append(p.errors, ParseError{
		Code:     ErrUnexpectedToken,
		Message:  "Unexpected token: " + token.Value,
		Position: &token.Position,
	})
	return nil
}

func (p *Parser) parseGroup() ASTNode {
	openParen := p.consume() // consume '('
	if openParen == nil {
		return nil
	}

	expr := p.parseExpression()
	if expr == nil {
		return nil
	}

	closeParen := p.peek(0)
	if closeParen == nil || closeParen.Type != TokenParen || closeParen.Value != ")" {
		pos := Position{Line: 1, Column: 1}
		if closeParen != nil {
			pos = closeParen.Position
		}
		p.errors = append(p.errors, ParseError{
			Code:     ErrExpectedClosingParen,
			Message:  "Expected closing parenthesis",
			Position: &pos,
		})
		return nil
	}
	p.consume() // consume ')'

	return &GroupNode{Children: []ASTNode{expr}}
}

func (p *Parser) parseComparison() ASTNode {
	keyToken := p.consume()
	if keyToken == nil {
		return nil
	}

	// Parse key - could be simple field or nested field
	var key interface{}
	if strings.Contains(keyToken.Value, ".") {
		key = p.parseNestedFieldFromString(keyToken.Value)
	} else {
		key = keyToken.Value
	}

	// Expect operator
	opToken := p.consume()
	if opToken == nil {
		return nil
	}

	if opToken.Type != TokenOperator {
		p.errors = append(p.errors, ParseError{
			Code:     ErrExpectedOperator,
			Message:  "Expected operator after field name",
			Position: &opToken.Position,
		})
		return nil
	}

	op, ok := ParseOperator(opToken.Value)
	if !ok {
		p.errors = append(p.errors, ParseError{
			Code:     ErrUnknownOperator,
			Message:  "Unknown operator: " + opToken.Value,
			Position: &opToken.Position,
		})
		return nil
	}

	// Expect value
	valueToken := p.consume()
	if valueToken == nil {
		return nil
	}

	if valueToken.Type != TokenValue && valueToken.Type != TokenKey {
		p.errors = append(p.errors, ParseError{
			Code:     ErrExpectedValue,
			Message:  "Expected value after operator",
			Position: &valueToken.Position,
		})
		return nil
	}

	// Parse value
	var value interface{}
	value = valueToken.Value

	// Try to parse as number or boolean if not quoted
	if !valueToken.Quoted {
		if valueToken.Value == "true" {
			value = true
		} else if valueToken.Value == "false" {
			value = false
		} else if valueToken.Value == "null" {
			value = nil
		}
		// Could add number parsing here if needed
	}

	return &ExpressionNode{
		Key:      key,
		Operator: op,
		Value:    value,
		Quoted:   valueToken.Quoted,
	}
}

func (p *Parser) parseNestedFieldFromString(s string) NestedField {
	// Handle quoted segments in the path
	var parts []string
	var current strings.Builder
	inQuote := false
	var quoteChar rune

	for _, r := range s {
		if inQuote {
			if r == quoteChar {
				inQuote = false
				parts = append(parts, current.String())
				current.Reset()
			} else {
				current.WriteRune(r)
			}
		} else if r == '"' || r == '\'' {
			inQuote = true
			quoteChar = r
			if current.Len() > 0 {
				// Push what we have so far
				for _, part := range strings.Split(current.String(), ".") {
					if part != "" {
						parts = append(parts, part)
					}
				}
				current.Reset()
			}
		} else if r == '.' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	if len(parts) == 0 {
		return NestedField{Base: s, Path: nil}
	}

	return NestedField{
		Base: parts[0],
		Path: parts[1:],
	}
}

func (p *Parser) parseSelectFields(tokens []Token) []SelectField {
	var fields []SelectField

	i := 0
	for i < len(tokens) {
		token := tokens[i]

		if token.Type == TokenKey || token.Type == TokenValue {
			field := SelectField{}

			// Parse field name
			if strings.Contains(token.Value, ".") {
				field.Field = p.parseNestedFieldFromString(token.Value)
			} else {
				field.Field = token.Value
			}

			// Check for alias (next token is also a key/value without operator)
			if i+1 < len(tokens) {
				nextToken := tokens[i+1]
				// Simple alias detection - if there's no operator between two keys
				if nextToken.Type == TokenKey || nextToken.Type == TokenValue {
					// This could be an alias or another field
					// For simplicity, we treat comma-separated fields
					// In LogchefQL, fields after pipe are space-separated
				}
			}

			fields = append(fields, field)
		}

		i++
	}

	return fields
}

// DetectMissingBooleanOperators checks for patterns like key=value key=value without and/or
func DetectMissingBooleanOperators(tokens []Token) *ParseError {
	for i := 0; i < len(tokens)-3; i++ {
		if tokens[i].Type == TokenKey &&
			tokens[i+1].Type == TokenOperator &&
			(tokens[i+2].Type == TokenValue || tokens[i+2].Type == TokenKey) &&
			tokens[i+3].Type == TokenKey {

			firstKey := tokens[i].Value
			secondKey := tokens[i+3].Value
			return &ParseError{
				Code:    ErrMissingBooleanOperator,
				Message: "Missing boolean operator (and/or) between conditions: '" + firstKey + "' and '" + secondKey + "'",
				Position: &Position{
					Line:   tokens[i+3].Position.Line,
					Column: tokens[i+3].Position.Column,
				},
			}
		}
	}
	return nil
}
