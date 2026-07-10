import { describe, it, expect } from "vitest";
import { matchesColumnFilter, parseNumericFilter } from "../columnFilter";

describe("parseNumericFilter", () => {
  it("parses a bare number as an implicit equality filter", () => {
    expect(parseNumericFilter("42")).toEqual({ operator: "=", value: 42 });
  });

  it("parses each supported operator", () => {
    expect(parseNumericFilter(">5")).toEqual({ operator: ">", value: 5 });
    expect(parseNumericFilter(">=5")).toEqual({ operator: ">=", value: 5 });
    expect(parseNumericFilter("<5")).toEqual({ operator: "<", value: 5 });
    expect(parseNumericFilter("<=5")).toEqual({ operator: "<=", value: 5 });
    expect(parseNumericFilter("=5")).toEqual({ operator: "=", value: 5 });
    expect(parseNumericFilter("!=5")).toEqual({ operator: "!=", value: 5 });
  });

  it("tolerates surrounding and internal whitespace", () => {
    expect(parseNumericFilter("  >=  12.5  ")).toEqual({ operator: ">=", value: 12.5 });
  });

  it("parses negative and decimal numbers", () => {
    expect(parseNumericFilter("-3.14")).toEqual({ operator: "=", value: -3.14 });
    expect(parseNumericFilter("<=-1")).toEqual({ operator: "<=", value: -1 });
  });

  it("returns null for non-numeric input", () => {
    expect(parseNumericFilter("error")).toBeNull();
    expect(parseNumericFilter("500ms")).toBeNull();
    expect(parseNumericFilter("")).toBeNull();
    expect(parseNumericFilter(">")).toBeNull();
  });
});

describe("matchesColumnFilter", () => {
  it("matches everything when the filter is empty or whitespace", () => {
    expect(matchesColumnFilter("anything", "")).toBe(true);
    expect(matchesColumnFilter("anything", "   ")).toBe(true);
    expect(matchesColumnFilter(null, "")).toBe(true);
  });

  it("does a case-insensitive contains match for text", () => {
    expect(matchesColumnFilter("GET /api/users", "api")).toBe(true);
    expect(matchesColumnFilter("GET /api/users", "API")).toBe(true);
    expect(matchesColumnFilter("GET /api/users", "missing")).toBe(false);
  });

  it("treats null/undefined cells as empty text", () => {
    expect(matchesColumnFilter(null, "foo")).toBe(false);
    expect(matchesColumnFilter(undefined, "foo")).toBe(false);
    expect(matchesColumnFilter(null, "")).toBe(true);
  });

  it("stringifies object cell values for text matching", () => {
    expect(matchesColumnFilter({ code: "ETIMEDOUT" }, "etimedout")).toBe(true);
  });

  describe("numeric comparisons against numeric cells", () => {
    it("applies equality by default", () => {
      expect(matchesColumnFilter(500, "500")).toBe(true);
      expect(matchesColumnFilter(200, "500")).toBe(false);
    });

    it("applies >, >=, <, <=, != operators", () => {
      expect(matchesColumnFilter(500, ">400")).toBe(true);
      expect(matchesColumnFilter(500, ">500")).toBe(false);
      expect(matchesColumnFilter(500, ">=500")).toBe(true);
      expect(matchesColumnFilter(500, "<600")).toBe(true);
      expect(matchesColumnFilter(500, "<=500")).toBe(true);
      expect(matchesColumnFilter(500, "!=200")).toBe(true);
      expect(matchesColumnFilter(500, "!=500")).toBe(false);
    });

    it("coerces numeric-looking string cells (e.g. from ClickHouse) before comparing", () => {
      expect(matchesColumnFilter("500", ">=400")).toBe(true);
      expect(matchesColumnFilter("42.5", "<50")).toBe(true);
    });
  });

  it("falls back to text contains when the filter is numeric but the cell isn't", () => {
    // e.g. filtering a message column for the digits "500" shouldn't require an exact numeric match
    expect(matchesColumnFilter("request to /orders/500 failed", "500")).toBe(true);
    expect(matchesColumnFilter("no numbers here", "500")).toBe(false);
  });

  it("does not treat booleans as numeric", () => {
    expect(matchesColumnFilter(true, "1")).toBe(false);
    expect(matchesColumnFilter(true, "true")).toBe(true);
  });
});
