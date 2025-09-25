import type { ParseError } from './types';

export interface UserFriendlyError {
  code: string;
  message: string;
  suggestion?: string;
  position?: { line: number; column: number };
  examples?: string[];
  severity?: 'error' | 'warning' | 'info';
}

export class LogChefQLErrorHandler {
  static getUserFriendlyError(error: ParseError, context?: string): UserFriendlyError {
    const errorMap: Record<string, UserFriendlyError> = {
      'UNTERMINATED_STRING': {
        code: 'UNTERMINATED_STRING',
        message: 'Unterminated string literal',
        suggestion: 'Close the string with a matching quote character',
        examples: ['field = "value"', "field = 'value'"],
        severity: 'error'
      },
      'UNEXPECTED_END': {
        code: 'UNEXPECTED_END',
        message: 'Unexpected end of input',
        suggestion: 'Complete your query - it appears to be cut off',
        examples: ['field = "value"', 'field > 100 and status = "active"'],
        severity: 'error'
      },
      'UNEXPECTED_TOKEN': {
        code: 'UNEXPECTED_TOKEN',
        message: context ? `Unexpected token: "${context}"` : 'Unexpected token',
        suggestion: 'Check for typos and valid LogChefQL syntax',
        examples: ['field = "value"', 'field.nested = 123', '(field = "a") and (field = "b")'],
        severity: 'error'
      },
      'EXPECTED_OPERATOR': {
        code: 'EXPECTED_OPERATOR',
        message: 'Expected an operator after field name',
        suggestion: 'Add an operator after the field name',
        examples: ['field = "value"', 'count > 100', 'name ~ "pattern"'],
        severity: 'error'
      },
      'EXPECTED_VALUE': {
        code: 'EXPECTED_VALUE',
        message: 'Expected a value after operator',
        suggestion: 'Add a value after the operator',
        examples: ['field = "value"', 'count > 100', 'flag = true'],
        severity: 'error'
      },
      'EXPECTED_CLOSING_PAREN': {
        code: 'EXPECTED_CLOSING_PAREN',
        message: 'Expected closing parenthesis',
        suggestion: 'Add a closing parenthesis ) to match the opening one',
        examples: ['(field = "value")', '(count > 10) and (status = "active")'],
        severity: 'error'
      },
      'UNKNOWN_OPERATOR': {
        code: 'UNKNOWN_OPERATOR',
        message: context ? `Unknown operator: "${context}"` : 'Unknown operator',
        suggestion: 'Use one of the supported operators',
        examples: ['field = "exact"', 'field != "not"', 'field ~ "regex"', 'count > 100', 'count >= 50'],
        severity: 'error'
      },
      'UNKNOWN_BOOLEAN_OPERATOR': {
        code: 'UNKNOWN_BOOLEAN_OPERATOR',
        message: context ? `Unknown boolean operator: "${context}"` : 'Unknown boolean operator',
        suggestion: 'Use "and" or "or" to combine conditions (case insensitive)',
        examples: ['field = "a" and field = "b"', 'count > 10 or status = "active"'],
        severity: 'error'
      },
      'INVALID_TOKEN_TYPE': {
        code: 'INVALID_TOKEN_TYPE',
        message: context ? `Invalid token type: "${context}"` : 'Invalid token type',
        suggestion: 'Check your query syntax',
        examples: ['field = "value"', 'field.nested.key = 123'],
        severity: 'error'
      },
      'PARSE_ERROR': {
        code: 'PARSE_ERROR',
        message: error.message || 'Syntax error in query',
        suggestion: 'Check your query syntax and structure',
        examples: ['field = "value"', '(field1 = "a" or field2 = "b") and field3 > 10'],
        severity: 'error'
      }
    };

    const baseError = errorMap[error.code] || {
      code: 'UNKNOWN_ERROR',
      message: error.message,
      suggestion: 'Check your query syntax for errors',
      severity: 'error' as const
    };

    return {
      ...baseError,
      position: error.position
    };
  }

  static createErrorFromMessage(message: string, position?: { line: number; column: number }): UserFriendlyError {
    return {
      code: 'CUSTOM_ERROR',
      message,
      position,
      suggestion: 'Review your query and try again',
      severity: 'error'
    };
  }

  /**
   * Format a user-friendly error message with position information and examples
   */
  static formatErrorMessage(error: UserFriendlyError, originalQuery?: string): string {
    let formatted = error.message;

    // Add position information if available
    if (error.position) {
      formatted += ` at line ${error.position.line}, column ${error.position.column}`;
    }

    // Add context from original query if available
    if (originalQuery && error.position) {
      const lines = originalQuery.split('\n');
      const errorLine = lines[error.position.line - 1];
      if (errorLine) {
        formatted += `\n\nIn: ${errorLine}`;
        if (error.position.column > 1) {
          const pointer = ' '.repeat(4 + error.position.column - 1) + '^';
          formatted += `\n${pointer}`;
        }
      }
    }

    // Add suggestion
    if (error.suggestion) {
      formatted += `\n\nSuggestion: ${error.suggestion}`;
    }

    // Add examples if available
    if (error.examples && error.examples.length > 0) {
      formatted += `\n\nExamples:`;
      error.examples.forEach(example => {
        formatted += `\n  ${example}`;
      });
    }

    return formatted;
  }

  static createParseError(code: string, message?: string, position?: { line: number; column: number }): ParseError {
    return {
      code,
      message: message || code,
      position
    };
  }
}

// Helper functions to create specific errors with position info
export function createUnexpectedEndError(position?: { line: number; column: number }): ParseError {
  return LogChefQLErrorHandler.createParseError('UNEXPECTED_END', 'Unexpected end of input', position);
}

export function createUnexpectedTokenError(tokenValue: string, position?: { line: number; column: number }): ParseError {
  return LogChefQLErrorHandler.createParseError('UNEXPECTED_TOKEN', `Unexpected token: ${tokenValue}`, position);
}

export function createExpectedOperatorError(position?: { line: number; column: number }): ParseError {
  return LogChefQLErrorHandler.createParseError('EXPECTED_OPERATOR', 'Expected an operator after field name', position);
}

export function createExpectedValueError(position?: { line: number; column: number }): ParseError {
  return LogChefQLErrorHandler.createParseError('EXPECTED_VALUE', 'Expected a value after operator', position);
}

export function createExpectedClosingParenError(position?: { line: number; column: number }): ParseError {
  return LogChefQLErrorHandler.createParseError('EXPECTED_CLOSING_PAREN', 'Expected closing parenthesis', position);
}

export function createUnknownOperatorError(operator: string, position?: { line: number; column: number }): ParseError {
  return LogChefQLErrorHandler.createParseError('UNKNOWN_OPERATOR', `Unknown operator: ${operator}`, position);
}

export function createUnknownBooleanOperatorError(operator: string, position?: { line: number; column: number }): ParseError {
  return LogChefQLErrorHandler.createParseError('UNKNOWN_BOOLEAN_OPERATOR', `Unknown boolean operator: ${operator}`, position);
}

export function createInvalidTokenTypeError(tokenType: string, position?: { line: number; column: number }): ParseError {
  return LogChefQLErrorHandler.createParseError('INVALID_TOKEN_TYPE', `Unexpected token type: ${tokenType}`, position);
}