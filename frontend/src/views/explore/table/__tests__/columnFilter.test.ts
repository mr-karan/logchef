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

  describe("operator fall-through on non-numeric cells", () => {
    it("handles != as an explicit negation of the text contains-match, not a literal '!=<value>' search", () => {
      // Previously this fell through to a literal contains-match for the
      // string "!=500", which could never match real data.
      expect(matchesColumnFilter("order 500 shipped", "!=500")).toBe(false);
      expect(matchesColumnFilter("order 501 shipped", "!=500")).toBe(true);
      expect(matchesColumnFilter("no numbers here", "!=500")).toBe(true);
    });

    it("keeps the bare-value contains-match for the implicit/explicit '=' operator", () => {
      expect(matchesColumnFilter("order 500 shipped", "=500")).toBe(true);
      expect(matchesColumnFilter("order 501 shipped", "=500")).toBe(false);
    });

    it("does not match ordering operators (>, >=, <, <=) against non-numeric cells", () => {
      // Ordering has no meaningful text analog, and falling through to a
      // literal search for e.g. ">400" would just always fail silently -
      // an explicit non-match is more predictable than that.
      expect(matchesColumnFilter("order 500 shipped", ">400")).toBe(false);
      expect(matchesColumnFilter("order 500 shipped", ">=400")).toBe(false);
      expect(matchesColumnFilter("order 500 shipped", "<600")).toBe(false);
      expect(matchesColumnFilter("order 500 shipped", "<=600")).toBe(false);
    });
  });

  describe("toComparableNumber tightening (exotic numeric-string forms)", () => {
    it("does not treat whitespace-only strings as the number 0", () => {
      // Number("   ") === 0 in JS; that should not make "=0" match blank cells.
      expect(matchesColumnFilter("   ", "=0")).toBe(false);
      expect(matchesColumnFilter("", "=0")).toBe(false);
    });

    it("does not coerce hex literals", () => {
      // Number("0x10") === 16; the cell text "0x10" should not numerically match "=16" -
      // it instead falls back to a plain text contains-match against "16" (absent here).
      expect(matchesColumnFilter("0x10", "=16")).toBe(false);
      // "=10" still matches via the text contains-match fallback, since "10" is
      // literally a substring of "0x10" - that's the pre-existing text-search
      // behaviour, not a numeric coercion of the hex literal.
      expect(matchesColumnFilter("0x10", "=10")).toBe(true);
    });

    it("does not coerce exponential notation", () => {
      // Number("1e3") === 1000; the literal string "1e3" should not satisfy "=1000".
      expect(matchesColumnFilter("1e3", "=1000")).toBe(false);
      // It still falls back to a plain text contains-match against "1".
      expect(matchesColumnFilter("1e3", "1")).toBe(true);
    });

    it("does not coerce Infinity/NaN literals", () => {
      expect(matchesColumnFilter("Infinity", ">1000000")).toBe(false);
      expect(matchesColumnFilter("NaN", "=0")).toBe(false);
    });

    it("still coerces plain signed/decimal numeric strings", () => {
      expect(matchesColumnFilter("-42.5", "<=-40")).toBe(true);
      expect(matchesColumnFilter(" 500 ", ">=400")).toBe(true);
    });
  });
});
