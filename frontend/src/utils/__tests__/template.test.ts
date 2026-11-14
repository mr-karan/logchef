import { describe, expect, it } from "vitest";
import { BUILTIN_MACROS, extractTemplateVariables, isBuiltinMacro, templateMacros } from "../template";

// The frontend mirrors the macro contract of RFC 0003 where it reads a
// template: which names are variables to ask for, and which macros a query uses.
describe("built-in macros", () => {
  it("knows the closed set of names", () => {
    expect([...BUILTIN_MACROS].sort()).toEqual(["__from", "__interval", "__interval_s", "__time_filter", "__timezone", "__to", "__tz_offset"]);
    expect(isBuiltinMacro("__from")).toBe(true);
    expect(isBuiltinMacro("__tenant")).toBe(false);
    expect(isBuiltinMacro("from")).toBe(false);
  });

  // Rule 3: a macro is never a request variable, so the UI must not ask for one.
  it("does not list a macro as a variable", () => {
    const sql = "SELECT toStartOfInterval(ts, {{__interval}}, {{__timezone}}) AS t FROM logs WHERE {{__time_filter}} AND service = {{service}} AND ts < {{ __to }}";
    expect(extractTemplateVariables(sql, "clickhouse-sql")).toEqual([{ name: "service", isOptional: false }]);
    expect(extractTemplateVariables("_time:[{{__from}}, {{__to}}) | stats by (_time:{{__interval}} offset {{__tz_offset}}) count()", "logsql")).toEqual([]);
  });

  it("still lists a stored variable that has the prefix and is not a macro", () => {
    expect(extractTemplateVariables("SELECT * FROM logs WHERE t = {{__tenant}}", "clickhouse-sql")).toEqual([{ name: "__tenant", isOptional: false }]);
  });

  // Rule 6: a macro counts as provided, so it does not make a block's variable required.
  it("keeps a variable optional in a block that also holds a macro", () => {
    expect(extractTemplateVariables("SELECT 1 FROM t WHERE 1 [[ AND ts >= {{__from}} AND c = {{customer}} ]]", "clickhouse-sql")).toEqual([{ name: "customer", isOptional: true }]);
    expect(extractTemplateVariables("SELECT 1 FROM t WHERE 1 [[ AND {{__time_filter}} ]]", "clickhouse-sql")).toEqual([]);
  });

  it("reports the macros a query uses", () => {
    const macros = templateMacros("SELECT count() FROM logs WHERE {{__time_filter}} AND bucket = {{ __interval_s }}", "clickhouse-sql");
    expect([...macros].sort()).toEqual(["__interval_s", "__time_filter"]);
    expect([...templateMacros("SELECT {{service}} FROM logs", "clickhouse-sql")]).toEqual([]);
  });

  // Rule 2: the server expands a macro only in an expression.
  it.each([
    ["a line comment", "SELECT 1 -- {{__time_filter}}\nFROM logs", "clickhouse-sql"],
    ["a block comment", "SELECT 1 /* {{__from}} /* {{__to}} */ */ FROM logs", "clickhouse-sql"],
    ["a string", "SELECT '{{__from}}' FROM logs", "clickhouse-sql"],
    ["a quoted identifier", "SELECT `{{__interval}}`, \"{{__timezone}}\" FROM logs", "clickhouse-sql"],
    ["a heredoc", "SELECT $tag${{__to}}$tag$ FROM logs", "clickhouse-sql"],
    ["a LogsQL hash comment", "error # {{__from}}", "logsql"],
    ["a LogsQL raw string", "msg:`{{__tz_offset}}`", "logsql"],
  ] as const)("ignores a macro inside %s", (_name, query, dialect) => {
    expect(templateMacros(query, dialect).size).toBe(0);
  });
});
