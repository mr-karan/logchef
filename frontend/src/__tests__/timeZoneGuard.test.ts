import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

// Types keep a zone paired with its value inside the time module, but cannot stop
// a new file from reading or formatting in the browser zone. These rules can.

// A query variable's date is zone-less wall-clock text; the user's SQL names its
// zone. Browser-zone Date getters only round-trip those fields.
const QUERY_VARIABLE_DATE = [/^components\/date-time-picker\/SingleDatePicker\.vue$/, /^components\/query-editor\/variableUtils\.ts$/];

type Rule = { name: string; offenders: (source: string) => number[]; allowed: RegExp[] };

function linesMatching(pattern: RegExp): (source: string) => number[] {
  return (source) => [...source.matchAll(new RegExp(pattern, "g"))].map((match) => lineOf(source, match.index));
}

function lineOf(source: string, index: number): number {
  return source.slice(0, index).split("\n").length;
}

// The argument text of the call whose "(" is at openIndex.
function callArguments(source: string, openIndex: number): string {
  let depth = 0;
  for (let i = openIndex; i < source.length; i++) {
    if (source[i] === "(") depth++;
    if (source[i] === ")" && --depth === 0) return source.slice(openIndex + 1, i);
  }
  return source.slice(openIndex + 1);
}

// toLocaleDateString and toLocaleTimeString exist only on Date. toLocaleString is
// also on numbers, so it counts only on `new Date(...)` or a name bound to one.
function zonelessDateFormatting(source: string): number[] {
  const dateNames = new Set(
    [...source.matchAll(/\b(?:const|let|var)\s+(\w+)\s*=\s*new Date\(|\b(\w+)\s*:\s*Date\b/g)].map((m) => m[1] ?? m[2]),
  );
  const calls = /(new Date\([^;\n]*?\)|\b\w+)\.toLocale(Date|Time)?String\(/g;
  return [...source.matchAll(calls)]
    .filter(([, receiver, kind]) => kind !== undefined || receiver.startsWith("new Date(") || dateNames.has(receiver))
    .filter((match) => !/\btimeZone\b/.test(callArguments(source, match.index + match[0].length - 1)))
    .map((match) => lineOf(source, match.index));
}

const RULES: Rule[] = [
  {
    name: "@internationalized/date import",
    offenders: linesMatching(/from\s+["']@internationalized\/date["']/),
    allowed: [/^utils\/timeWindow[^/]*\.ts$/, /^components\/date-time-picker\//, /^components\/ui\/calendar\//],
  },
  {
    name: "getLocalTimeZone",
    offenders: linesMatching(/\bgetLocalTimeZone\b/),
    allowed: [/^utils\/timeWindow\.ts$/, /^components\/ui\/calendar\//, ...QUERY_VARIABLE_DATE],
  },
  {
    name: "resolvedOptions().timeZone",
    offenders: linesMatching(/resolvedOptions\(\s*\)\s*\.timeZone\b/),
    allowed: [],
  },
  {
    name: "zoneFromPreference with a fixed preference",
    offenders: linesMatching(/zoneFromPreference\(\s*["']/),
    allowed: [/^utils\/timeWindow[^/]*\.ts$/],
  },
  {
    name: "Date formatted with toLocale*String and no timeZone",
    offenders: zonelessDateFormatting,
    allowed: QUERY_VARIABLE_DATE,
  },
  {
    name: "Date built from wall-clock fields",
    offenders: linesMatching(/new Date\(\s*[\w.?]+\.year\b/),
    allowed: [],
  },
];

const root = resolve(import.meta.dirname, "..");

function sourceFiles(): string[] {
  return readdirSync(root, { recursive: true, encoding: "utf8" })
    .map((name) => name.replaceAll("\\", "/"))
    .filter((name) => /\.(ts|vue)$/.test(name) && !name.includes("__tests__/") && !/\.(test|spec)\.ts$/.test(name));
}

describe("time zone guard", () => {
  it("scans the source tree wherever the tests run from", () => {
    expect(sourceFiles()).toContain("utils/timeWindow.ts");
  });

  it.each(RULES)("allows $name only where its zone is explicit", ({ offenders, allowed }) => {
    const found = sourceFiles()
      .filter((name) => !allowed.some((pattern) => pattern.test(name)))
      .flatMap((name) => offenders(readFileSync(resolve(root, name), "utf8")).map((line) => `${name}:${line}`));
    expect(found).toEqual([]);
  });

  it("tells Date formatting apart from number formatting", () => {
    const flagged = zonelessDateFormatting(
      [
        "count.toLocaleString()",
        "new Date(ts).toLocaleString(locale)",
        "new Date(ts).toLocaleString(locale, { timeZone: zone })",
        "const d = new Date(ts); d.toLocaleString(locale)",
        "total.toLocaleString(locale, { maximumFractionDigits: 1 })",
        "value.toLocaleDateString(locale)",
        "new Date(row[f]).toLocaleTimeString()",
      ].join("\n"),
    );
    expect(flagged).toEqual([2, 4, 6, 7]);
  });
});
