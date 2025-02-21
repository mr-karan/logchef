import { format } from "date-fns";
import type { Source } from "@/api/sources";

/**
 * Format a date string using date-fns
 * @param dateString - The date string to format
 * @param formatStr - The format string to use (defaults to 'PPp' - "Feb 17, 2025, 3:17 PM")
 * @returns Formatted date string or 'Never' if date is invalid/undefined
 */
export function formatDate(
  dateString: string | undefined,
  formatStr = "PPp"
): string {
  if (!dateString) return "Never";
  try {
    const date = new Date(dateString);
    return format(date, formatStr);
  } catch (error) {
    console.error("Error formatting date:", error);
    return "Invalid date";
  }
}

/**
 * Format a source's display name as "database.table_name"
 * @param source - The source object containing connection info
 * @returns Formatted source name string
 */
export function formatSourceName(source: Source): string {
  return `${source.connection.database}.${source.connection.table_name}`;
}

/**
 * Format a source's display name with optional schema type
 * @param source - The source object containing connection info
 * @param includeSchema - Whether to include the schema type in parentheses
 * @returns Formatted source name string with optional schema type
 */
export function formatSourceNameWithSchema(
  source: Source,
  includeSchema = true
): string {
  const baseName = formatSourceName(source);
  return includeSchema && source.connection.table_name ? `${baseName} (${source.connection.table_name})` : baseName;
}
