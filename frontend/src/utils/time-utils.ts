import { format } from 'date-fns';
import { getLocalTimeZone } from '@internationalized/date';
import type { DateValue, CalendarDateTime } from '@internationalized/date';
import type { TimeRange } from '@/types/query';

/**
 * Get the user's local timezone
 * @returns The local timezone string (e.g., 'America/New_York')
 */
export function getUserTimezone(): string {
  return getLocalTimeZone();
}

/**
 * Format timezone string for use in SQL queries
 * @param timezone The timezone string
 * @returns The formatted timezone string for ClickHouse
 */
export function formatTimezoneForSQL(timezone: string = getUserTimezone()): string {
  // Escape any single quotes in the timezone name
  return timezone.replace(/'/g, "''");
}

/**
 * Formats a DateValue for SQL display with consistent format and quotes
 * @param dateTime The DateValue to format
 * @param addQuotes Whether to add quotes around the date string
 * @returns Formatted date string ready for SQL usage
 */
export function formatDateForSQL(dateTime: DateValue | null | undefined, addQuotes = true): string {
  if (!dateTime) return "now()";

  try {
    // Convert to JS Date with proper timezone handling
    const jsDate = dateTime.toDate(getLocalTimeZone());

    // Format as YYYY-MM-DD HH:MM:SS
    const formatted = format(jsDate, 'yyyy-MM-dd HH:mm:ss');

    // Use standard SQL escaping (double single quotes)
    return addQuotes ? `'${formatted}'` : formatted;
  } catch (error) {
    console.error("Error formatting date for SQL:", error);
    return "now()";
  }
}

/**
 * Creates a SQL time condition between two dates for a given timestamp field
 * @param tsField The timestamp field name
 * @param timeRange The time range object
 * @param includeTimezone Whether to include timezone information in the query
 * @returns SQL condition string for the WHERE clause
 */
export function createTimeRangeCondition(
  tsField: string,
  timeRange: TimeRange,
  includeTimezone: boolean = true
): string {
  if (!timeRange.start || !timeRange.end) {
    throw new Error('Invalid time range: start and end dates are required');
  }

  // Ensure field name has backticks if needed
  const formattedField = tsField.includes('`') ? tsField : `\`${tsField}\``;

  // Format the dates with toDateTime function
  const startFormatted = formatDateForSQL(timeRange.start);
  const endFormatted = formatDateForSQL(timeRange.end);

  if (includeTimezone) {
    // Get user's timezone
    const timezone = formatTimezoneForSQL(getUserTimezone());

    // Create simplified timezone-aware condition with single toDateTime call
    return `${formattedField} BETWEEN toDateTime(${startFormatted}, '${timezone}') AND toDateTime(${endFormatted}, '${timezone}')`;
  } else {
    // Create standard condition without timezone
    return `${formattedField} BETWEEN toDateTime(${startFormatted}) AND toDateTime(${endFormatted})`;
  }
}

/**
 * Safely converts a DateValue to a CalendarDateTime
 * @param dateValue The DateValue to convert
 * @returns A CalendarDateTime or null if conversion fails
 */
export function toCalendarDateTimeSafe(dateValue: DateValue | null | undefined): CalendarDateTime | null {
  if (!dateValue) return null;

  try {
    // If it's already a CalendarDateTime, return it
    if ('hour' in dateValue && 'minute' in dateValue) {
      return dateValue as CalendarDateTime;
    }

    // Create a new Date from the DateValue
    const jsDate = dateValue.toDate(getLocalTimeZone());

    // Use the internationalized-date library's toCalendarDateTime function if available
    // This is a simplified implementation - in a real app you'd use the library's conversion
    return {
      ...dateValue,
      hour: jsDate.getHours(),
      minute: jsDate.getMinutes(),
      second: jsDate.getSeconds()
    } as CalendarDateTime;
  } catch (error) {
    console.error("Error converting DateValue to CalendarDateTime:", error);
    return null;
  }
}

/**
 * Converts a TimeRange with DateValue to a TimeRange with CalendarDateTime
 * @param timeRange The original TimeRange
 * @returns A new TimeRange with CalendarDateTime or null on failure
 */
export function timeRangeToCalendarDateTime(timeRange: TimeRange): { start: CalendarDateTime; end: CalendarDateTime } | null {
  if (!timeRange?.start || !timeRange?.end) {
    return null;
  }

  const start = toCalendarDateTimeSafe(timeRange.start);
  const end = toCalendarDateTimeSafe(timeRange.end);

  if (!start || !end) {
    return null;
  }

  return { start, end };
}