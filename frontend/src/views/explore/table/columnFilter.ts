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

// Only a plain, unambiguous decimal shape counts as "numeric" for a string
// cell - deliberately stricter than JS's `Number()` coercion, which also
// accepts things no user typing a numeric filter would mean:
//   - "" / "   "        -> Number() gives 0
//   - "0x10"             -> Number() gives 16 (hex literal)
//   - "1e3"              -> Number() gives 1000 (exponential notation)
//   - "Infinity"/"NaN"   -> Number() gives Infinity / NaN
// None of those should silently make a text cell match a numeric filter.
const NUMERIC_STRING_RE = /^-?\d+(?:\.\d+)?$/;

function toComparableNumber(cellValue: unknown): number | null {
  if (typeof cellValue === "number") {
    return Number.isNaN(cellValue) ? null : cellValue;
  }
  if (typeof cellValue === "string") {
    const trimmed = cellValue.trim();
    if (!NUMERIC_STRING_RE.test(trimmed)) return null;
    return Number(trimmed);
  }
  return null;
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

    // Cell isn't numeric (e.g. a text column that happens to contain digits).
    // Fall back to a text match against the bare numeric value - NOT the
    // full `trimmed` filter string, which would still carry the operator
    // prefix (e.g. "!=500") and could never literally appear in real data.
    // Ordering comparisons (>, >=, <, <=) have no meaningful text analog, so
    // they simply don't match a non-numeric cell. "=" keeps the previous
    // plain contains-match behaviour; "!=" is handled explicitly as its
    // negation instead of silently falling through to a literal (and always
    // failing) contains-match on "!=<value>".
    const valueText = String(numericFilter.value).toLowerCase();
    const cellText = toComparableText(cellValue).toLowerCase();
    switch (numericFilter.operator) {
      case "=":
        return cellText.includes(valueText);
      case "!=":
        return !cellText.includes(valueText);
      default:
        return false;
    }
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
