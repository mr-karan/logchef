export type FilterOperator = '=' | '!=' | '~' | '!~'

// Values longer than this are filtered with "contains"; an exact match on a
// whole log line almost never matches anything else.
export const LONG_VALUE_LENGTH = 80
const CONTAINS_TERM_LENGTH = 60
const IDENT = /^@?[a-zA-Z_][a-zA-Z0-9_:@-]*$/

export function quoteValue(value: string): string {
  const escaped = value
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/\n/g, '\\n')
    .replace(/\r/g, '\\r')
    .replace(/\t/g, '\\t')
  return `"${escaped}"`
}

export function formatValue(value: unknown): string {
  if (value === null || value === undefined) return 'null'
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  if (typeof value === 'string') return quoteValue(value)
  return quoteValue(JSON.stringify(value))
}

export function fieldPath(segments: readonly string[]): string {
  return segments.map(segment => IDENT.test(segment) ? segment : quoteValue(segment)).join('.')
}

export function isLongValue(value: unknown): boolean {
  return typeof value === 'string' && (value.length > LONG_VALUE_LENGTH || value.includes('\n'))
}

// The term a "contains" filter uses: the user's selection when there is one,
// else the start of the first line, cut at a word boundary.
export function containsTerm(value: string, selection = ''): string {
  const selected = selection.trim()
  if (selected) return selected
  const line = value.trim().split('\n')[0]
  if (line.length <= CONTAINS_TERM_LENGTH) return line
  const cut = line.slice(0, CONTAINS_TERM_LENGTH)
  const boundary = cut.lastIndexOf(' ')
  return boundary > CONTAINS_TERM_LENGTH / 2 ? cut.slice(0, boundary) : cut
}

export function buildCondition(field: string, operator: FilterOperator, value: unknown): string {
  return `${field}${operator}${formatValue(value)}`
}

// Index of the first pipe outside a quoted string, or -1.
function pipeIndex(query: string): number {
  let quote = ''
  for (let i = 0; i < query.length; i++) {
    const char = query[i]
    if (quote) {
      if (char === '\\') i++
      else if (char === quote) quote = ''
    } else if (char === '"' || char === "'") {
      quote = char
    } else if (char === '|') {
      return i
    }
  }
  return -1
}

function splitPipe(query: string): { where: string; select: string } {
  const index = pipeIndex(query)
  if (index < 0) return { where: query.trim(), select: '' }
  return { where: query.slice(0, index).trim(), select: query.slice(index + 1).trim() }
}

function joinPipe(where: string, select: string): string {
  if (!select) return where
  return where ? `${where} | ${select}` : `| ${select}`
}

export function appendCondition(query: string, condition: string): string {
  const { where, select } = splitPipe(query)
  if (!where) return joinPipe(condition, select)
  // Extra parentheses are harmless; a missing pair changes the meaning.
  const grouped = /\bor\b/i.test(where) ? `(${where})` : where
  return joinPipe(`${grouped} and ${condition}`, select)
}

export function hasSelect(query: string): boolean {
  return splitPipe(query).select !== ''
}

// Adds a projected field. A query without a projection gets one listing the
// columns currently shown, so the new field joins them instead of replacing them.
export function addSelectField(query: string, field: string, shownColumns: readonly string[]): string {
  const { where, select } = splitPipe(query)
  const fields = select ? select.split(/\s+/) : [...shownColumns]
  if (!fields.includes(field)) fields.push(field)
  return joinPipe(where, fields.join(' '))
}

export interface FilterChoice {
  operator: FilterOperator
  value: unknown
}

// Long values and selected text filter by substring; anything else by exact value.
export function filterChoices(value: unknown, selection = ''): { include: FilterChoice; exclude: FilterChoice } {
  if (selection.trim() || isLongValue(value)) {
    const term = containsTerm(typeof value === 'string' ? value : JSON.stringify(value), selection)
    return { include: { operator: '~', value: term }, exclude: { operator: '!~', value: term } }
  }
  return { include: { operator: '=', value }, exclude: { operator: '!=', value } }
}
