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

import { SQLParser } from "./ast";

/**
 * Gets a default SQL query for a table
 * @param tableName The table name
 * @param tsField The timestamp field
 * @param startTimestamp Start timestamp in seconds (unix epoch)
 * @param endTimestamp End timestamp in seconds (unix epoch)
 * @param limit Number of results to return
 * @returns A default SQL query
 */
export function getDefaultSQLQuery(
  tableName: string,
  tsField: string = "timestamp",
  startTimestamp?: number,
  endTimestamp?: number,
  limit: number = 1000
): string {
  // Use provided timestamps or default to the last hour
  const oneHourAgo = new Date();
  oneHourAgo.setHours(oneHourAgo.getHours() - 1);

  const now = new Date();

  // Format dates for ClickHouse
  const startTimeFormatted =
    startTimestamp !== undefined
      ? startTimestamp
      : Math.floor(oneHourAgo.getTime() / 1000);

  const endTimeFormatted =
    endTimestamp !== undefined
      ? endTimestamp
      : Math.floor(now.getTime() / 1000);

  // Format dates as ISO strings for direct use in query
  const startTimeIso = new Date(startTimeFormatted * 1000).toISOString();
  const endTimeIso = new Date(endTimeFormatted * 1000).toISOString();

  // Create a query using our AST builder
  const query = {
    type: "query",
    selectClause: {
      type: "select",
      columns: ["*"],
    },
    fromClause: {
      type: "from",
      table: tableName,
    },
    whereClause: {
      type: "where",
      conditions: `${tsField} BETWEEN '${startTimeIso}' AND '${endTimeIso}'`,
      hasTimestampFilter: true,
    },
    orderByClause: {
      type: "orderby",
      columns: [
        {
          type: "orderby_column",
          column: tsField,
          direction: "DESC",
        },
      ],
    },
    limitClause: {
      type: "limit",
      value: limit,
    },
  };

  return SQLParser.toSQL(query as any);
}

/**
 * Formats an SQL query for display
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
