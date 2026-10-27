import { describe, expect, it } from "vitest";
import { buildSQLValueInsertText, getSQLCompletionCursor } from "../sql-value-completion";

const schema = {
  method: { type: "LowCardinality(String)" },
  status: { type: "UInt16" },
  tags: { type: "Array(String)" },
  host: { type: "String" },
  level: { type: "LowCardinality(String)" },
};

function cursorAtCaret(query: string) {
  const offset = query.indexOf("|");
  return getSQLCompletionCursor(query.replace("|", ""), offset, schema);
}

function contextAtCaret(query: string) {
  const cursor = cursorAtCaret(query);
  return cursor.kind === "value" ? cursor : null;
}

describe("SQL string value completion", () => {
  it.each([
    "SELECT * FROM http WHERE method = '|'",
    "SELECT * FROM http WHERE method != '|'",
    "SELECT * FROM http WHERE method <> '|'",
    "SELECT * FROM http WHERE method IN ('|')",
    "SELECT * FROM http WHERE method in ('GET', '|')",
    "SELECT * FROM http WHERE method IN ('GET', 'POST', '|')",
    "SELECT * FROM http WHERE method LIKE '|'",
    "SELECT * FROM http WHERE `method` = '|'",
    'SELECT * FROM http WHERE "method" = \'|\'',
    "SELECT * FROM http WHERE h.method = '|'",
    "SELECT * FROM http WHERE method /* predicate */ = '|'",
    "SELECT * FROM http WHERE -- previous predicate\nmethod = '|'",
    "SELECT * FROM http WHERE method = '|",
    "SELECT * FROM http WHERE method NOT IN ('|')",
    "SELECT * FROM http WHERE method not in ('GET', '|')",
    "SELECT * FROM http WHERE method NOT LIKE '|'",
  ])("offers method values for %s", (query) => {
    expect(contextAtCaret(query)).toMatchObject({ field: "method", fieldType: "LowCardinality(String)", partial: "" });
  });

  it.each([
    "SELECT * FROM http WHERE method = 'GE|'",
    "SELECT * FROM http WHERE method IN ('POST', 'GE|')",
    "SELECT * FROM http WHERE method LIKE 'GE|'",
  ])("filters using the partial string in %s", (query) => {
    expect(contextAtCaret(query)?.partial).toBe("GE");
  });

  it.each([
    "SELECT * FROM http -- method = '|'",
    "SELECT * FROM http /* method = '|' */",
    "SELECT 'method = |'",
    'SELECT "method = \'|\'"',
    "SELECT `method = '|`",
    "SELECT * FROM http WHERE unknown = '|'",
    "SELECT * FROM http WHERE toString = '|'",
    "SELECT * FROM http WHERE tags = '|'",
    "SELECT * FROM http WHERE method > '|'",
    "SELECT * FROM http WHERE method >= '|'",
    "SELECT * FROM http WHERE method = 'GET' |",
    "SELECT * FROM http WHERE method IN (lower('GET'), '|')",
    "SELECT * FROM http WHERE method IN '|'",
    "SELECT * FROM http WHERE method = 'GET', '|'",
    "SELECT * FROM http WHERE method IN ('GET') AND '|'",
    "SELECT concat('method = ', '|')",
  ])("offers no values for %s", (query) => {
    expect(contextAtCaret(query)).toBeNull();
  });

  it.each([
    "SELECT * FROM http -- method = '|'",
    "SELECT * FROM http /* method = '|' */",
    "SELECT 'method = |'",
    "SELECT `method = '|`",
    "SELECT * FROM http WHERE unknown = '|'",
    "SELECT * FROM http WHERE method > '|'",
  ])("reports a literal without values for %s", (query) => {
    expect(cursorAtCaret(query)).toEqual({ kind: "literal" });
  });

  it("closes earlier literals when a later literal is unclosed", () => {
    expect(contextAtCaret("SELECT * FROM http WHERE host = 'a' AND method = '|")).toMatchObject({ field: "method", partial: "" });
  });

  it.each([
    "SELECT * FROM http WHERE method = 'GET' |",
    "SELECT * FROM http WHERE |",
    "SEL|",
    "SELECT * FROM http WHERE host = 'a' AND | AND method = '",
  ])("reports SQL code for %s", (query) => {
    expect(cursorAtCaret(query)).toEqual({ kind: "code" });
  });

  it.each([
    { query: "method = 'GE|'", value: "GET", expected: "method = 'GET'" },
    { query: "method = 'G|ET' AND status = 200", value: "POST", expected: "method = 'POST' AND status = 200" },
    { query: "method = '|", value: "GET", expected: "method = 'GET'" },
    { query: "method IN ('GET', '|')", value: "POST", expected: "method IN ('GET', 'POST')" },
    { query: "method = 'it''|s'", value: "it's", expected: "method = 'it''s'" },
    { query: "method = '|'", value: "a\\b'c", expected: "method = 'a\\\\b''c'" },
    { query: "method = '|'", value: "", expected: "method = ''" },
    { query: "method = '| AND status = 'x'", value: "GET", expected: "method = 'GET' AND status = 'x'" },
    { query: "method = '| AND referer = 'a' AND host = 'b'", value: "GET", expected: "method = 'GET' AND referer = 'a' AND host = 'b'" },
    { query: "method = 'G|ET' AND referer = 'a'", value: "POST", expected: "method = 'POST' AND referer = 'a'" },
    { query: "method = 'G|ET' -- the user's filter", value: "POST", expected: "method = 'POST' -- the user's filter" },
    { query: "WHERE level = '| AND msg = 'x'", value: "GET", expected: "WHERE level = 'GET' AND msg = 'x'" },
    { query: "WHERE host = 'G|ET' AND method = '", value: "POST", expected: "WHERE host = 'POST' AND method = '" },
    { query: "method = 'G|ET' AND host = '", value: "GET", expected: "method = 'GET' AND host = '" },
    { query: "method IN ('G|ET', 'POST') AND host = '", value: "PUT", expected: "method IN ('PUT', 'POST') AND host = '" },
    { query: "method = '| AND path = '/api'", value: "GET", expected: "method = 'GET' AND path = '/api'" },
    { query: "method = 'G|ET'::String", value: "POST", expected: "method = 'POST'::String" },
    { query: "method = 'G|ET'||'x'", value: "POST", expected: "method = 'POST'||'x'" },
    { query: "method = 'G|ET'-- c", value: "POST", expected: "method = 'POST'-- c" },
    { query: "method = 'G|ET'/* c */ AND status = 200", value: "POST", expected: "method = 'POST'/* c */ AND status = 200" },
  ])("inserts $value into $query with SQL escaping and one closing quote", ({ query, value, expected }) => {
    const context = contextAtCaret(query);
    expect(context).not.toBeNull();
    if (!context) return;
    const text = query.replace("|", "");
    expect(text.slice(0, context.start) + buildSQLValueInsertText(value) + text.slice(context.end)).toBe(expected);
  });

  it("decodes escaped quote and backslash prefixes for filtering", () => {
    expect(contextAtCaret("method = 'it''s\\\\|'")?.partial).toBe("it's\\");
  });

  it("scans a long IN list in linear time", () => {
    const fastest = (count: number) => {
      const list = Array.from({ length: count }, (_, i) => `'host-${String(i).padStart(5, "0")}.example'`).join(", ");
      const text = `SELECT * FROM http WHERE host IN (${list}, '')`;
      let best = Infinity;
      for (let run = 0; run < 5; run++) {
        const started = performance.now();
        const cursor = getSQLCompletionCursor(text, text.length - 2, schema);
        best = Math.min(best, performance.now() - started);
        expect(cursor).toMatchObject({ kind: "value", field: "host", partial: "" });
      }
      return best;
    };
    // Four times the input costs about four times as long when linear and sixteen when quadratic.
    expect(fastest(8000) / fastest(2000)).toBeLessThan(8);
  });
});
