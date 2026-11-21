import { filterFieldLabel, formatFilterValue, type QueryFilter, type QueryFilterOperator } from '@/utils/queryFilters'

type Translate = (key: string, values?: Record<string, unknown>) => string

const POSITIVE: Record<QueryFilterOperator, string> = {
  is: 'filters.op.is',
  is_one_of: 'filters.op.isOneOf',
  contains: 'filters.op.contains',
  exists: 'filters.op.exists',
  gt: 'filters.op.gt',
  gte: 'filters.op.gte',
  lt: 'filters.op.lt',
  lte: 'filters.op.lte',
}

const NEGATIVE: Record<QueryFilterOperator, string> = {
  is: 'filters.op.isNot',
  is_one_of: 'filters.op.isNotOneOf',
  contains: 'filters.op.notContains',
  exists: 'filters.op.notExists',
  // An excluded range reads as the opposite range.
  gt: 'filters.op.lte',
  gte: 'filters.op.lt',
  lt: 'filters.op.gte',
  lte: 'filters.op.gt',
}

export function filterOperatorLabel(t: Translate, operator: QueryFilterOperator, negate = false): string {
  return t((negate ? NEGATIVE : POSITIVE)[operator])
}

/** What a pill shows after its field: "us-central1", "is one of a, b", "exists". */
export function filterValueLabel(t: Translate, filter: QueryFilter): string {
  const values = filter.values.map(formatFilterValue)
  if (filter.operator === 'is') return values[0] ?? ''
  if (filter.operator === 'exists') return filterOperatorLabel(t, 'exists', filter.negate)
  return `${filterOperatorLabel(t, filter.operator, filter.negate)} ${values.join(', ')}`
}

/** The whole pill text, for its title and screen readers. */
export function filterTitle(t: Translate, filter: QueryFilter): string {
  const field = filterFieldLabel(filter.field)
  if (filter.operator === 'is') return `${field} ${filterOperatorLabel(t, 'is', filter.negate)} ${filterValueLabel(t, filter)}`
  return `${field}: ${filterValueLabel(t, filter)}`
}
