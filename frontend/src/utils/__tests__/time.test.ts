import { afterEach, describe, expect, it, vi } from "vitest";
import { parseRelativeTimeString } from "@/utils/time";

function at(isoLocal: string) {
  vi.useFakeTimers();
  vi.setSystemTime(new Date(isoLocal));
}

describe("parseRelativeTimeString", () => {
  afterEach(() => vi.useRealTimers());

  it.each([
    ["2026-11-01T10:00:00", "2026-10-31T00:00:00", "2026-11-01T00:00:00"],
    ["2026-01-01T10:00:00", "2025-12-31T00:00:00", "2026-01-01T00:00:00"],
    ["2026-03-15T10:00:00", "2026-03-14T00:00:00", "2026-03-15T00:00:00"],
  ])("yesterday on %s covers the whole previous day", (clock, start, end) => {
    at(clock);
    const range = parseRelativeTimeString("yesterday");
    expect(range.start.toString()).toBe(start);
    expect(range.end.toString()).toBe(end);
  });

  it("today starts at midnight", () => {
    at("2026-11-01T10:00:00");
    expect(parseRelativeTimeString("today").start.toString()).toBe("2026-11-01T00:00:00");
  });
});
