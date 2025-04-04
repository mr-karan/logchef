import type { Updater } from "@tanstack/vue-table";
import type { Ref, VNode } from "vue";
import { h } from 'vue';
import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function valueUpdater<T extends Updater<any>>(
  updaterOrValue: T,
  ref: Ref
) {
  ref.value =
    typeof updaterOrValue === "function"
      ? updaterOrValue(ref.value)
      : updaterOrValue;
  return ref.value;
}

export function formatTimestamp(value: string, timezone: 'local' | 'utc' = 'local'): string {
    try {
        const date = new Date(value);

        // Check if the date is valid
        if (isNaN(date.getTime())) {
            return value; // Return original value if invalid
        }

        // Format based on timezone preference
        if (timezone === 'utc') {
            // Use UTC formatting - keep the 'Z' to indicate UTC
            return date.toISOString();
        } else {
            // Use local timezone with ISO format and timezone offset
            const tzOffset = date.getTimezoneOffset();
            const absOffset = Math.abs(tzOffset);
            const offsetHours = Math.floor(absOffset / 60).toString().padStart(2, '0');
            const offsetMinutes = (absOffset % 60).toString().padStart(2, '0');
            const offsetSign = tzOffset <= 0 ? '+' : '-'; // Note: getTimezoneOffset returns negative for positive offsets

            // Format the date in ISO format with timezone offset
            const localISOTime = new Date(date.getTime() - (tzOffset * 60000))
                .toISOString()
                .slice(0, -1); // Remove the trailing Z

            return `${localISOTime}${offsetSign}${offsetHours}:${offsetMinutes}`;
        }
    } catch (e) {
        return value; // Return original value if there's an error
    }
}

// Common badge styles with subtle ring border
const BADGE_BASE_STYLES =
  "inline-flex items-center rounded-full px-2 py-[2px] text-[11px] font-semibold tracking-wide uppercase leading-none ring-1 ring-inset ring-opacity-10";

// Severity level color mapping with improved contrast and consistent saturation
const severityColorMap = new Map([
  // Debug - Neutral Gray (lighter than Trace)
  [
    "debug",
    `${BADGE_BASE_STYLES} text-gray-800 bg-gray-300 ring-gray-400 dark:text-gray-200 dark:bg-gray-700 dark:ring-gray-600`,
  ],
  // Info - Balanced blue at 500 level
  [
    "info",
    `${BADGE_BASE_STYLES} text-white bg-blue-500 ring-blue-600 dark:text-blue-50 dark:bg-blue-700 dark:ring-blue-500`,
  ],
  [
    "information",
    `${BADGE_BASE_STYLES} text-white bg-blue-500 ring-blue-600 dark:text-blue-50 dark:bg-blue-700 dark:ring-blue-500`,
  ],
  // Warning - Improved contrast with black text on yellow
  [
    "warn",
    `${BADGE_BASE_STYLES} text-black bg-yellow-400 ring-yellow-500 dark:text-yellow-100 dark:bg-yellow-600 dark:ring-yellow-500`,
  ],
  [
    "warning",
    `${BADGE_BASE_STYLES} text-black bg-yellow-400 ring-yellow-500 dark:text-yellow-100 dark:bg-yellow-600 dark:ring-yellow-500`,
  ],
  // Error - Bright red at 500 level
  [
    "error",
    `${BADGE_BASE_STYLES} text-white bg-red-500 ring-red-600 dark:text-red-50 dark:bg-red-700 dark:ring-red-500 animate-pulse-subtle`,
  ],
  // Fatal/Critical - Darker red for highest severity
  [
    "fatal",
    `${BADGE_BASE_STYLES} text-white bg-red-700 ring-red-800 dark:text-red-50 dark:bg-red-800 dark:ring-red-700 animate-pulse-subtle`,
  ],
  [
    "critical",
    `${BADGE_BASE_STYLES} text-white bg-red-700 ring-red-800 dark:text-red-50 dark:bg-red-800 dark:ring-red-700 animate-pulse-subtle`,
  ],
  // Trace - Slightly darker gray than Debug
  [
    "trace",
    `${BADGE_BASE_STYLES} text-gray-800 bg-gray-200 ring-gray-300 dark:text-gray-200 dark:bg-gray-600 dark:ring-gray-500`,
  ],
]);

// Default fallback style for unknown severity levels
const defaultSeverityStyle = `${BADGE_BASE_STYLES} text-gray-800 bg-gray-300 ring-gray-400 dark:text-gray-200 dark:bg-gray-700 dark:ring-gray-600`;

// Common severity column names for O(1) lookup
const severityColumnNames = new Set([
  "severity",
  "severity_text",
  "severity_level",
  "level",
  "log_level",
  "lvl",
  "loglevel",
]);

// Keep track of dynamically added fields from source metadata
const dynamicSeverityFields = new Set<string>();

// Function to register a custom severity field from source metadata
export function registerSeverityField(fieldName: string): void {
  if (fieldName && typeof fieldName === 'string') {
    dynamicSeverityFields.add(fieldName.toLowerCase());
  }
}

// Function to get severity badge classes
export function getSeverityClasses(
  value: unknown,
  columnName: string
): string | null {
  // Check if column is in standard list or dynamically registered
  if (
    (!severityColumnNames.has(columnName.toLowerCase()) &&
     !dynamicSeverityFields.has(columnName.toLowerCase())) ||
    typeof value !== "string"
  ) {
    return null;
  }

  // Normalize the value
  const normalizedValue = value.toLowerCase().trim();

  // Get color classes from map
  return (
    severityColorMap.get(normalizedValue) ||
    defaultSeverityStyle
  );
}


// --- Log Content Formatting ---

// Regex for HTTP methods (case-insensitive, word boundaries)
const httpMethodRegex = /\b(GET|POST|PUT|DELETE|HEAD|PATCH|OPTIONS)\b/gi;

// Regex for ISO-like timestamps (YYYY-MM-DDTHH:MM:SS.sss followed by optional Z or +/-HH:MM offset)
// Captures date and time parts separately.
const timestampRegex = /(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}:\d{2}(\.\d+)?)(?:Z|[+-]\d{2}:\d{2})?/g;

// Get status code class for styling
function getStatusCodeClass(code: string): string {
  const firstDigit = code.charAt(0);
  switch (firstDigit) {
    case '1': return 'status-info';     // Informational
    case '2': return 'status-success';  // Success
    case '3': return 'status-redirect'; // Redirection
    case '4': return 'status-error';    // Client Error
    case '5': return 'status-server';   // Server Error
    default:  return 'status-unknown';  // Unknown
  }
}

interface MatchResult {
  index: number;
  length: number;
  type: 'http' | 'timestamp' | 'status';
  value: string; // Full match
  groups?: string[]; // Captured groups (e.g., date, time)
}

/**
 * Formats log content string with special styling for HTTP methods and timestamps.
 * Uses a single pass approach for better performance.
 *
 * @param value The raw string content.
 * @param isStatusColumn Whether this column contains status codes (for more aggressive matching)
 * @returns An array of VNodes or strings.
 */
export function formatLogContent(value: string, isStatusColumn: boolean = false): (VNode | string)[] {
  const results: (VNode | string)[] = [];
  let lastIndex = 0;
  const matches: MatchResult[] = [];

  // Find all HTTP method matches
  let match;
  while ((match = httpMethodRegex.exec(value)) !== null) {
    matches.push({
      index: match.index,
      length: match[0].length,
      type: 'http',
      value: match[0],
    });
  }

  // Find all timestamp matches
  while ((match = timestampRegex.exec(value)) !== null) {
    // Ensure timestamp matches don't overlap with already found HTTP methods
    const overlaps = matches.some(m =>
      (match!.index < m.index + m.length && match!.index + match![0].length > m.index)
    );
    if (!overlaps) {
      matches.push({
        index: match.index,
        length: match[0].length,
        type: 'timestamp',
        value: match[0],
        groups: [match[1], match[2]] // [date, time]
      });
    }
  }

  // Only look for status codes in status columns
  if (isStatusColumn) {
    const statusValue = value.trim();
    // Must be exactly 3 digits and start with 1-5
    if (/^[1-5][0-9]{2}$/.test(statusValue)) {
      matches.push({
        index: value.indexOf(statusValue),
        length: statusValue.length,
        type: 'status',
        value: statusValue,
      });
    }
  }

  // Sort matches by their starting index
  matches.sort((a, b) => a.index - b.index);

  // Process the string and build the VNode array
  for (const currentMatch of matches) {
    // Add text node for content before the current match
    if (currentMatch.index > lastIndex) {
      results.push(value.substring(lastIndex, currentMatch.index));
    }

    // Add styled VNode for the current match
    if (currentMatch.type === 'http') {
      const method = currentMatch.value.toUpperCase();
      const methodClass = ['PATCH', 'OPTIONS'].includes(method)
        ? 'http-method http-method-utility' // Grey background for utility methods
        : `http-method http-method-${method.toLowerCase()}`; // Existing styling for other methods
      results.push(
        h('span', { class: methodClass }, method)
      );
    } else if (currentMatch.type === 'timestamp' && currentMatch.groups) {
      const fullMatch = currentMatch.value;
      const datePart = currentMatch.groups![0];
      const timePart = currentMatch.groups![1];
      const separator = value[currentMatch.index + datePart.length]; // T or space
      // Calculate the offset part based on the full match length minus date, separator, and time
      const offsetPart = fullMatch.substring(datePart.length + 1 + timePart.length);

      const children = [
        h('span', { class: 'timestamp-date' }, datePart),
        h('span', { class: 'timestamp-separator' }, separator),
        h('span', { class: 'timestamp-time' }, timePart),
      ];

      // Add the offset part if it exists
      if (offsetPart) {
        children.push(h('span', { class: 'timestamp-offset' }, offsetPart));
      }

      results.push(h('span', { class: 'timestamp' }, children));
    } else if (currentMatch.type === 'status') {
      const code = currentMatch.value;
      results.push(
        h('span', { class: `status-code ${getStatusCodeClass(code)}` }, code)
      );
    }

    lastIndex = currentMatch.index + currentMatch.length;
  }

  // Add any remaining text after the last match
  if (lastIndex < value.length) {
    results.push(value.substring(lastIndex));
  }

  // If no matches were found, return the original string wrapped in an array
  if (results.length === 0 && value.length > 0) {
    return [value];
  }

  return results;
}
