import type { Alert } from "@/api/alerts";
import type { AlertSilence, AlertSilenceRequest } from "@/api/silences";
import { epochMs, formatInZone, formatWallClock, parseWallClock, type ZoneId } from "@/utils/timeWindow";
import { weekdayNames } from "@/utils/reportSchedule";

export type SilenceStatus = "active" | "scheduled" | "expired";

/**
 * The labels a silence matches against: the rule's labels, the group values,
 * and alertname and severity, which win a name clash. Mirrors
 * SilenceLabels in internal/alerts/silence.go.
 */
export function silenceLabels(alert: Alert, group: Record<string, string> | undefined): Record<string, string> {
  return { ...alert.labels, ...group, alertname: alert.name, severity: alert.severity };
}

/**
 * The matchers that silence one group: each group column with the value the
 * server matches. A group column named alertname or severity takes the rule's
 * value, because the rule's value wins that clash.
 */
export function groupMatchers(alert: Alert, group: Record<string, string> | undefined): Record<string, string> {
  const labels = silenceLabels(alert, group);
  return Object.fromEntries(Object.keys(group ?? {}).map((key) => [key, labels[key]]));
}

/**
 * Whether the silence is active at now. A once window and a passed last day
 * are exact here; inside a weekly window the server's `active` flag from the
 * last load decides, since that needs the window's time zone rules.
 */
export function silenceActiveAt(silence: AlertSilence, now: number): boolean {
  if (silence.ends_at && new Date(silence.ends_at).getTime() <= now) return false;
  if (silence.schedule_kind === "once") return !!silence.starts_at && new Date(silence.starts_at).getTime() <= now;
  return silence.active;
}

/** The active silences that mute this rule's group. */
export function activeSilencesFor(silences: AlertSilence[], alert: Alert, group: Record<string, string> | undefined, now: number): AlertSilence[] {
  const labels = silenceLabels(alert, group);
  return silences.filter((s) =>
    silenceActiveAt(s, now) &&
    s.source_id === alert.source_id &&
    (s.alert_id == null || s.alert_id === alert.id) &&
    Object.entries(s.matchers ?? {}).every(([key, value]) => labels[key] === value),
  );
}

export function silenceStatus(silence: AlertSilence, now: number): SilenceStatus {
  if (silenceActiveAt(silence, now)) return "active";
  if (silence.ends_at && new Date(silence.ends_at).getTime() <= now) return "expired";
  return "scheduled";
}

/** "2h 30m", "45m", "1h". */
export function formatMinutes(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (!h) return `${m}m`;
  return m ? `${h}h ${m}m` : `${h}h`;
}

type Translate = (key: string, values?: Record<string, unknown>) => string;

export function describeSilenceSchedule(silence: AlertSilence, t: Translate, locale: string, zone: ZoneId): string {
  const when = (value: string) => formatInZone(epochMs(new Date(value)), zone, { dateStyle: "medium", timeStyle: "short" }, locale);
  if (silence.schedule_kind === "once") {
    return t("silences.scheduleOnce", { start: when(silence.starts_at ?? ""), end: when(silence.ends_at ?? "") });
  }
  const weekly = silence.weekly;
  if (!weekly) return "";
  const names = weekdayNames(locale);
  const text = t("silences.scheduleWeekly", {
    // Monday first, as the form shows the week.
    days: [...weekly.weekdays].sort((a, b) => ((a + 6) % 7) - ((b + 6) % 7)).map((d) => names[d]).join(", "),
    time: weekly.start,
    duration: formatMinutes(weekly.duration_minutes),
    timezone: weekly.timezone,
  });
  return silence.ends_at ? `${text}, ${t("silences.scheduleUntil", { date: when(silence.ends_at) })}` : text;
}

/** A datetime-local input value ("YYYY-MM-DDTHH:MM") for the instant in the zone. */
export function toDateTimeInput(ms: number, zone: ZoneId): string {
  return formatWallClock(epochMs(ms), zone).slice(0, 16).replace(" ", "T");
}

/** The instant a datetime-local value names in the zone, as RFC 3339, or null when it is not a time. */
export function fromDateTimeInput(value: string, zone: ZoneId): string | null {
  const ms = parseWallClock(value, zone);
  return ms === null ? null : new Date(ms).toISOString();
}

/**
 * The request that copies a silence. A once silence keeps its length but
 * starts now when its start has passed, so a copy of an old silence is valid.
 */
export function silenceRequestFrom(silence: AlertSilence, now: number): AlertSilenceRequest {
  let startsAt = silence.starts_at ?? null;
  let endsAt = silence.ends_at ?? null;
  if (silence.schedule_kind === "once" && startsAt && endsAt) {
    const start = new Date(startsAt).getTime();
    if (start < now) {
      const length = new Date(endsAt).getTime() - start;
      startsAt = new Date(now).toISOString();
      endsAt = new Date(now + length).toISOString();
    }
  } else if (endsAt && new Date(endsAt).getTime() <= now) {
    endsAt = null;
  }
  return {
    source_id: silence.source_id,
    alert_id: silence.alert_id ?? null,
    matchers: { ...silence.matchers },
    comment: silence.comment,
    schedule_kind: silence.schedule_kind,
    starts_at: startsAt,
    ends_at: endsAt,
    weekly: silence.weekly ? { ...silence.weekly, weekdays: [...silence.weekly.weekdays] } : null,
  };
}
