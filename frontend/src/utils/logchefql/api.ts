import { parseToSQL, validateQuery, validateQueryWithDetails, extractQueryMetadata } from './index';
import { tokenize } from './tokenizer';
import type { Token, Operator } from './types';

// New interface to represent a query condition with strong typing
export interface QueryCondition {
  field: string;      // Column/field name
  operator: string;   // The operator used (=, !=, ~, !~, etc.)
  value: string;      // The value being compared against
  isRegex: boolean;   // Whether this is a regex operation
}

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

// Helper function to extract conditions from tokens
function extractConditionsFromTokens(tokens: Token[]): QueryCondition[] {
  const conditions: QueryCondition[] = [];
  let i = 0;

  while (i < tokens.length) {
    // Look for the pattern: key -> operator -> value
    if (tokens[i]?.type === 'key' &&
        i + 2 < tokens.length &&
        tokens[i+1]?.type === 'operator' &&
        (tokens[i+2]?.type === 'value' || tokens[i+2]?.type === 'key')) {

      const field = tokens[i].value;
      const operator = tokens[i+1].value;
      const value = tokens[i+2].value;

      // Check if it's a regex operation
      const isRegex = operator === '~' || operator === '!~';

      conditions.push({
        field,
        operator,
        value,
        isRegex
      });

      // Skip the processed tokens
      i += 3;
    } else {
      // Skip tokens that don't form a complete condition
      i++;
    }
  }

  return conditions;
}

/**
 * Parses a LogchefQL query and returns the translated SQL conditions.
 * Includes error handling and structured query metadata.
 * @param query The LogchefQL query string.
 * @returns Object containing success status, SQL conditions, or an error message.
 */
export function parseAndTranslateLogchefQL(query: string): {
  success: boolean;
  sql?: string;
  error?: string;
  meta?: {
    fieldsUsed: string[];
    conditions: QueryCondition[];  // New: Add structured conditions
    operations?: ('filter' | 'sort' | 'limit')[];
  };
} {
  // Handle empty query explicitly
  if (!query || query.trim() === '') {
    return { success: true, sql: '', meta: { fieldsUsed: [], conditions: [] } };
  }

  const { sql, errors } = parseToSQL(query);

  if (errors.length > 0) {
    return {
      success: false,
      error: errors[0].message
    };
  }

  // Extract field names and detailed conditions from tokens
  const { tokens } = tokenize(query);

  // Extract fields used (with deduplication)
  const fieldsUsed = tokens
    .filter(token => token.type === 'key')
    .map(token => token.value)
    .filter((field, index, self) => self.indexOf(field) === index);

  // Extract structured condition information
  const conditions = extractConditionsFromTokens(tokens);

  return {
    success: true,
    sql,
    meta: {
      fieldsUsed,
      conditions,
      operations: sql ? ['filter'] : []
    }
  };
}

/**
 * Detects missing boolean operators between conditions in LogchefQL query.
 * Specifically checks for patterns like: key=value key=value (without and/or between them)
 *
 * @param query The LogchefQL query string to validate
 * @returns Error message if missing operators found, null otherwise
 */
function detectMissingBooleanOperators(query: string): string | null {
  if (!query || query.trim() === '') {
    return null;
  }

  const { tokens, errors } = tokenize(query);
  if (errors.length > 0) {
    return null; // Other errors will be caught by the main validator
  }

  for (let i = 0; i < tokens.length - 3; i++) {
    // Look for pattern: key -> operator -> value -> key
    // This indicates a missing boolean operator between conditions
    if (
      tokens[i]?.type === 'key' &&
      tokens[i+1]?.type === 'operator' &&
      (tokens[i+2]?.type === 'value' || tokens[i+2]?.type === 'key') &&
      tokens[i+3]?.type === 'key'
    ) {
      // Build a more helpful error message
      const firstKey = tokens[i].value;
      const secondKey = tokens[i+3].value;
      return `Missing boolean operator (and/or) between conditions: '${firstKey}' and '${secondKey}'`;
    }
  }

  return null;
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
  // First check for missing boolean operators
  const missingOperatorError = detectMissingBooleanOperators(query);
  if (missingOperatorError) {
    return {
      valid: false,
      error: missingOperatorError,
      // Position set to the last token for display purposes
      errorPosition: { line: 1, column: query.length }
    };
  }

  // Then do the standard validation
  return validateQueryWithDetails(query);
}

/**
 * Validate a LogchefQL query to check if it's syntactically correct.
 * This validation includes checks for:
 * 1. Missing boolean operators between conditions (e.g., "field1=value field2=value")
 * 2. Valid syntax (parentheses, operators, etc.)
 * 3. Well-formed expressions
 *
 * @param query The LogchefQL query string.
 * @returns Whether the query is valid.
 */
export function validateLogchefQL(query: string): boolean {
  return validateLogchefQLWithDetails(query).valid;
}
