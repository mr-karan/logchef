import { SQL_KEYWORDS } from "./clickhouse-sql";

type SQLClause = "keyword" | "select-list" | "after-table" | "where" | "prewhere"
  | "having" | "group" | "group-by" | "order" | "order-by" | "limit";
export type SQLCompletionContext = "statement" | "table" | "expression" | "order-direction" | SQLClause;
export type SQLCompletionKind = "table" | "column" | "function" | "keyword" | "type";

export const SQL_COMPLETION_KEYWORDS = [...SQL_KEYWORDS, "GROUP BY", "ORDER BY"];

const priorities = {
  statement: { keyword: 1, column: 2, function: 3, table: 4, type: 5 },
  table: { table: 0, column: 1, function: 2, keyword: 3, type: 4 },
  expression: { column: 0, function: 1, keyword: 2, table: 3, type: 4 },
  keyword: { keyword: 0, column: 1, function: 2, table: 3, type: 4 },
} satisfies Record<string, Record<SQLCompletionKind, number>>;

const keywordPriorities: Record<SQLCompletionContext, readonly string[]> = {
  statement: ["SELECT"],
  table: [],
  expression: [],
  keyword: [],
  "select-list": ["FROM", "AS"],
  "after-table": ["WHERE", "PREWHERE", "GROUP BY", "ORDER BY", "LIMIT"],
  where: ["AND", "OR", "GROUP BY", "ORDER BY", "LIMIT"],
  prewhere: ["AND", "OR", "WHERE", "GROUP BY", "ORDER BY", "LIMIT"],
  having: ["AND", "OR", "ORDER BY", "LIMIT"],
  group: ["BY"],
  "group-by": ["HAVING", "ORDER BY", "LIMIT"],
  order: ["BY"],
  "order-by": ["DESC", "ASC", "LIMIT"],
  "order-direction": ["LIMIT"],
  limit: ["OFFSET", "FORMAT", "UNION"],
};

const clauseTokens = new Map<string, SQLClause>([
  ["SELECT", "select-list"], ["FROM", "after-table"], ["JOIN", "after-table"],
  ["WHERE", "where"], ["PREWHERE", "prewhere"], ["HAVING", "having"],
  ["GROUP", "group"], ["ORDER", "order"], ["LIMIT", "limit"],
]);

const expressionTokens = new Set([
  "SELECT", "WHERE", "PREWHERE", "HAVING", "ON", "AND", "OR", "BY", "AS",
  ",", "(", "=", "!=", "<>", "<", ">", "<=", ">=", "+", "-", "*", "/", "%",
]);

export function classifySQLCompletionContext(textBeforeWord: string): SQLCompletionContext {
  const tokens = textBeforeWord.matchAll(/--[^\r\n]*|\/\*[\s\S]*?(?:\*\/|$)|'(?:\\.|''|[^'\\])*(?:'|$)|"(?:\\.|""|[^"\\])*(?:"|$)|`(?:``|[^`])*(?:`|$)|[a-z_][a-z0-9_$]*|<>|[!<>=]=?|[^\s]/gi);
  let lastToken = "";
  let previousToken = "";
  let clause: SQLClause = "keyword";
  const enclosingClauses: SQLClause[] = [];
  for (const [token] of tokens) {
    if (token.startsWith("--") || token.startsWith("/*")) continue;
    previousToken = lastToken;
    lastToken = token.toUpperCase();

    if (lastToken === ";") {
      clause = "keyword";
      enclosingClauses.length = 0;
    } else if (lastToken === "(") {
      enclosingClauses.push(clause);
    } else if (lastToken === ")") {
      clause = enclosingClauses.pop() ?? clause;
    } else if (lastToken === "BY" && previousToken === "GROUP") {
      clause = "group-by";
    } else if (lastToken === "BY" && previousToken === "ORDER") {
      clause = "order-by";
    } else if (previousToken !== ".") {
      clause = clauseTokens.get(lastToken) ?? clause;
    }
  }

  if (!lastToken || lastToken === ";") return "statement";
  if (lastToken === "FROM" || lastToken === "JOIN") return "table";
  if (clause === "order-by" && (lastToken === "ASC" || lastToken === "DESC")) return "order-direction";
  if (clause === "select-list" && lastToken === "*" && ["SELECT", ",", "."].includes(previousToken)) return clause;
  if (expressionTokens.has(lastToken)) return "expression";
  return clause;
}

export function getSQLCompletionSortText(
  context: SQLCompletionContext,
  kind: SQLCompletionKind,
  label: string,
): string {
  const category = context === "statement" || context === "table" || context === "expression"
    ? context : "keyword";
  const priority = context === "statement" && kind === "keyword" && label === "SELECT"
    ? 0
    : priorities[category][kind];
  const preferredIndex = kind === "keyword" ? keywordPriorities[context].indexOf(label) : -1;
  const keywordIndex = kind === "keyword" ? SQL_COMPLETION_KEYWORDS.indexOf(label) : -1;
  const order = keywordIndex < 0 ? label.toLowerCase() : String(keywordIndex).padStart(3, "0");
  const preference = preferredIndex < 0 ? `1-${order}` : `0-${String(preferredIndex).padStart(3, "0")}`;
  return `${priority}-${preference}`;
}
