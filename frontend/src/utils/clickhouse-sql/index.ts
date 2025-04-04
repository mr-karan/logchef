// Export key elements from language.ts
export { CharType, tokenTypes, SQL_KEYWORDS, CLICKHOUSE_FUNCTIONS, SQL_TYPES, isNumeric } from './language';

/**
 * Validates a ClickHouse SQL query with basic syntax checking
 * @param query The SQL query to validate
 * @returns Whether the query appears to have valid syntax
 */
export function validateSQL(query: string): boolean {
  // Empty query is not valid for direct execution
  if (!query || !query.trim()) {
    return false;
  }

  try {
    // Basic validation - make sure query has SELECT and FROM
    const hasSelect = /\bSELECT\b/i.test(query);
    const hasFrom = /\bFROM\b/i.test(query);

    // Check for unbalanced parentheses
    const openParens = (query.match(/\(/g) || []).length;
    const closeParens = (query.match(/\)/g) || []).length;
    if (openParens !== closeParens) {
      return false;
    }

    // Check for unbalanced quotes (single or double)
    const singleQuotes = (query.match(/'/g) || []).length;
    const doubleQuotes = (query.match(/"/g) || []).length;
    if (singleQuotes % 2 !== 0 || doubleQuotes % 2 !== 0) {
      return false;
    }

    // Must have both SELECT and FROM clauses
    return hasSelect && hasFrom;
  } catch (error) {
    console.error("Error during SQL validation:", error);
    return false;
  }
}
