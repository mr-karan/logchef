import type { ASTNode, Token, ParseError, Value, NestedField } from './types';
import { Operator, BoolOperator } from './types';

export class QueryParser {
  private position = 0;
  private tokens: Token[];
  private errors: ParseError[] = [];

  constructor(tokens: Token[]) {
    this.tokens = tokens;
  }

  public parse(): { ast: ASTNode | null; errors: ParseError[] } {
    if (this.tokens.length === 0) {
      return { ast: null, errors: [] };
    }

    try {
      const ast = this.parseExpression();
      return { ast, errors: this.errors };
    } catch (error) {
      if (error instanceof Error) {
        const token = this.position < this.tokens.length
          ? this.tokens[this.position]
          : this.tokens[this.tokens.length - 1];

        this.errors.push({
          code: 'PARSE_ERROR',
          message: error.message,
          position: token?.position
        });
      }
      return { ast: null, errors: this.errors };
    }
  }

  private peek(offset = 0): Token | undefined {
    return this.tokens[this.position + offset];
  }

  private consume(): Token {
    const token = this.tokens[this.position];
    if (!token) {
      throw new Error('Unexpected end of input');
    }
    this.position++;
    return token;
  }

  private expect(type: Token['type'], value?: string): Token {
    const token = this.consume();
    if (token.type !== type || (value !== undefined && token.value !== value)) {
      throw new Error(
        `Expected ${type}${value ? ` with value "${value}"` : ''}, but got ${token.type} with value "${token.value}"`
      );
    }
    return token;
  }

  private error(message: string, token: Token): ParseError {
    return {
      code: 'PARSE_ERROR',
      message,
      position: token.position
    };
  }

  private parseExpression(): ASTNode {
    let left = this.parsePrimary();

    while (this.peek()?.type === 'bool') {
      const operatorToken = this.consume();
      const operator = this.mapToBoolOperator(operatorToken.value);
      const right = this.parsePrimary();

      // If we already have a logical node as left, and with the same operator,
      // we can add the right child to its children array
      if (left.type === 'logical' && left.operator === operator) {
        left.children.push(right);
      } else {
        // Otherwise create a new logical node
        left = {
          type: 'logical',
          operator,
          children: [left, right]
        };
      }
    }

    // Check if there's more content after this expression but no boolean operator
    // This would indicate a potential missing boolean operator between conditions
    if (this.peek() && this.peek()?.type === 'key') {
      const nextToken = this.peek()!;
      this.errors.push({
        code: 'PARSE_ERROR',
        message: `Missing boolean operator (and/or) before '${nextToken.value}'`,
        position: nextToken.position
      });

      // Continue parsing to give the best effort result
      const right = this.parsePrimary();
      // Assume an AND operator to give a best-effort parse
      if (left.type === 'logical' && left.operator === BoolOperator.AND) {
        left.children.push(right);
      } else {
        left = {
          type: 'logical',
          operator: BoolOperator.AND,
          children: [left, right]
        };
      }
    }

    return left;
  }

  private parsePrimary(): ASTNode {
    const token = this.peek();

    if (!token) {
      throw new Error('Unexpected end of input');
    }

    if (token.type === 'paren' && token.value === '(') {
      this.consume(); // Consume the open paren
      const expressions: ASTNode[] = [];

      // Parse expressions until we hit a closing paren
      while (this.peek() && !(this.peek()?.type === 'paren' && this.peek()?.value === ')')) {
        expressions.push(this.parseExpression());
      }

      // Consume the closing paren
      if (this.peek()?.type === 'paren' && this.peek()?.value === ')') {
        this.consume();
      } else {
        throw new Error('Expected closing parenthesis');
      }

      // If there's only one expression in the group, we don't need the group wrapper
      if (expressions.length === 1) {
        return expressions[0];
      }

      return {
        type: 'group',
        children: expressions
      };
    }

    // Handle key-operator-value expressions
    if (token.type === 'key') {
      const keyToken = this.consume();
      const operatorToken = this.expect('operator');
      const valueToken = this.consume();

      if (valueToken.type !== 'value' && valueToken.type !== 'key') {
        throw new Error(`Expected value but got ${valueToken.type}`);
      }

      return {
        type: 'expression',
        key: this.parseNestedField(keyToken.value),
        operator: this.mapToOperator(operatorToken.value),
        value: this.parseValue(valueToken.value, (valueToken as any).quoted)
      };
    }

    throw new Error(`Unexpected token type: ${token.type}`);
  }

  private mapToOperator(operator: string): Operator {
    switch (operator) {
      case '=': return Operator.EQUALS;
      case '!=': return Operator.NOT_EQUALS;
      case '~': return Operator.REGEX;
      case '!~': return Operator.NOT_REGEX;
      case '>': return Operator.GT;
      case '<': return Operator.LT;
      case '>=': return Operator.GTE;
      case '<=': return Operator.LTE;
      default:
        throw new Error(`Unknown operator: ${operator}`);
    }
  }

  private mapToBoolOperator(operator: string): BoolOperator {
    const normalizedOp = operator.toUpperCase();
    if (normalizedOp === 'AND') return BoolOperator.AND;
    if (normalizedOp === 'OR') return BoolOperator.OR;
    throw new Error(`Unknown boolean operator: ${operator}`);
  }

  private parseValue(value: string, quoted?: boolean): Value {
    // If explicitly quoted, always treat as string
    if (quoted) {
      return value;
    }

    // Only unquoted literals are coerced
    if (value === 'null' || value === 'NULL') return null;
    if (value === 'true' || value === 'TRUE') return true;
    if (value === 'false' || value === 'FALSE') return false;

    // Numbers: harden to avoid precision loss
    // Decimal numbers
    if (/^-?\d+\.\d+$/.test(value)) {
      return Number(value);
    }
    // Integers: only coerce if safe
    if (/^-?\d+$/.test(value)) {
      try {
        const bi = BigInt(value);
        if (bi <= BigInt(Number.MAX_SAFE_INTEGER) && bi >= BigInt(Number.MIN_SAFE_INTEGER)) {
          return Number(value);
        }
        // Unsafe integer range: keep as string
        return value;
      } catch {
        // If BigInt parsing fails, keep as string
        return value;
      }
    }

    // Best-effort: if the user somehow provided quotes in value, strip them
    if ((value.startsWith('"') && value.endsWith('"')) ||
        (value.startsWith("'") && value.endsWith("'"))) {
      return value.slice(1, -1);
    }

    return value;
  }

  private parseNestedField(fieldValue: string): string | NestedField {
    // Check if field contains dots (nested access)
    if (!fieldValue.includes('.')) {
      return fieldValue;
    }

    // Split on dots, handling quoted segments
    const segments: string[] = [];
    let current = '';
    let inQuotes = false;
    let quoteChar = '';
    let i = 0;

    while (i < fieldValue.length) {
      const char = fieldValue[i];

      if (!inQuotes && (char === '"' || char === "'")) {
        inQuotes = true;
        quoteChar = char;
        current += char;
      } else if (inQuotes && char === quoteChar) {
        inQuotes = false;
        current += char;
        quoteChar = '';
      } else if (!inQuotes && char === '.') {
        if (current.trim()) {
          segments.push(current.trim());
        }
        current = '';
      } else {
        current += char;
      }
      i++;
    }

    // Add final segment
    if (current.trim()) {
      segments.push(current.trim());
    }

    // If we have multiple segments, create a NestedField
    if (segments.length > 1) {
      return {
        base: segments[0],
        path: segments.slice(1)
      };
    }

    // Fallback to simple field name
    return fieldValue;
  }
}