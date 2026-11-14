import { calendarDayCount, parseRelativeSpec, windowLabel } from "./timeWindow";

/** A calendar range a dashboard or a panel can pin, next to the rolling quick ranges. */
export const CALENDAR_RANGES = ["today", "yesterday", "today-6d", "today-29d"] as const;

interface RangeLabelMessage {
  key: string;
  count: number;
}

/** The i18n message that names a relative range, or null for text that is not one. */
export function relativeRangeLabel(spec: string): RangeLabelMessage | null {
  const parsed = parseRelativeSpec(spec);
  if (!parsed) return null;
  if (parsed.kind === "duration") return windowLabel({ kind: "relative", spec });
  const days = calendarDayCount(spec);
  if (days === null) return { key: "palette.range.yesterday", count: 1 };
  return days === 1 ? { key: "palette.range.today", count: 1 } : { key: "dashboards.rangeCalendarDays", count: days };
}
