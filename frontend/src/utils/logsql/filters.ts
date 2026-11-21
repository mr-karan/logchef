// Builds VictoriaLogs LogsQL filters from table and row actions, the native
// counterpart of utils/logchefql/filters. It edits query text and does not
// validate it: VictoriaLogs reports a malformed query when it runs.
import { quoteValue, type FilterOperator } from '@/utils/logchefql/filters'

const BARE_FIELD = /^[a-zA-Z_][a-zA-Z0-9_.]*$/
const REGEX_SPECIAL = /[\\.+*?()|[\]{}^$]/g

// Table cells name a column as is; the expanded row names it as a LogchefQL
// path, which quotes names such as "http.status". VictoriaLogs stores nested
// JSON flattened, so a single quoted segment is the column name.
export function logsqlColumn(field: string): string {
  const quoted = /^"((?:[^"\\]|\\.)*)"$/.exec(field)
  return quoted ? quoted[1].replace(/\\(.)/g, '$1') : field
}

export function logsqlField(field: string): string {
  const name = logsqlColumn(field)
  return BARE_FIELD.test(name) ? name : quoteValue(name)
}

function exactValue(value: unknown): string {
  if (value === null || value === undefined) return '""'
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return quoteValue(typeof value === 'string' ? value : JSON.stringify(value))
}

function textValue(value: unknown): string {
  if (value === null || value === undefined) return ''
  return typeof value === 'string' ? value : JSON.stringify(value)
}

export function escapeRegex(text: string): string {
  return text.replace(REGEX_SPECIAL, '\\$&')
}

// `=` is an exact match, `~` matches the literal text anywhere in the value,
// and the comparisons are LogsQL range filters. A missing field reads as "".
export function buildLogsqlFilter(field: string, operator: FilterOperator, value: unknown): string {
  const name = logsqlField(field)
  switch (operator) {
    case '=': return `${name}:=${exactValue(value)}`
    case '!=': return `NOT ${name}:=${exactValue(value)}`
    case '~': return `${name}:~${quoteValue(escapeRegex(textValue(value)))}`
    case '!~': return `NOT ${name}:~${quoteValue(escapeRegex(textValue(value)))}`
    default: return `${name}:${operator}${exactValue(value)}`
  }
}

// Index of the first pipe outside a quoted string, or -1.
function pipeIndex(query: string): number {
  let quote = ''
  for (let i = 0; i < query.length; i++) {
    const char = query[i]
    if (quote) {
      if (char === '\\') i++
      else if (char === quote) quote = ''
    } else if (char === '"' || char === "'" || char === '`') {
      quote = char
    } else if (char === '|') {
      return i
    }
  }
  return -1
}

// True when `or` appears outside quotes and parentheses.
function hasTopLevelOr(filter: string): boolean {
  let quote = ''
  let depth = 0
  for (let i = 0; i < filter.length; i++) {
    const char = filter[i]
    if (quote) {
      if (char === '\\') i++
      else if (char === quote) quote = ''
    } else if (char === '"' || char === "'" || char === '`') {
      quote = char
    } else if (char === '(') {
      depth++
    } else if (char === ')') {
      depth--
    } else if (depth === 0 && /\s/.test(filter[i - 1] ?? ' ') && /^or(?=[\s(])/i.test(filter.slice(i))) {
      return true
    }
  }
  return false
}

// Adds a filter to the query's filter expression, ahead of any pipes. LogsQL
// joins filters with whitespace as "and"; `*` alone matches everything.
export function appendLogsqlFilter(query: string, filter: string): string {
  const index = pipeIndex(query)
  const where = (index < 0 ? query : query.slice(0, index)).trim()
  const pipes = index < 0 ? '' : query.slice(index).trim()
  let combined = filter
  if (where && where !== '*') combined = `${hasTopLevelOr(where) ? `(${where})` : where} ${filter}`
  return pipes ? `${combined} ${pipes}` : combined
}

// The query's filter expression split at top-level whitespace, with a `NOT`
// joined to the term it negates. Null when a top-level `or` makes removing a
// term change the meaning of the rest.
function topLevelTerms(where: string): string[] | null {
  if (hasTopLevelOr(where)) return null
  const terms: string[] = []
  let quote = ''
  let depth = 0
  let current = ''
  for (let i = 0; i < where.length; i++) {
    const char = where[i]
    if (quote) {
      current += char
      if (char === '\\' && i + 1 < where.length) current += where[++i]
      else if (char === quote) quote = ''
      continue
    }
    if (char === '"' || char === "'" || char === '`') quote = char
    else if (char === '(') depth++
    else if (char === ')') depth--
    if (depth === 0 && /\s/.test(char)) {
      if (current) terms.push(current)
      current = ''
      continue
    }
    current += char
  }
  if (current) terms.push(current)
  const joined: string[] = []
  for (const term of terms) {
    const previous = joined.at(-1)
    if (previous && /^(?:not|and)$/i.test(previous) && !/^and$/i.test(term)) joined[joined.length - 1] = `${previous} ${term}`
    else joined.push(term)
  }
  return joined.map(term => term.replace(/^and\s+/i, '')).filter(term => !/^and$/i.test(term))
}

const TERM_FIELD = /^(?:"((?:[^"\\]|\\.)*)"|([^\s:()"'`!-][^\s:()"'`]*)):/

// The column a filter term filters on, or null for a word or a group.
function termColumn(term: string): string | null {
  const bare = term.replace(/^not\s+/i, '').replace(/^[!-]/, '')
  const match = TERM_FIELD.exec(bare)
  if (!match) return null
  return match[1] !== undefined ? match[1].replace(/\\(.)/g, '$1') : match[2]
}

function splitQuery(query: string): { where: string; pipes: string } {
  const index = pipeIndex(query)
  return index < 0 ? { where: query.trim(), pipes: '' } : { where: query.slice(0, index).trim(), pipes: query.slice(index).trim() }
}

// Columns with a top-level filter in the query, for the table header filters.
export function logsqlFilteredColumns(query: string): string[] {
  const terms = topLevelTerms(splitQuery(query).where) ?? []
  return [...new Set(terms.map(termColumn).filter((column): column is string => column !== null && column !== '_time'))]
}

// Drops the top-level filters on a column; `*` stands in for an emptied filter.
export function removeLogsqlColumnFilters(query: string, column: string): string {
  const { where, pipes } = splitQuery(query)
  const terms = topLevelTerms(where)
  if (!terms) return query
  const kept = terms.filter(term => termColumn(term) !== column)
  if (kept.length === terms.length) return query
  const filter = kept.join(' ') || '*'
  return pipes ? `${filter} ${pipes}` : filter
}
