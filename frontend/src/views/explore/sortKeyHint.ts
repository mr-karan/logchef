export type QueryMode = "logchefql" | "native";

// Expression keys such as toYYYYMMDD(ts) are not fields a user can filter on.
const PLAIN_FIELD = /^[A-Za-z_][\w.]*$/;

function escapeRegExp(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Field names inside quoted values must not count as filters.
const QUOTED = /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g;

function filtersOn(rawQuery: string, mode: QueryMode, field: string): boolean {
  const query = rawQuery.replace(QUOTED, '""');
  const name = escapeRegExp(field);
  if (mode === "logchefql") {
    return new RegExp(`(^|[^\\w.])${name}\\s*(=|!=|=~|!~|~|>=|<=|>|<)`, "i").test(query);
  }
  const where = query.search(/\bwhere\b/i);
  return where >= 0 && new RegExp(`(^|[^\\w.])\`?${name}\`?(?![\\w])`, "i").test(query.slice(where));
}

/** Sort-key fields worth suggesting: none while the query already filters on one, else all plain non-timestamp keys. */
export function unfilteredSortKeys(query: string, mode: QueryMode, sortKeys: readonly string[], timestampField: string | undefined): string[] {
  const fields = sortKeys.filter((key) => key !== timestampField && PLAIN_FIELD.test(key));
  return fields.some((field) => filtersOn(query, mode, field)) ? [] : fields;
}
