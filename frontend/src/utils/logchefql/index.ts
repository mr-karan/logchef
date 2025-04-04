// Re-export all necessary components from the modernized implementation

import { tokenize } from './tokenizer';
import { QueryParser } from './parser';
import { SQLVisitor } from './sql-generator';
import type { ParseError, Token } from './types';
import { Operator as OperatorEnum, BoolOperator } from './types';

// Export types and enums
export { Operator as OperatorEnum, BoolOperator } from './types';
export type { ASTNode, Token, ParseError, Value } from './types';

// Export modernized implementation
export { tokenize } from './tokenizer';
export { QueryParser } from './parser';
export { SQLVisitor } from './sql-generator';

// Define parser state enum for the editor
export enum State {
  INITIAL = 'INITIAL',
  KEY = 'KEY',
  KEY_VALUE_OPERATOR = 'KEY_VALUE_OPERATOR',
  EXPECT_BOOL_OP = 'EXPECT_BOOL_OP',
  BOOL_OP_DELIMITER = 'BOOL_OP_DELIMITER'
}

// Define Operator enum for the editor (renamed to avoid conflicts with the original Operator enum)
export const Operator = {
  EQUALS: '=',
  NOT_EQUALS: '!=',
  REGEX: '~',
  NOT_REGEX: '!~',
  GT: '>',
  LT: '<',
  GTE: '>=',
  LTE: '<='
};

// Define and export valid key-value operators
export const VALID_KEY_VALUE_OPERATORS = ['=', '!=', '>', '<', '>=', '<=', '~', '!~'];

// Define and export tokenTypes for Monaco editor integration
export const tokenTypes = [
  'key',       // For field/property keys
  'operator',  // For operators like =, !=, >, <, etc.
  'value',     // For literal values
  'paren',     // For parentheses
  'bool'       // For boolean operators like AND, OR
];

/**
 * Helper function to check if a string value is numeric.
 *
 * @param value The string to check
 * @returns True if the string can be parsed as a valid number
 */
export function isNumeric(value: string): boolean {
  return !isNaN(parseFloat(value)) && isFinite(Number(value));
}

/**
 * A modern parser implementation that provides the necessary functionality
 * for auto-completion and syntax highlighting in the Monaco editor.
 */
export class Parser {
  public root: any = null;
  public errorText: string = '';
  public errno: number = 0;
  public state: State = State.INITIAL;
  public key: string = '';
  public value: string = '';
  private tokens: Array<Token> = [];

  // Parse method to analyze the query and determine the parser state
  parse(text: string, raiseError: boolean = false, ignoreLastChar: boolean = false): any {
    if (!text || text.trim() === '') {
      this.state = State.INITIAL;
      return null;
    }

    try {
      // Analyze the text to determine the parser state and extract key/value
      this.analyzeForState(text, ignoreLastChar);

      // Use the implementation to actually parse and validate
      const result = tokenize(text);
      this.tokens = result.tokens; // Store tokens for monaco highlighting

      if (result.errors.length > 0) {
        const error = result.errors[0];
        this.errorText = error.message;
        this.errno = 1;

        if (raiseError) {
          throw new Error(this.errorText);
        }

        return null;
      }

      // Parser tokens into AST
      const parser = new QueryParser(this.tokens);
      const { ast, errors: parseErrors } = parser.parse();

      if (parseErrors.length > 0 || !ast) {
        if (parseErrors.length > 0) {
          const error = parseErrors[0];
          this.errorText = error.message;
          this.errno = 1;
        } else {
          this.errorText = 'Invalid query structure';
          this.errno = 1;
        }

        if (raiseError) {
          throw new Error(this.errorText);
        }

        return null;
      }

      // Store the parsed AST as root for compatibility
      this.root = ast;
      return ast;
    } catch (error: any) {
      this.errorText = error.message || 'Parse error';
      this.errno = 1;

      if (raiseError) {
        throw error;
      }

      return null;
    }
  }

  // Helper method to analyze the text and determine the state for editor completion
  private analyzeForState(text: string, ignoreLastChar: boolean = false): void {
    if (!text) {
      this.state = State.INITIAL;
      return;
    }

    const processedText = ignoreLastChar ? text.slice(0, -1) : text;

    // Simplified state analysis for completion purposes
    // This could be improved with a more sophisticated approach
    const trimmed = processedText.trim();

    if (trimmed === '') {
      this.state = State.INITIAL;
      return;
    }

    // Check if we're in a key position (at the start or after AND/OR)
    if (/and\s*$|or\s*$|\(\s*$|^\s*$/.test(trimmed)) {
      this.state = State.KEY;
      return;
    }

    // Check if we have a key and are expecting an operator
    const keyMatch = trimmed.match(/(\w+)$/);
    if (keyMatch) {
      // If the key is standalone (not part of an expression), we're in KEY state
      const beforeKey = trimmed.slice(0, trimmed.length - keyMatch[0].length).trim();
      if (beforeKey === '' || beforeKey.endsWith('and') || beforeKey.endsWith('or') || beforeKey.endsWith('(')) {
        this.state = State.KEY;
        this.key = keyMatch[1];
        return;
      }
    }

    // Check if we have a key-operator and are expecting a value
    const operatorMatch = trimmed.match(/(\w+)\s*([=!<>~]+|!=|!~|>=|<=)$/);
    if (operatorMatch) {
      this.state = State.KEY_VALUE_OPERATOR;
      this.key = operatorMatch[1];
      return;
    }

    // Check if we have a complete expression and are expecting a boolean operator
    const expressionMatch = trimmed.match(/(\w+)\s*([=!<>~]+|!=|!~|>=|<=)\s*["']?([^"']*)["']?$/);
    if (expressionMatch) {
      this.state = State.EXPECT_BOOL_OP;
      this.key = expressionMatch[1];
      this.value = expressionMatch[3];
      return;
    }

    // Default state if nothing else matches
    this.state = State.BOOL_OP_DELIMITER;
  }

  // Method to generate Monaco editor tokens for syntax highlighting
  generateMonacoTokens(): number[] {
    if (!this.tokens || this.tokens.length === 0) {
      return [];
    }

    // Monaco expects semantic tokens in the format:
    // [deltaLine, deltaColumn, length, tokenTypeIndex, tokenModifierIndex]
    // where delta is relative to the previous token
    const result: number[] = [];
    let prevLine = 0;
    let prevCol = 0;

    for (const token of this.tokens) {
      // Find the token type index
      const tokenTypeIndex = tokenTypes.indexOf(token.type);
      if (tokenTypeIndex === -1) continue; // Skip unknown token types

      // Calculate delta line and column
      const deltaLine = token.position.line - 1 - prevLine;

      // If we're on the same line, calculate delta from previous token
      // Otherwise, it's the absolute column position
      const deltaColumn = deltaLine === 0
        ? token.position.column - 1 - prevCol
        : token.position.column - 1;

      // Token length = value's length
      const length = token.value.length;

      // We don't use token modifiers, so it's 0
      const tokenModifierIndex = 0;

      // Add token information to the result
      result.push(
        deltaLine,       // deltaLine
        deltaColumn,     // deltaColumn
        length,          // length
        tokenTypeIndex,  // tokenTypeIndex
        tokenModifierIndex // tokenModifierIndex
      );

      // Update previous position
      prevLine = token.position.line - 1;
      prevCol = token.position.column - 1 + length;
    }

    return result;
  }
}

// Simple placeholder for compatibility
export const Expression = {};

/**
 * Parse a LogchefQL query string and convert it to SQL.
 *
 * @param query The LogchefQL query string to parse
 * @param options Configuration options
 * @returns Object containing SQL, parameters (if parameterized), and any errors
 */
export function parseToSQL(
  query: string,
  options?: { parameterized?: boolean }
): { sql: string; params: unknown[]; errors: ParseError[] } {
  // Handle empty queries
  if (!query || query.trim() === '') {
    return { sql: '', params: [], errors: [] };
  }

  // Tokenize the query
  const { tokens, errors: tokenizeErrors } = tokenize(query);
  if (tokenizeErrors.length > 0) {
    return { sql: '', params: [], errors: tokenizeErrors };
  }

  // Parse tokens into AST
  const parser = new QueryParser(tokens);
  const { ast, errors: parseErrors } = parser.parse();
  if (parseErrors.length > 0 || !ast) {
    return { sql: '', params: [], errors: parseErrors };
  }

  // Generate SQL from AST
  const visitor = new SQLVisitor(options?.parameterized);
  const { sql, params } = visitor.generate(ast);

  return { sql, params, errors: [] };
}

/**
 * Validate a LogchefQL query string.
 *
 * @param query The LogchefQL query string to validate
 * @returns Whether the query is valid
 */
export function validateQuery(query: string): boolean {
  // Empty queries are considered valid
  if (!query || query.trim() === '') {
    return true;
  }

  const { errors } = parseToSQL(query);
  return errors.length === 0;
}

/**
 * Enhanced validation with detailed error information.
 *
 * @param query The LogchefQL query string to validate
 * @returns Object with validation result and error details
 */
export function validateQueryWithDetails(query: string): {
  valid: boolean;
  error?: string;
  errorPosition?: { line: number; column: number };
} {
  // Empty queries are considered valid
  if (!query || query.trim() === '') {
    return { valid: true };
  }

  const { errors } = parseToSQL(query);

  if (errors.length === 0) {
    return { valid: true };
  }

  const firstError = errors[0];
  return {
    valid: false,
    error: firstError.message,
    errorPosition: firstError.position
  };
}

/**
 * Validates a LogchefQL query string and returns a boolean result.
 * Provided for compatibility with existing code.
 *
 * @param query The query to validate
 * @returns Whether the query is valid
 */
export function validateLogchefQL(query: string): boolean {
  return validateQueryWithDetails(query).valid;
}

/**
 * Parse a LogchefQL query and return information about fields used in the query.
 *
 * @param query The LogchefQL query string to parse
 * @returns Object containing fields used and operation types
 */
export function extractQueryMetadata(query: string): {
  success: boolean;
  fieldsUsed: string[];
  operations: ('filter')[];
  error?: string;
} {
  // Handle empty queries
  if (!query || query.trim() === '') {
    return { success: true, fieldsUsed: [], operations: [] };
  }

  const { tokens, errors: tokenizeErrors } = tokenize(query);
  if (tokenizeErrors.length > 0) {
    return {
      success: false,
      fieldsUsed: [],
      operations: [],
      error: tokenizeErrors[0].message
    };
  }

  // Extract field names from key tokens
  const fieldsUsed = tokens
    .filter(token => token.type === 'key')
    .map(token => token.value)
    .filter((field, index, self) => self.indexOf(field) === index); // Deduplicate

  return {
    success: true,
    fieldsUsed,
    operations: fieldsUsed.length > 0 ? ['filter'] : []
  };
}
