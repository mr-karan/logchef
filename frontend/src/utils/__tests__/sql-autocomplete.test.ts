import { describe, expect, it } from "vitest";
import {
  SQL_COMPLETION_KEYWORDS,
  classifySQLCompletionContext,
  getSQLCompletionSortText,
  type SQLCompletionKind,
} from "../sql-autocomplete";
import { SQL_KEYWORDS } from "../clickhouse-sql";

describe("classifySQLCompletionContext", () => {
  it.each(["", " \n\t", "SELECT method FROM http;\n", "-- start a query\n"])(
    "recognizes a statement start after %j",
    (text) => expect(classifySQLCompletionContext(text)).toBe("statement"),
  );

  it.each(["SELECT * FROM ", "select * from\n", "SELECT * FROM /* logs */ ", "SELECT * FROM http JOIN "])(
    "expects a table after %j",
    (text) => expect(classifySQLCompletionContext(text)).toBe("table"),
  );

  it.each([
    "SELECT ",
    "SELECT * FROM http WHERE ",
    "SELECT * FROM http WHERE status = 200 AND ",
    "SELECT * FROM http WHERE status = 200 OR ",
    "SELECT * FROM http ORDER BY ",
    "SELECT * FROM http GROUP BY ",
    "SELECT * FROM http PREWHERE ",
    "SELECT method FROM http GROUP BY method HAVING ",
    "SELECT * FROM http WHERE method IN (",
    "SELECT method AS ",
    "SELECT method + ",
    "SELECT method * ",
    "SELECT count(*) FROM http WHERE status = 200 AND ",
    "SELECT method, ",
    "SELECT count(",
    "SELECT * FROM http WHERE isNotNull(\n",
    "SELECT * FROM http WHERE (",
    "SELECT * FROM http WHERE status = ",
    "SELECT * FROM http WHERE status != ",
    "SELECT * FROM http WHERE status <> ",
    "SELECT * FROM http WHERE status < ",
    "SELECT * FROM http WHERE status > ",
    "SELECT * FROM http WHERE status <= ",
    "SELECT * FROM http WHERE status >= ",
    "SELECT * FROM http WHERE -- FROM is not a clause here\n",
    "SELECT * FROM http WHERE /* FROM */ ",
  ])("expects an expression after %j", (text) => {
    expect(classifySQLCompletionContext(text)).toBe("expression");
  });

  it.each([
    { text: "SELECT method ", context: "select-list" },
    { text: "SELECT * ", context: "select-list" },
    { text: "SELECT count() ", context: "select-list" },
    { text: 'SELECT "FROM" ', context: "select-list" },
    { text: "SELECT `WHERE` ", context: "select-list" },
    { text: "SELECT method AS verb ", context: "select-list" },
    { text: "SELECT * FROM default.http ", context: "after-table" },
    { text: "SELECT * FROM http AS logs ", context: "after-table" },
    { text: "SELECT * FROM http WHERE method = 'FROM' ", context: "where" },
    { text: "SELECT * FROM http WHERE method = 'it''s FROM' ", context: "where" },
    { text: "SELECT * FROM http WHERE (status >= 400) ", context: "where" },
    { text: "SELECT * FROM http PREWHERE status = 200 ", context: "prewhere" },
    { text: "SELECT method FROM http GROUP BY method HAVING count() > 1 ", context: "having" },
    { text: "SELECT method FROM http GROUP BY method ", context: "group-by" },
    { text: "SELECT * FROM http ORDER BY timestamp ", context: "order-by" },
    { text: "SELECT * FROM http ORDER BY timestamp DESC ", context: "order-direction" },
    { text: "SELECT * FROM http ORDER BY timestamp ASC ", context: "order-direction" },
    { text: "SELECT * FROM http LIMIT 10 ", context: "limit" },
    { text: "SELECT * FROM http GROUP ", context: "group" },
    { text: "SELECT * FROM http ORDER ", context: "order" },
    { text: "SELECT * FROM (SELECT * FROM http WHERE status = 200) ", context: "after-table" },
    { text: "SELECT * FROM http WHERE id IN (SELECT id FROM http GROUP BY id) ", context: "where" },
    { text: "SELECT * FROM http WHERE id IN (SELECT id ", context: "select-list" },
    { text: "select method from http where status = 200 /* ORDER BY */ ", context: "where" },
    { text: "SELECT * FROM http ORDER BY timestamp; SELECT method ", context: "select-list" },
  ])("recognizes $context in $text", ({ text, context }) => {
    expect(classifySQLCompletionContext(text)).toBe(context);
  });
});

describe("getSQLCompletionSortText", () => {
  it("prefers a common clause over a format keyword after a selected column", () => {
    const context = classifySQLCompletionContext("SELECT method ");
    const ranked = ["FORMAT", "FROM"].sort((a, b) =>
      getSQLCompletionSortText(context, "keyword", a).localeCompare(
        getSQLCompletionSortText(context, "keyword", b),
      ),
    );
    expect(ranked).toEqual(["FROM", "FORMAT"]);
  });

  const items: { kind: SQLCompletionKind; label: string }[] = [
    { kind: "keyword", label: "WHERE" },
    { kind: "type", label: "String" },
    { kind: "function", label: "count" },
    { kind: "column", label: "method" },
    { kind: "table", label: "http" },
    { kind: "keyword", label: "SELECT" },
  ];

  it.each([
    { text: "", labels: ["SELECT", "WHERE", "method", "count", "http", "String"] },
    { text: "SELECT * FROM ", labels: ["http", "method", "count", "SELECT", "WHERE", "String"] },
    { text: "SELECT ", labels: ["method", "count", "SELECT", "WHERE", "http", "String"] },
    { text: "SELECT * FROM http WHERE ", labels: ["method", "count", "SELECT", "WHERE", "http", "String"] },
    { text: "SELECT * FROM http ORDER BY ", labels: ["method", "count", "SELECT", "WHERE", "http", "String"] },
    { text: "SELECT count(", labels: ["method", "count", "SELECT", "WHERE", "http", "String"] },
  ])("ranks every candidate for %j", ({ text, labels }) => {
    const context = classifySQLCompletionContext(text);
    const ranked = [...items].sort((a, b) =>
      getSQLCompletionSortText(context, a.kind, a.label).localeCompare(
        getSQLCompletionSortText(context, b.kind, b.label),
      ),
    );
    expect(ranked.map((item) => item.label)).toEqual(labels);
  });
});

describe("SQL clause keyword ranking", () => {
  it.each([
    { text: "SELECT method ", keywords: ["FROM", "AS"] },
    { text: "SELECT * FROM default.http ", keywords: ["WHERE", "PREWHERE", "GROUP BY", "ORDER BY", "LIMIT"] },
    { text: "SELECT * FROM http WHERE method = 'GET' ", keywords: ["AND", "OR", "GROUP BY", "ORDER BY", "LIMIT"] },
    { text: "SELECT * FROM http PREWHERE method = 'GET' ", keywords: ["AND", "OR", "WHERE", "GROUP BY", "ORDER BY", "LIMIT"] },
    { text: "SELECT method FROM http GROUP BY method ", keywords: ["HAVING", "ORDER BY", "LIMIT"] },
    { text: "SELECT method FROM http GROUP BY method HAVING count() > 1 ", keywords: ["AND", "OR", "ORDER BY", "LIMIT"] },
    { text: "SELECT * FROM http ORDER BY timestamp ", keywords: ["DESC", "ASC", "LIMIT"] },
    { text: "SELECT * FROM http ORDER BY timestamp ASC ", keywords: ["LIMIT"] },
    { text: "SELECT * FROM http ORDER BY timestamp DESC ", keywords: ["LIMIT"] },
    { text: "SELECT * FROM http LIMIT 10 ", keywords: ["OFFSET", "FORMAT", "UNION"] },
    { text: "SELECT * FROM http GROUP ", keywords: ["BY"] },
    { text: "SELECT * FROM http ORDER ", keywords: ["BY"] },
  ])("prefers legal continuations in $text without dropping keywords", ({ text, keywords }) => {
    const context = classifySQLCompletionContext(text);
    const ranked = [...SQL_COMPLETION_KEYWORDS].sort((a, b) =>
      getSQLCompletionSortText(context, "keyword", a).localeCompare(
        getSQLCompletionSortText(context, "keyword", b),
      ),
    );
    expect(ranked.slice(0, keywords.length)).toEqual(keywords);
    expect(new Set(ranked)).toEqual(new Set([...SQL_KEYWORDS, "GROUP BY", "ORDER BY"]));
    expect(ranked).toHaveLength(SQL_KEYWORDS.length + 2);
  });
});
