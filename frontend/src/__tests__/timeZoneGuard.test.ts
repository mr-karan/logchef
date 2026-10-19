import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

// Types keep a zone paired with its value inside the time module, but cannot stop
// a new file from building browser-zone wall clocks. These rules can.
const RULES = [
  {
    name: "@internationalized/date import",
    pattern: /from\s+["']@internationalized\/date["']/,
    allowed: [/^utils\/timeWindow[^/]*\.ts$/, /^components\/date-time-picker\//, /^components\/ui\/calendar\//],
  },
  {
    name: "getLocalTimeZone",
    pattern: /\bgetLocalTimeZone\b/,
    allowed: [/^utils\/timeWindow\.ts$/, /^components\/ui\/calendar\//],
  },
  {
    name: "Date built from wall-clock fields",
    pattern: /new Date\(\s*[\w.?]+\.year\b/,
    allowed: [],
  },
];

const root = resolve("src");

function sourceFiles(): string[] {
  return readdirSync(root, { recursive: true, encoding: "utf8" })
    .map((name) => name.replaceAll("\\", "/"))
    .filter((name) => /\.(ts|vue)$/.test(name) && !name.includes("__tests__/") && !/\.(test|spec)\.ts$/.test(name));
}

describe("time zone guard", () => {
  it.each(RULES)("allows $name only inside the time module", ({ pattern, allowed }) => {
    const offenders = sourceFiles().filter(
      (name) => pattern.test(readFileSync(`${root}/${name}`, "utf8")) && !allowed.some((dir) => dir.test(name)),
    );
    expect(offenders).toEqual([]);
  });
});
