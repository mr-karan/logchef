import { getLocalTimeZone } from "@internationalized/date";
import type { DateValue } from "@internationalized/date";

/**
 * Formats a DateValue for SQL display with proper quotes
 */
function formatSqlDate(date: DateValue | null | undefined): string {
  if (!date) return "now()";

  try {
    // Convert to JS Date with proper timezone handling
    const jsDate = date.toDate(getLocalTimeZone());

    // Format as YYYY-MM-DD HH:MM:SS
    const formatted = jsDate.toISOString()
      .replace('T', ' ')      // Replace T with space
      .replace(/\.\d+Z$/, ''); // Remove milliseconds and Z

    return `'${formatted}'`;
  } catch (error) {
    console.error("Error formatting date for SQL:", error);
    return "now()";
  }
}

/**
 * Generates a default SQL query based on current application state
 */
export function generateDefaultSqlQuery(params: {
  tableName: string;
  timestampField: string;
  timeRange: { start: DateValue; end: DateValue } | null;
  limit?: number;
}): string {
  const { tableName, timestampField, timeRange, limit = 100 } = params;

  // Use backticks for table/field names to handle special characters, unless it contains a dot
  const tableRef = tableName
    ? (tableName.includes('.') ? tableName : `\`${tableName}\``)
    : "logs"; // Default if no table name provided
  const tsField = `\`${timestampField || "timestamp"}\``;

  // Format time range with proper SQL functions
  let timeFilter = "";
  if (timeRange?.start && timeRange?.end) {
    const startFormatted = formatSqlDate(timeRange.start);
    const endFormatted = formatSqlDate(timeRange.end);
    timeFilter = `WHERE ${tsField} BETWEEN toDateTime(${startFormatted}) AND toDateTime(${endFormatted})`;
  } else {
    // Fallback to relative time if no specific range
    timeFilter = `WHERE ${tsField} > now() - INTERVAL 1 HOUR`;
  }

  // Build the complete query
  return `SELECT *
FROM ${tableRef}
${timeFilter}
LIMIT ${limit}`;
}

/**
 * Generates a default LogchefQL query based on current application state
 */
export function generateDefaultLogchefqlQuery(params: {
  timeRange: { start: DateValue; end: DateValue } | null;
  commonField?: string;
  commonValue?: string;
}): string {
  const { commonField, commonValue, timeRange } = params;

  // For LogchefQL, we usually just return an empty string or very simple filter
  if (commonField && commonValue) {
    return `${commonField}="${commonValue}"`;
  }

  // Default to empty query - time range is handled separately in the UI
  return "";
}
