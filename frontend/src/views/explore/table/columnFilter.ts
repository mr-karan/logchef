// Client-side column filter predicate for the explore data table.
//
// Filters only the currently loaded result page (no server round-trip).
// Baseline behaviour is a case-insensitive "contains" match against the
// cell's text representation. If the typed filter looks like a numeric
// comparison (e.g. ">= 500", "!= 3", "42") and the cell's value is itself
// numeric, a numeric comparison is used instead.

import type { Row } from "@tanstack/vue-table";

export type ColumnFilterOperator = ">" | ">=" | "<" | "<=" | "=" | "!=";

export interface ParsedNumericFilter {
  operator: ColumnFilterOperator;
  value: number;
}

// Longer operators (>=, <=, !=) must be listed before their single-character
// prefixes (>, <) so the regex engine matches the full operator first.
const NUMERIC_FILTER_RE = /^(>=|<=|!=|=|>|<)?\s*(-?\d+(?:\.\d+)?)\s*$/;

/**
 * Parses a filter string as a numeric comparison, e.g. ">= 500" -> { operator: '>=', value: 500 }.
 * A bare number (e.g. "42") defaults to the "=" operator.
 * Returns null if the string isn't a numeric comparison expression.
 */
export function parseNumericFilter(input: string): ParsedNumericFilter | null {
  const match = NUMERIC_FILTER_RE.exec(input.trim());
  if (!match) return null;

  const operator = (match[1] as ColumnFilterOperator) || "=";
  const value = Number(match[2]);
  if (Number.isNaN(value)) return null;

  return { operator, value };
}

function toComparableNumber(cellValue: unknown): number | null {
  if (cellValue === null || cellValue === undefined || cellValue === "") return null;
  if (typeof cellValue === "boolean") return null;
  const num = typeof cellValue === "number" ? cellValue : Number(cellValue);
  return Number.isNaN(num) ? null : num;
}

function toComparableText(cellValue: unknown): string {
  if (cellValue === null || cellValue === undefined) return "";
  if (typeof cellValue === "object") {
    try {
      return JSON.stringify(cellValue);
    } catch {
      return String(cellValue);
    }
  }
  return String(cellValue);
}

/**
 * Returns true if `cellValue` matches the (already-trimmed, non-empty) `filterValue`.
 * An empty/whitespace-only filterValue always matches (no-op filter).
 */
export function matchesColumnFilter(cellValue: unknown, filterValue: string): boolean {
  const trimmed = filterValue.trim();
  if (!trimmed) return true;

  const numericFilter = parseNumericFilter(trimmed);
  if (numericFilter) {
    const cellNumber = toComparableNumber(cellValue);
    if (cellNumber !== null) {
      switch (numericFilter.operator) {
        case ">":
          return cellNumber > numericFilter.value;
        case ">=":
          return cellNumber >= numericFilter.value;
        case "<":
          return cellNumber < numericFilter.value;
        case "<=":
          return cellNumber <= numericFilter.value;
        case "!=":
          return cellNumber !== numericFilter.value;
        case "=":
          return cellNumber === numericFilter.value;
      }
    }
    // Cell isn't numeric (e.g. a text column that happens to contain digits) -
    // fall through to a plain text contains-match below.
  }

  return toComparableText(cellValue).toLowerCase().includes(trimmed.toLowerCase());
}

/**
 * TanStack Table `filterFn` wired onto every column in columns.ts.
 * Filtering across multiple columns combines with AND semantics for free,
 * since getFilteredRowModel() applies each active column filter in sequence.
 */
export function columnFilterFn(row: Row<Record<string, any>>, columnId: string, filterValue: string): boolean {
  return matchesColumnFilter(row.getValue(columnId), filterValue);
}
