import { parseToSQL, validateQuery, validateQueryWithDetails } from './index';
import { tokenize } from './tokenizer';

/**
 * Translates a LogchefQL query string to SQL WHERE clause conditions.
 * @param query The LogchefQL query string.
 * @returns SQL WHERE clause conditions string (without the "WHERE" keyword).
 */
export function translateToSQLConditions(query: string): string {
  if (!query || query.trim() === '') {
    return '';
  }

  const { sql, errors } = parseToSQL(query);

  if (errors.length > 0) {
    console.warn('Error parsing LogchefQL query:', errors[0].message);
    return '';
  }

  return sql;
}

/**
 * Validates a LogchefQL query string.
 * @param query The LogchefQL query string.
 * @returns True if the query is syntactically valid, false otherwise.
 */
export function validateLogchefQL(query: string): boolean {
  return validateQuery(query);
}

/**
 * Enhanced validation with detailed error information
 * @param query The LogchefQL query string
 * @returns Object with validation result and error details
 */
export function validateLogchefQLWithDetails(query: string): {
  valid: boolean;
  error?: string;
  errorPosition?: { line: number; column: number };
} {
  return validateQueryWithDetails(query);
}

/**
 * Parses a LogchefQL query and returns the translated SQL conditions.
 * Includes error handling.
 * @param query The LogchefQL query string.
 * @returns Object containing success status, SQL conditions, or an error message.
 */
export function parseAndTranslateLogchefQL(query: string): {
  success: boolean;
  sql?: string;
  error?: string;
  meta?: {
    fieldsUsed: string[];
    operations?: ('filter' | 'sort' | 'limit')[];
  };
} {
  // Handle empty query explicitly
  if (!query || query.trim() === '') {
    return { success: true, sql: '' };
  }

  const { sql, errors } = parseToSQL(query);

  if (errors.length > 0) {
    return {
      success: false,
      error: errors[0].message
    };
  }

  // Extract field names from key tokens by tokenizing directly
  const { tokens } = tokenize(query);

  const fieldsUsed = tokens
    .filter(token => token.type === 'key')
    .map(token => token.value)
    .filter((field, index, self) => self.indexOf(field) === index); // Deduplicate

  return {
    success: true,
    sql,
    meta: {
      fieldsUsed,
      operations: sql ? ['filter'] : []
    }
  };
}
