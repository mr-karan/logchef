import { toRaw } from 'vue'

// Fields with a value in at least one row. A ClickHouse table returns every
// column on every row, so an empty or null value does not count as present.
export function fieldsWithValues(rows: readonly Record<string, unknown>[]): Set<string> {
  const present = new Set<string>()
  for (const row of toRaw(rows)) {
    for (const [key, value] of Object.entries(toRaw(row))) {
      if (present.has(key) || value === null || value === undefined || value === '') continue
      present.add(key)
    }
  }
  return present
}

const IDENT_START = /[A-Za-z_@]/
const IDENT_CHAR = /[\w.@-]/

// Field names a LogsQL query filters on: a bare or quoted name directly
// before a colon, outside quoted values. Pipes are scanned too, so
// `| filter status:500` counts. The caller keeps only names the source knows,
// which drops a stray match such as the hour in an unquoted `12:30`.
export function logsqlFilterFields(query: string): string[] {
  const found = new Set<string>()
  let i = 0
  while (i < query.length) {
    const char = query[i]
    if (char === '"' || char === "'" || char === '`') {
      let j = i + 1
      let text = ''
      while (j < query.length && query[j] !== char) {
        if (query[j] === '\\' && j + 1 < query.length) j++
        text += query[j]
        j++
      }
      if (query[j + 1] === ':' && text) found.add(text)
      i = j + 1
      continue
    }
    if (IDENT_START.test(char) && !/[\w.@]/.test(query[i - 1] ?? ' ')) {
      let j = i
      while (j < query.length && IDENT_CHAR.test(query[j])) j++
      if (query[j] === ':') found.add(query.slice(i, j))
      i = j
      continue
    }
    i++
  }
  return [...found]
}
