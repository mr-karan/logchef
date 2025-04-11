import { format } from 'date-fns';
import { getLocalTimeZone } from '@internationalized/date';
import type { DateValue, CalendarDateTime } from '@internationalized/date';
import type { TimeRange } from '@/types/query';

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
 * @returns SQL condition string for the WHERE clause
 */
export function createTimeRangeCondition(tsField: string, timeRange: TimeRange): string {
  if (!timeRange.start || !timeRange.end) {
    throw new Error('Invalid time range: start and end dates are required');
  }

  // Ensure field name has backticks if needed
  const formattedField = tsField.includes('`') ? tsField : `\`${tsField}\``;

  // Format the dates with toDateTime function
  const startFormatted = formatDateForSQL(timeRange.start);
  const endFormatted = formatDateForSQL(timeRange.end);

  return `${formattedField} BETWEEN toDateTime(${startFormatted}) AND toDateTime(${endFormatted})`;
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