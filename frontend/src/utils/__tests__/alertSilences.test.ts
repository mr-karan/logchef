import { describe, expect, it } from "vitest";
import type { Alert } from "@/api/alerts";
import type { AlertSilence } from "@/api/silences";
import type { ZoneId } from "@/utils/timeWindow";
import { activeSilencesFor, describeSilenceSchedule, fromDateTimeInput, groupMatchers, silenceRequestFrom, silenceStatus, toDateTimeInput } from "../alertSilences";

const rule: Alert = {
  id: 7, source_id: 3, name: "High errors", severity: "critical", labels: { team: "payments", severity: "low" },
  query_language: "clickhouse-sql", editor_mode: "native", query: "SELECT count() FROM logs", lookback_seconds: 300,
  threshold_operator: "gt", threshold_value: 10, frequency_seconds: 60, destination_ids: [], no_data_policy: "zero",
  notify_on_resolve: true, is_active: true, last_state: "firing", created_at: "2026-11-01", updated_at: "2026-11-01",
};

function silence(overrides: Partial<AlertSilence>): AlertSilence {
  return {
    id: 1, source_id: 3, alert_id: null, matchers: {}, comment: "deploy", schedule_kind: "once",
    starts_at: "2026-11-16T10:00:00Z", ends_at: "2026-11-16T11:00:00Z",
    created_at: "2026-11-16T09:00:00Z", updated_at: "2026-11-16T09:00:00Z", active: true,
    ...overrides,
  };
}

describe("activeSilencesFor", () => {
  const group = { service: "checkout" };
  const during = Date.parse("2026-11-16T10:30:00Z");

  it("matches a source-wide silence with no matchers", () => {
    expect(activeSilencesFor([silence({})], rule, group, during)).toHaveLength(1);
  });

  it("matches group values, rule labels and alertname", () => {
    const s = silence({ matchers: { service: "checkout", team: "payments", alertname: "High errors" } });
    expect(activeSilencesFor([s], rule, group, during)).toHaveLength(1);
  });

  it("lets the rule's severity win over a label of the same name", () => {
    expect(activeSilencesFor([silence({ matchers: { severity: "critical" } })], rule, group, during)).toHaveLength(1);
    expect(activeSilencesFor([silence({ matchers: { severity: "low" } })], rule, group, during)).toHaveLength(0);
  });

  it("skips another group, another rule, another source and a silence not yet started", () => {
    expect(activeSilencesFor([silence({ matchers: { service: "cart" } })], rule, group, during)).toHaveLength(0);
    expect(activeSilencesFor([silence({ matchers: { region: "eu" } })], rule, group, during)).toHaveLength(0);
    expect(activeSilencesFor([silence({ alert_id: 8 })], rule, group, during)).toHaveLength(0);
    expect(activeSilencesFor([silence({ source_id: 4 })], rule, group, during)).toHaveLength(0);
    expect(activeSilencesFor([silence({ starts_at: "2026-11-16T10:45:00Z" })], rule, group, during)).toHaveLength(0);
  });

  it("drops a once silence whose end has passed while the page stayed open", () => {
    expect(activeSilencesFor([silence({})], rule, group, Date.parse("2026-11-16T11:00:00Z"))).toHaveLength(0);
  });

  it("matches a silence on this rule", () => {
    expect(activeSilencesFor([silence({ alert_id: 7 })], rule, group, during)).toHaveLength(1);
  });
});

describe("silenceStatus", () => {
  const now = Date.parse("2026-11-16T12:00:00Z");

  it("is active inside a once window, whatever the flag from the last load", () => {
    expect(silenceStatus(silence({ active: false, starts_at: "2026-11-16T11:00:00Z", ends_at: "2026-11-16T13:00:00Z" }), now)).toBe("active");
  });

  it("is expired once its end has passed, even if it was active at load", () => {
    expect(silenceStatus(silence({ active: true }), now)).toBe("expired");
  });

  it("follows the server flag inside a weekly window", () => {
    const weekly = { weekdays: [1], start: "11:00", duration_minutes: 120, timezone: "UTC" };
    expect(silenceStatus(silence({ active: true, schedule_kind: "weekly", starts_at: null, ends_at: null, weekly }), now)).toBe("active");
  });

  it("is scheduled before its window", () => {
    const s = silence({ active: false, starts_at: "2026-11-17T10:00:00Z", ends_at: "2026-11-17T11:00:00Z" });
    expect(silenceStatus(s, now)).toBe("scheduled");
  });

  it("keeps a weekly silence without an end scheduled between windows", () => {
    const s = silence({
      active: false, schedule_kind: "weekly", starts_at: null, ends_at: null,
      weekly: { weekdays: [0], start: "02:00", duration_minutes: 60, timezone: "UTC" },
    });
    expect(silenceStatus(s, now)).toBe("scheduled");
  });
});

describe("groupMatchers", () => {
  it("copies the group's columns", () => {
    expect(groupMatchers(rule, { service: "checkout", region: "eu" })).toEqual({ service: "checkout", region: "eu" });
  });

  it("uses the rule's value for a group column the rule's own labels override", () => {
    const matchers = groupMatchers(rule, { severity: "ERROR", alertname: "x" });
    expect(matchers).toEqual({ severity: "critical", alertname: "High errors" });
    const s = silence({ alert_id: 7, matchers });
    expect(activeSilencesFor([s], rule, { severity: "ERROR", alertname: "x" }, Date.parse("2026-11-16T10:30:00Z"))).toHaveLength(1);
  });
});

describe("silenceRequestFrom", () => {
  const now = Date.parse("2026-11-16T12:00:00Z");

  it("moves a copy of a past once silence to start now with the same length", () => {
    const copy = silenceRequestFrom(silence({ matchers: { service: "checkout" } }), now);
    expect(copy.starts_at).toBe("2026-11-16T12:00:00.000Z");
    expect(copy.ends_at).toBe("2026-11-16T13:00:00.000Z");
    expect(copy.matchers).toEqual({ service: "checkout" });
    expect(copy.alert_id).toBeNull();
  });

  it("keeps a future once silence as it is", () => {
    const s = silence({ starts_at: "2026-11-17T10:00:00Z", ends_at: "2026-11-17T11:00:00Z" });
    expect(silenceRequestFrom(s, now)).toMatchObject({ starts_at: "2026-11-17T10:00:00Z", ends_at: "2026-11-17T11:00:00Z" });
  });

  it("drops the passed last day of a weekly silence", () => {
    const s = silence({
      schedule_kind: "weekly", starts_at: null, ends_at: "2026-11-01T00:00:00Z",
      weekly: { weekdays: [6, 0], start: "22:00", duration_minutes: 240, timezone: "Asia/Kolkata" },
    });
    const copy = silenceRequestFrom(s, now);
    expect(copy.ends_at).toBeNull();
    expect(copy.weekly).toEqual({ weekdays: [6, 0], start: "22:00", duration_minutes: 240, timezone: "Asia/Kolkata" });
  });
});

describe("datetime-local values", () => {
  it("reads and writes wall clock time in the zone", () => {
    const zone = "Asia/Kolkata" as ZoneId;
    const ms = Date.parse("2026-11-16T10:00:00Z");
    expect(toDateTimeInput(ms, zone)).toBe("2026-11-16T15:30");
    expect(fromDateTimeInput("2026-11-16T15:30", zone)).toBe("2026-11-16T10:00:00.000Z");
    expect(fromDateTimeInput("", zone)).toBeNull();
  });
});

describe("describeSilenceSchedule", () => {
  it("lists a weekly window's days Monday first", () => {
    const s = silence({
      schedule_kind: "weekly", starts_at: null, ends_at: null,
      weekly: { weekdays: [0, 1, 6], start: "22:00", duration_minutes: 90, timezone: "Europe/Berlin" },
    });
    const t = (key: string, values?: Record<string, unknown>) => `${key} ${JSON.stringify(values)}`;
    expect(describeSilenceSchedule(s, t, "en", "UTC" as ZoneId)).toBe(
      'silences.scheduleWeekly {"days":"Mon, Sat, Sun","time":"22:00","duration":"1h 30m","timezone":"Europe/Berlin"}',
    );
  });
});
