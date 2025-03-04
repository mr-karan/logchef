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

// Common badge styles
const BADGE_BASE_STYLES =
  "inline-flex items-center rounded-md px-2 py-1 text-xs font-semibold uppercase tracking-wide ring-1 ring-inset shadow-sm";

// Severity level color mapping with improved contrast
const severityColorMap = new Map([
  // Debug - Neutral Gray
  [
    "debug",
    `${BADGE_BASE_STYLES} text-gray-700 bg-gray-200 ring-gray-300 dark:text-gray-300 dark:bg-gray-800 dark:ring-gray-600`,
  ],
  // Info - Blue (Lighter for readability)
  [
    "info",
    `${BADGE_BASE_STYLES} text-blue-800 bg-blue-200 ring-blue-300 dark:text-blue-300 dark:bg-blue-900 dark:ring-blue-600`,
  ],
  [
    "information",
    `${BADGE_BASE_STYLES} text-blue-800 bg-blue-200 ring-blue-300 dark:text-blue-300 dark:bg-blue-900 dark:ring-blue-600`,
  ],
  // Warning - Amber (More readable than yellow)
  [
    "warn",
    `${BADGE_BASE_STYLES} text-amber-800 bg-amber-200 ring-amber-300 dark:text-amber-300 dark:bg-amber-900 dark:ring-amber-600`,
  ],
  [
    "warning",
    `${BADGE_BASE_STYLES} text-amber-800 bg-amber-200 ring-amber-300 dark:text-amber-300 dark:bg-amber-900 dark:ring-amber-600`,
  ],
  // Error - Red (Stronger contrast)
  [
    "error",
    `${BADGE_BASE_STYLES} text-red-800 bg-red-200 ring-red-300 dark:text-red-300 dark:bg-red-900 dark:ring-red-600`,
  ],
  // Fatal/Critical - Deep Red (Striking difference)
  [
    "fatal",
    `${BADGE_BASE_STYLES} text-red-900 bg-red-300 ring-red-400 dark:text-red-200 dark:bg-red-950 dark:ring-red-700`,
  ],
  [
    "critical",
    `${BADGE_BASE_STYLES} text-red-900 bg-red-300 ring-red-400 dark:text-red-200 dark:bg-red-950 dark:ring-red-700`,
  ],
  // Trace - Cool Gray (More subtle)
  [
    "trace",
    `${BADGE_BASE_STYLES} text-gray-600 bg-gray-100 ring-gray-300 dark:text-gray-400 dark:bg-gray-800 dark:ring-gray-700`,
  ],
]);

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
    `${BADGE_BASE_STYLES} text-gray-700 bg-gray-200 ring-gray-300 dark:text-gray-300 dark:bg-gray-800 dark:ring-gray-600`
  );
}
