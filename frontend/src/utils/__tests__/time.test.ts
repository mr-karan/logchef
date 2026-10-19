import { readdirSync, readFileSync } from "node:fs";
import { join, relative, resolve } from "node:path";
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

// Explorer wall-clock fields mean time in the Explorer's timezone. Building a
// Date from those fields, or fields from a Date's getters, uses the browser's
// zone instead and shifts the instant. Convert with the helpers above.
describe("calendar field conversions", () => {
  const browserZonePatterns = [
    /new Date\(\s*[\w$.?]+\.year\b/,
    /new CalendarDateTime\(\s*[\w$.?]+\.getFullYear\(\)/,
  ];

  function sourceFiles(dir: string): string[] {
    return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) return entry.name === "__tests__" ? [] : sourceFiles(path);
      return /\.(ts|vue)$/.test(entry.name) && !/\.(test|spec)\.ts$/.test(entry.name) ? [path] : [];
    });
  }

  it("never go through the browser's zone", () => {
    const offenders = sourceFiles(resolve("src")).filter((file) => {
      const text = readFileSync(file, "utf8");
      return browserZonePatterns.some((pattern) => pattern.test(text));
    });
    expect(offenders.map((file) => relative(resolve("src"), file))).toEqual([]);
  });
});
