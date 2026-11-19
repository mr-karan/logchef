import { describe, expect, it } from "vitest";
import { describeSchedule, isCalendarRange, savedQueryVariables, scheduleFor, splitValues } from "../reportSchedule";
import type { SavedQuery } from "@/api/savedQueries";

describe("scheduleFor", () => {
  it("keeps only the fields each frequency uses, as the server requires", () => {
    const weekly = scheduleFor("weekly", { frequency: "daily", hour: 7, minute: 30 });
    expect(weekly).toEqual({ frequency: "weekly", hour: 7, minute: 30, weekdays: [1, 2, 3, 4, 5] });
    expect(scheduleFor("hourly", weekly)).toEqual({ frequency: "hourly", minute: 30 });
    expect(scheduleFor("monthly", weekly)).toEqual({ frequency: "monthly", hour: 7, minute: 30, day_of_month: 1 });
    expect(scheduleFor("daily", { frequency: "monthly", hour: 7, minute: 30, day_of_month: 3 })).toEqual({ frequency: "daily", hour: 7, minute: 30 });
  });

  it("keeps chosen weekdays in order when switching back to weekly", () => {
    expect(scheduleFor("weekly", { frequency: "weekly", hour: 9, minute: 0, weekdays: [6, 0, 3] }).weekdays).toEqual([0, 3, 6]);
  });
});

describe("describeSchedule", () => {
  const t = (key: string, values?: Record<string, unknown>) => `${key} ${JSON.stringify(values ?? {})}`;
  it("names each frequency with its time", () => {
    expect(describeSchedule({ frequency: "hourly", minute: 5 }, t, "en")).toBe('reports.scheduleHourly {"minute":"05"}');
    expect(describeSchedule({ frequency: "daily", hour: 9, minute: 0 }, t, "en")).toBe('reports.scheduleDaily {"time":"09:00"}');
    expect(describeSchedule({ frequency: "weekly", hour: 9, minute: 0, weekdays: [5, 4, 3, 2, 1] }, t, "en")).toBe('reports.scheduleWeekdays {"time":"09:00"}');
    expect(describeSchedule({ frequency: "weekly", hour: 18, minute: 30, weekdays: [3, 1] }, t, "en")).toBe('reports.scheduleWeekly {"days":"Mon, Wed","time":"18:30"}');
    expect(describeSchedule({ frequency: "monthly", hour: 9, minute: 0, day_of_month: 28 }, t, "en")).toBe('reports.scheduleMonthly {"day":28,"time":"09:00"}');
  });
});

describe("isCalendarRange", () => {
  it("tells calendar ranges from rolling ones", () => {
    expect(["today", "yesterday", "today-6d"].map(isCalendarRange)).toEqual([true, true, true]);
    expect(["24h", "7d", "1h"].map(isCalendarRange)).toEqual([false, false, false]);
  });
});

describe("savedQueryVariables", () => {
  function query(content: string): SavedQuery {
    return { id: 1, source_id: 1, name: "q", description: "", query_language: "clickhouse-sql", editor_mode: "native", query_content: content, created_at: "", updated_at: "" };
  }
  it("reads names, labels and the stored default", () => {
    const content = JSON.stringify({ content: "SELECT 1", variables: [
      { name: "host", label: "Host", type: "text", value: "", defaultValue: "api-1" },
      { name: "env", label: "", type: "text", value: "prod", isOptional: true },
      { name: "day", type: "day", value: "" },
      { name: "hosts", type: "text", inputType: "multiselect", value: [], defaultValue: ["api-1", "api-2"] },
      { name: "codes", type: "number", inputType: "multiselect", value: [] },
    ] });
    expect(savedQueryVariables(query(content))).toEqual([
      { name: "host", label: "Host", optional: false, multiple: false, fallback: "api-1" },
      { name: "env", label: "env", optional: true, multiple: false, fallback: "prod" },
      { name: "day", label: "day", optional: false, multiple: false, fallback: "" },
      { name: "hosts", label: "hosts", optional: false, multiple: true, fallback: "api-1, api-2" },
      { name: "codes", label: "codes", optional: false, multiple: true, fallback: "" },
    ]);
  });
  it("returns none for content that does not parse", () => {
    expect(savedQueryVariables(query("{not json"))).toEqual([]);
    expect(savedQueryVariables(query("{}"))).toEqual([]);
  });
});

describe("splitValues", () => {
  it("reads comma-separated values and drops blanks", () => {
    expect(splitValues(" api-1, api-2 ,, ")).toEqual(["api-1", "api-2"]);
  });
});
