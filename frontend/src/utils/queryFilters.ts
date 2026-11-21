// Filter pills: field filters kept beside the query text, like Kibana's
// filter bar. A pill is language neutral, so the same pills apply to a
// LogchefQL query and to a native LogsQL query, and they join the query with
// "and" only when it runs.
import { appendCondition, buildCondition, formatValue, quoteValue, type FilterOperator } from '@/utils/logchefql/filters'
import { appendLogsqlFilter, buildLogsqlFilter, logsqlColumn, logsqlField } from '@/utils/logsql/filters'

export type QueryFilterOperator = 'is' | 'is_one_of' | 'contains' | 'exists' | 'gt' | 'gte' | 'lt' | 'lte'
export type QueryFilterValue = string | number | boolean

export const QUERY_FILTER_OPERATORS: readonly QueryFilterOperator[] = ['is', 'is_one_of', 'contains', 'exists', 'gt', 'gte', 'lt', 'lte']

export interface QueryFilter {
  /** A LogchefQL field path, as table cells and the Fields sidebar name fields. */
  field: string
  operator: QueryFilterOperator
  /** One value, several for is_one_of, none for exists. */
  values: QueryFilterValue[]
  /** Exclude the matching rows instead of keeping them. */
  negate?: boolean
  /** Kept in the bar but left out of the query. */
  disabled?: boolean
  label?: string
}

/** The languages pills translate to. */
export type FilterLanguage = 'logchefql' | 'logsql'

const RANGE: Record<'gt' | 'gte' | 'lt' | 'lte', FilterOperator> = { gt: '>', gte: '>=', lt: '<', lte: '<=' }
// LogchefQL has no "not", so an excluded range takes the opposite comparison.
const INVERTED_RANGE: Record<'gt' | 'gte' | 'lt' | 'lte', FilterOperator> = { gt: '<=', gte: '<', lt: '>=', lte: '>' }

export function operatorArity(operator: QueryFilterOperator): 'none' | 'one' | 'many' {
  if (operator === 'exists') return 'none'
  return operator === 'is_one_of' ? 'many' : 'one'
}

/** A filter has what its operator needs. */
export function isCompleteFilter(filter: QueryFilter): boolean {
  if (!filter.field.trim()) return false
  const arity = operatorArity(filter.operator)
  if (arity === 'none') return true
  if (arity === 'one') return filter.values.length === 1
  return filter.values.length > 0
}

/** The pill a table, header or sidebar action adds. */
export function filterFromAction(field: string, operator: FilterOperator, value: unknown): QueryFilter {
  const values = [toFilterValue(value)]
  switch (operator) {
    case '=': return { field, operator: 'is', values }
    case '!=': return { field, operator: 'is', values, negate: true }
    case '~': return { field, operator: 'contains', values }
    case '!~': return { field, operator: 'contains', values, negate: true }
    case '>': return { field, operator: 'gt', values }
    case '>=': return { field, operator: 'gte', values }
    case '<': return { field, operator: 'lt', values }
    default: return { field, operator: 'lte', values }
  }
}

export function toFilterValue(value: unknown): QueryFilterValue {
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return value
  if (value === null || value === undefined) return ''
  return JSON.stringify(value)
}

/** A value typed into the editor: digits compare as a number, like the Fields sidebar. */
export function parseTypedValue(text: string): QueryFilterValue {
  return /^-?\d+(\.\d+)?$/.test(text.trim()) ? Number(text.trim()) : text
}

export function filterToLogchefql(filter: QueryFilter): string {
  const { field, operator, values, negate } = filter
  switch (operator) {
    case 'is': return buildCondition(field, negate ? '!=' : '=', values[0])
    case 'contains': return buildCondition(field, negate ? '!~' : '~', values[0])
    case 'exists': return `${field}${negate ? '=' : '!='}""`
    case 'is_one_of': {
      if (negate) return values.map(value => buildCondition(field, '!=', value)).join(' and ')
      const terms = values.map(value => buildCondition(field, '=', value))
      return terms.length === 1 ? terms[0] : `(${terms.join(' or ')})`
    }
    default: return buildCondition(field, (negate ? INVERTED_RANGE : RANGE)[operator], values[0])
  }
}

export function filterToLogsql(filter: QueryFilter): string {
  const { field, operator, values, negate } = filter
  const not = negate ? 'NOT ' : ''
  switch (operator) {
    case 'is': return buildLogsqlFilter(field, negate ? '!=' : '=', values[0])
    case 'contains': return buildLogsqlFilter(field, negate ? '!~' : '~', values[0])
    case 'exists': return `${not}${logsqlField(field)}:*`
    case 'is_one_of': {
      const list = values.map(value => typeof value === 'string' ? quoteValue(value) : String(value)).join(',')
      return `${not}${logsqlField(field)}:in(${list})`
    }
    default: return `${not}${buildLogsqlFilter(field, RANGE[operator], values[0])}`
  }
}

export function filterExpression(filter: QueryFilter, language: FilterLanguage): string {
  return language === 'logsql' ? filterToLogsql(filter) : filterToLogchefql(filter)
}

/** The pills that join the query: enabled and complete. */
export function activeFilters(filters: readonly QueryFilter[] | undefined): QueryFilter[] {
  return (filters ?? []).filter(filter => !filter.disabled && isCompleteFilter(filter))
}

/**
 * The query with the pills joined to its filter part with "and", ahead of any
 * pipe. Disabled and incomplete pills are left out.
 */
export function applyFilters(query: string, filters: readonly QueryFilter[] | undefined, language: FilterLanguage): string {
  let combined = query
  for (const filter of activeFilters(filters)) {
    const expression = filterExpression(filter, language)
    combined = language === 'logsql' ? appendLogsqlFilter(combined, expression) : appendCondition(combined, expression)
  }
  return combined
}

/** The field name a pill shows: a quoted LogchefQL segment without its quotes. */
export function filterFieldLabel(field: string): string {
  return logsqlColumn(field)
}

export function formatFilterValue(value: QueryFilterValue): string {
  return typeof value === 'string' ? value : String(value)
}

/** Columns that an enabled pill filters, for the table header filter marks. */
export function filteredFields(filters: readonly QueryFilter[] | undefined): string[] {
  return [...new Set(activeFilters(filters).map(filter => filterFieldLabel(filter.field)))]
}

export function sameFilter(a: QueryFilter, b: QueryFilter): boolean {
  return a.field === b.field && a.operator === b.operator && !!a.negate === !!b.negate &&
    a.values.length === b.values.length && a.values.every((value, index) => value === b.values[index])
}

export function sameFilters(a: readonly QueryFilter[] | undefined, b: readonly QueryFilter[] | undefined): boolean {
  return encodeFilters(a ?? []) === encodeFilters(b ?? [])
}

function cleanFilter(filter: QueryFilter): QueryFilter {
  return {
    field: filter.field,
    operator: filter.operator,
    values: [...filter.values],
    ...(filter.negate ? { negate: true } : {}),
    ...(filter.disabled ? { disabled: true } : {}),
    ...(filter.label?.trim() ? { label: filter.label.trim() } : {}),
  }
}

/** The JSON a URL or a saved query holds. Empty when there are no pills. */
export function encodeFilters(filters: readonly QueryFilter[]): string {
  return filters.length ? JSON.stringify(filters.map(cleanFilter)) : ''
}

/** Reads pills from a URL, a draft or a saved query, dropping anything malformed. */
export function decodeFilters(raw: unknown): QueryFilter[] {
  let data = raw
  if (typeof raw === 'string') {
    if (!raw.trim()) return []
    try {
      data = JSON.parse(raw)
    } catch {
      return []
    }
  }
  if (!Array.isArray(data)) return []
  const filters: QueryFilter[] = []
  for (const item of data) {
    if (!item || typeof item !== 'object') continue
    const { field, operator, values, negate, disabled, label } = item as Record<string, unknown>
    if (typeof field !== 'string' || !field.trim()) continue
    if (typeof operator !== 'string' || !QUERY_FILTER_OPERATORS.includes(operator as QueryFilterOperator)) continue
    if (!Array.isArray(values) || !values.every(value => ['string', 'number', 'boolean'].includes(typeof value))) continue
    const filter = cleanFilter({
      field,
      operator: operator as QueryFilterOperator,
      values: values as QueryFilterValue[],
      negate: negate === true,
      disabled: disabled === true,
      label: typeof label === 'string' ? label : undefined,
    })
    if (isCompleteFilter(filter)) filters.push(filter)
  }
  return filters
}

/** A plain-text value for a preview, quoted like the query would quote it. */
export function previewValue(value: QueryFilterValue): string {
  return typeof value === 'string' ? value : formatValue(value)
}
