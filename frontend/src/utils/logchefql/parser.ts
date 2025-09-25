import type { ASTNode, Token, ParseError, Value, NestedField, SelectField } from './types';
import { Operator, BoolOperator } from './types';
import {
  createUnexpectedEndError,
  createUnexpectedTokenError,
  createExpectedOperatorError,
  createExpectedValueError,
  createExpectedClosingParenError,
  createUnknownOperatorError,
  createUnknownBooleanOperatorError,
  createInvalidTokenTypeError
} from './errors';

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

    // Check if this is a query with pipe operator (field selection)
    const pipeIndex = this.tokens.findIndex(token => token.type === 'pipe');

    if (pipeIndex !== -1) {
      // Parse query with field selection: WHERE | SELECT
      const whereTokens = this.tokens.slice(0, pipeIndex);
      const selectTokens = this.tokens.slice(pipeIndex + 1);

      let whereClause: ASTNode | undefined;
      if (whereTokens.length > 0) {
        const whereParser = new QueryParser(whereTokens);
        const whereResult = whereParser.parse();
        if (whereResult.errors.length > 0) {
          this.errors.push(...whereResult.errors);
        }
        whereClause = whereResult.ast || undefined;
      }

      // Parse select fields - this should also collect errors
      const selectFields = this.parseSelectFields(selectTokens);

      // If we have errors, return null AST but still return collected errors
      if (this.errors.length > 0) {
        return { ast: null, errors: this.errors };
      }

      return {
        ast: {
          type: 'query',
          where: whereClause,
          select: selectFields
        },
        errors: this.errors
      };
    } else {
      // Regular expression parsing
      const ast = this.parseExpression();

      // If parsing failed or we have errors, return null AST
      if (!ast || this.errors.length > 0) {
        return { ast: null, errors: this.errors };
      }

      return { ast, errors: this.errors };
    }
  }

  private peek(offset = 0): Token | undefined {
    return this.tokens[this.position + offset];
  }

  private consume(): Token | null {
    const token = this.tokens[this.position];
    if (!token) {
      // Use the last token's position if available, otherwise estimate
      const lastToken = this.tokens[this.position - 1];
      const position = lastToken ? {
        line: lastToken.position.line,
        column: lastToken.position.column + lastToken.value.length
      } : { line: 1, column: 1 };
      this.errors.push(createUnexpectedEndError(position));
      return null;
    }
    this.position++;
    return token;
  }

  private expect(type: Token['type'], value?: string): Token | null {
    const token = this.consume();
    if (!token) {
      return null;
    }

    if (token.type !== type || (value !== undefined && token.value !== value)) {
      this.errors.push(createUnexpectedTokenError(
        `${token.type}:"${token.value}"`,
        token.position
      ));
      return null;
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

  private parseExpression(): ASTNode | null {
    let left = this.parsePrimary();
    if (!left) return null;

    while (this.peek()?.type === 'bool') {
      const operatorToken = this.consume();
      if (!operatorToken) break;

      const operator = this.mapToBoolOperator(operatorToken.value);
      if (!operator) break;

      const right = this.parsePrimary();
      if (!right) break;

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
      if (right) {
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
    }

    return left;
  }

  private parsePrimary(): ASTNode | null {
    const token = this.peek();

    if (!token) {
      // Use the last token's position if available, otherwise estimate
      const lastToken = this.tokens[this.position - 1];
      const position = lastToken ? {
        line: lastToken.position.line,
        column: lastToken.position.column + lastToken.value.length
      } : { line: 1, column: 1 };
      this.errors.push(createUnexpectedEndError(position));
      return null;
    }

    if (token.type === 'paren' && token.value === '(') {
      const openParen = this.consume(); // Consume the open paren
      if (!openParen) return null;

      const expressions: ASTNode[] = [];

      // Parse expressions until we hit a closing paren
      while (this.peek() && !(this.peek()?.type === 'paren' && this.peek()?.value === ')')) {
        const expr = this.parseExpression();
        if (expr) {
          expressions.push(expr);
        }
      }

      // Consume the closing paren
      if (this.peek()?.type === 'paren' && this.peek()?.value === ')') {
        this.consume();
      } else {
        const currentToken = this.peek();
        this.errors.push(createExpectedClosingParenError(currentToken?.position));
        return null;
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
      if (!keyToken) return null;

      const operatorToken = this.expect('operator');
      if (!operatorToken) return null;

      const valueToken = this.consume();
      if (!valueToken) return null;

      if (valueToken.type !== 'value' && valueToken.type !== 'key') {
        this.errors.push(createExpectedValueError(valueToken.position));
        return null;
      }

      const operator = this.mapToOperator(operatorToken.value);
      if (!operator) return null;

      const quoted = Boolean((valueToken as any).quoted);
      return {
        type: 'expression',
        key: this.parseNestedField(keyToken.value),
        operator,
        value: this.parseValue(valueToken.value, quoted),
        ...(quoted ? { quoted: true } : {}) // Only include if quoted
      };
    }

    this.errors.push(createInvalidTokenTypeError(token.type, token.position));
    return null;
  }

  private mapToOperator(operator: string): Operator | null {
    switch (operator) {
      case '=':
      case '==': return Operator.EQUALS;
      case '!=': return Operator.NOT_EQUALS;
      case '~': return Operator.REGEX;
      case '!~': return Operator.NOT_REGEX;
      case '>': return Operator.GT;
      case '<': return Operator.LT;
      case '>=': return Operator.GTE;
      case '<=': return Operator.LTE;
      default:
        const token = this.tokens[this.position - 1]; // Get the operator token we just processed
        this.errors.push(createUnknownOperatorError(operator, token?.position));
        return null;
    }
  }

  private mapToBoolOperator(operator: string): BoolOperator | null {
    const normalizedOp = operator.toUpperCase();
    if (normalizedOp === 'AND') return BoolOperator.AND;
    if (normalizedOp === 'OR') return BoolOperator.OR;

    const token = this.tokens[this.position - 1]; // Get the boolean operator token we just processed
    this.errors.push(createUnknownBooleanOperatorError(operator, token?.position));
    return null;
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

  private parseSelectFields(tokens: Token[]): SelectField[] {
    if (tokens.length === 0) {
      return [];
    }

    const fields: SelectField[] = [];
    let i = 0;

    while (i < tokens.length) {
      const token = tokens[i];

      if (token.type === 'key') {
        const field = this.parseNestedField(token.value);
        fields.push({ field });
        i++;

        // Skip comma if present (future enhancement for comma-separated fields)
        if (i < tokens.length && tokens[i].value === ',') {
          i++;
        }
      } else {
        // Collect error for unexpected token in SELECT clause
        this.errors.push(createUnexpectedTokenError(
          `${token.type}:"${token.value}"`,
          token.position
        ));
        i++;
      }
    }

    return fields;
  }
}