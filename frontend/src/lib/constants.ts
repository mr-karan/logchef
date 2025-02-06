// Toast durations in milliseconds
export const TOAST_DURATION = {
  SUCCESS: 3000, // 3 seconds for success messages
  ERROR: 5000, // 5 seconds for error messages (longer to read)
  WARNING: 4000, // 4 seconds for warnings
  INFO: 3000, // 3 seconds for info messages
} as const;

// Query modes
export const QUERY_MODE = {
  FILTERS: "filters",
  RAW_SQL: "raw_sql",
} as const;

export type QueryMode = (typeof QUERY_MODE)[keyof typeof QUERY_MODE];
