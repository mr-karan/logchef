import type { ReportFrequency, ReportRunStatus, ReportSchedule } from "@/api/reports";
import type { SavedQuery } from "@/api/savedQueries";

const WEEKDAYS_MON_FRI = [1, 2, 3, 4, 5];

/**
 * The schedule for frequency, keeping what prev already set. It holds only
 * the fields the frequency uses, because the server rejects any other.
 */
export function scheduleFor(frequency: ReportFrequency, prev?: ReportSchedule): ReportSchedule {
  const minute = prev?.minute ?? 0;
  const hour = prev?.hour ?? 9;
  switch (frequency) {
    case "hourly":
      return { frequency, minute };
    case "daily":
      return { frequency, hour, minute };
    case "weekly":
      return { frequency, hour, minute, weekdays: prev?.weekdays?.length ? [...prev.weekdays].sort((a, b) => a - b) : [...WEEKDAYS_MON_FRI] };
    case "monthly":
      return { frequency, hour, minute, day_of_month: prev?.day_of_month ?? 1 };
  }
}

export function scheduleTime(schedule: ReportSchedule): string {
  return `${String(schedule.hour ?? 0).padStart(2, "0")}:${String(schedule.minute).padStart(2, "0")}`;
}

/** Short weekday names, Sunday first, in the given locale. */
export function weekdayNames(locale: string): string[] {
  const format = new Intl.DateTimeFormat(locale, { weekday: "short", timeZone: "UTC" });
  // 2023-01-01 was a Sunday.
  return Array.from({ length: 7 }, (_, d) => format.format(new Date(Date.UTC(2023, 0, 1 + d))));
}

type Translate = (key: string, values?: Record<string, unknown>) => string;

/** "Daily at 09:00", "Mon, Wed at 09:00", "Weekdays at 09:00", ... */
export function describeSchedule(schedule: ReportSchedule, t: Translate, locale: string): string {
  const time = scheduleTime(schedule);
  switch (schedule.frequency) {
    case "hourly":
      return t("reports.scheduleHourly", { minute: String(schedule.minute).padStart(2, "0") });
    case "daily":
      return t("reports.scheduleDaily", { time });
    case "weekly": {
      const days = [...(schedule.weekdays ?? [])].sort((a, b) => a - b);
      if (days.join() === WEEKDAYS_MON_FRI.join()) return t("reports.scheduleWeekdays", { time });
      const names = weekdayNames(locale);
      return t("reports.scheduleWeekly", { days: days.map((d) => names[d]).join(", "), time });
    }
    case "monthly":
      return t("reports.scheduleMonthly", { day: schedule.day_of_month ?? 1, time });
  }
}

/** The relative ranges a report offers, as RFC 0003 writes them. */
export const REPORT_TIME_RANGES = ["1h", "6h", "12h", "24h", "7d", "30d", "today", "yesterday", "today-6d"] as const;

/** Calendar ranges resolve at the send time and refuse the day_start anchor. */
export function isCalendarRange(range: string): boolean {
  return range === "today" || range === "yesterday" || range.startsWith("today-");
}

export interface ReportVariable {
  name: string;
  label: string;
  optional: boolean;
  /** The query takes a list: the editor reads comma-separated values. */
  multiple: boolean;
  /** The stored default, which a report uses when it sets no value, as text. */
  fallback: string;
}

function hasValue(v: unknown): v is string | string[] {
  return (typeof v === "string" && v.trim() !== "") || (Array.isArray(v) && v.length > 0);
}

/** The variables a saved query declares, for the report editor. */
export function savedQueryVariables(query: SavedQuery): ReportVariable[] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(query.query_content);
  } catch {
    return [];
  }
  const vars = (parsed as { variables?: unknown }).variables;
  if (!Array.isArray(vars)) return [];
  return vars.flatMap((v: Record<string, unknown>) => {
    if (typeof v?.name !== "string") return [];
    const fallback = [v.defaultValue, v.value].find(hasValue);
    return [{
      name: v.name,
      label: typeof v.label === "string" && v.label ? v.label : v.name,
      optional: v.isOptional === true,
      multiple: v.inputType === "multiselect" || Array.isArray(fallback),
      fallback: Array.isArray(fallback) ? fallback.join(", ") : (fallback ?? ""),
    }];
  });
}

/** A list variable's text as the list the server takes. */
export function splitValues(text: string): string[] {
  return text.split(",").map((v) => v.trim()).filter(Boolean);
}

export type BadgeVariant = "default" | "secondary" | "destructive" | "success" | "outline";

export function runStatusVariant(status: ReportRunStatus): BadgeVariant {
  switch (status) {
    case "sent": return "success";
    case "failed": case "interrupted": return "destructive";
    case "partial": return "default";
    case "running": case "skipped": return "secondary";
  }
}
