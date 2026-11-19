import { isAggregation } from './aggregation'

export type FilterOperator = '=' | '!=' | '~' | '!~' | '>' | '>=' | '<' | '<='

export const TEXT_OPERATORS: readonly FilterOperator[] = ['=', '!=', '~', '!~']
export const NUMBER_OPERATORS: readonly FilterOperator[] = ['=', '!=', '>', '>=', '<', '<=']

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

// Splits a filter expression at its top-level `and`s. With a top-level `or`
// no single term constrains every row, so there is nothing to report or remove.
function topLevelTerms(where: string): string[] | null {
  const terms: string[] = []
  let quote = ''
  let depth = 0
  let start = 0
  for (let i = 0; i < where.length; i++) {
    const char = where[i]
    if (quote) {
      if (char === '\\') i++
      else if (char === quote) quote = ''
      continue
    }
    if (char === '"' || char === "'") quote = char
    else if (char === '(') depth++
    else if (char === ')') depth--
    else if (depth === 0 && /[\s)]/.test(where[i - 1] ?? ' ')) {
      const keyword = /^(and|or)(?=[\s(])/i.exec(where.slice(i))?.[1]
      if (keyword?.toLowerCase() === 'or') return null
      if (keyword) {
        terms.push(where.slice(start, i).trim())
        start = i + keyword.length
      }
    }
  }
  terms.push(where.slice(start).trim())
  return terms.filter(Boolean)
}

const SEGMENT = String.raw`@?[a-zA-Z_][a-zA-Z0-9_:@-]*|"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'`
const COMPARISON_FIELD = new RegExp(String.raw`^((?:${SEGMENT})(?:\s*\.\s*(?:${SEGMENT}))*)\s*(?:!=|!~|>=|<=|[=~><])`)

function unquoteSegment(segment: string): string {
  if (segment.startsWith('"') || segment.startsWith("'")) return segment.slice(1, -1).replace(/\\(.)/g, '$1')
  return segment
}

// The top-level column a comparison term filters, or null for a group, a
// nested path, or anything else.
function termColumn(term: string): string | null {
  const path = COMPARISON_FIELD.exec(term)?.[1]
  if (!path) return null
  const segments = path.match(new RegExp(SEGMENT, 'g')) ?? []
  return segments.length === 1 ? unquoteSegment(segments[0]) : null
}

// Columns that a top-level `and` term of the query compares.
export function filteredColumns(query: string): string[] {
  const terms = topLevelTerms(splitPipe(query).where) ?? []
  return [...new Set(terms.map(termColumn).filter((column): column is string => column !== null))]
}

export function removeColumnConditions(query: string, column: string): string {
  const { where, select } = splitPipe(query)
  const terms = topLevelTerms(where)
  if (!terms) return query
  const kept = terms.filter(term => termColumn(term) !== column)
  if (kept.length === terms.length) return query
  return joinPipe(kept.join(' and '), select)
}

export function hasSelect(query: string): boolean {
  return splitPipe(query).select !== ''
}

// Adds a projected field. A query without a projection gets one listing the
// columns currently shown, so the new field joins them instead of replacing them.
// An aggregation has no projection to extend.
export function addSelectField(query: string, field: string, shownColumns: readonly string[]): string {
  if (isAggregation(query)) return query
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
