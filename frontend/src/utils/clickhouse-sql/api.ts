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

/**
 * Extracts table name from SQL query
 * @param query The SQL query string
 * @returns Table name or null if not found
 */
export function extractTableName(query: string): string | null {
  if (!query || !query.trim()) {
    return null;
  }

  try {
    // Simple regex to extract table name from FROM clause
    const fromMatch = query.match(/FROM\s+([a-zA-Z0-9_.]+)(?:\s+|$)/i);
    if (fromMatch && fromMatch[1]) {
      return fromMatch[1].trim();
    }
    return null;
  } catch (error) {
    return null;
  }
}

/**
 * Formats an SQL query for better readability
 * @param query The SQL query to format
 * @returns Formatted SQL query
 */
export function formatSQLQuery(query: string): string {
  if (!query) return "";

  // Replace multiple spaces with a single space
  let formatted = query.replace(/\s+/g, " ");

  // Add newlines after common SQL keywords
  const keywords = [
    "SELECT",
    "FROM",
    "WHERE",
    "GROUP BY",
    "HAVING",
    "ORDER BY",
    "LIMIT",
  ];

  for (const keyword of keywords) {
    const regex = new RegExp(`\\b${keyword}\\b`, "gi");
    formatted = formatted.replace(regex, `\n${keyword}`);
  }

  // Add newlines and indentation for AND and OR
  formatted = formatted.replace(/\b(AND|OR)\b/gi, "\n  $1");

  // Trim extra whitespace
  return formatted.trim();
}
