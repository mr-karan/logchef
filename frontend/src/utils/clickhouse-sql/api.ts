/**
 * Validates a ClickHouse SQL query
 * @param query The query string to validate
 * @returns True if valid, false otherwise
 */
export function validateSQL(query: string): boolean {
  if (!query || !query.trim()) {
    return false;
  }

  try {
    // Basic validation - make sure query has SELECT and FROM
    const hasSelect = /\bSELECT\b/i.test(query);
    const hasFrom = /\bFROM\b/i.test(query);

    return hasSelect && hasFrom;
  } catch (error) {
    return false;
  }
}
