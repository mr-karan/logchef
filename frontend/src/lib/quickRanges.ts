// Relative time ranges offered as one-click choices in time pickers. `value` is
// the relative-time string the rest of the app parses ("15m", "6h", "7d").
export const QUICK_RANGES = [
  { value: "5m", duration: { minutes: 5 } },
  { value: "15m", duration: { minutes: 15 } },
  { value: "30m", duration: { minutes: 30 } },
  { value: "1h", duration: { hours: 1 } },
  { value: "3h", duration: { hours: 3 } },
  { value: "6h", duration: { hours: 6 } },
  { value: "12h", duration: { hours: 12 } },
  { value: "24h", duration: { hours: 24 } },
  { value: "2d", duration: { days: 2 } },
  { value: "7d", duration: { days: 7 } },
  { value: "30d", duration: { days: 30 } },
  { value: "90d", duration: { days: 90 } },
] as const;

export type QuickRange = (typeof QUICK_RANGES)[number];

/** The i18n message for a range's label, e.g. "Last 15 minutes". Pass `count` as the plural choice. */
export function quickRangeLabelMessage(range: QuickRange): { key: string; count: number } {
  if ("minutes" in range.duration) return { key: "time.lastMinutes", count: range.duration.minutes };
  if ("hours" in range.duration) return { key: "time.lastHours", count: range.duration.hours };
  return { key: "time.lastDays", count: range.duration.days };
}
