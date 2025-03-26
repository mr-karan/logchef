import type { Updater } from "@tanstack/vue-table";
import type { Ref } from "vue";
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
}

export function formatTimestamp(timestamp: string): string {
  try {
    const date = new Date(timestamp);
    // Check if the date is valid
    if (isNaN(date.getTime())) {
      return timestamp;
    }
    // Format as RFC3339 / ISO8601 with milliseconds
    return date.toISOString();
  } catch (e) {
    return timestamp;
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
