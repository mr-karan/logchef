// Header filters narrow the rows already loaded in the table. They never
// change the query, so they filter this page only.
import type { FilterOperator } from '@/utils/logchefql/filters'

export interface PageFilter {
  column: string
  operator: FilterOperator
  value: string | number
}

function cellText(value: unknown): string {
  if (value === null || value === undefined) return ''
  return typeof value === 'object' ? JSON.stringify(value) : String(value)
}

function cellNumber(value: unknown): number | null {
  if (value === null || value === undefined || value === '' || typeof value === 'object') return null
  const number = Number(value)
  return Number.isFinite(number) ? number : null
}

/** Text compares exactly for = and !=, and ignores case for contains. */
export function matchesPageFilter(row: Record<string, unknown>, filter: PageFilter): boolean {
  const value = row[filter.column]
  const { operator } = filter
  if (typeof filter.value === 'number') {
    const number = cellNumber(value)
    if (operator === '!=') return number !== filter.value
    if (number === null) return false
    switch (operator) {
      case '=': return number === filter.value
      case '>': return number > filter.value
      case '>=': return number >= filter.value
      case '<': return number < filter.value
      case '<=': return number <= filter.value
      default: break
    }
  }
  const text = cellText(value)
  const wanted = String(filter.value)
  switch (operator) {
    case '=': return text === wanted
    case '!=': return text !== wanted
    case '~': return text.toLowerCase().includes(wanted.toLowerCase())
    case '!~': return !text.toLowerCase().includes(wanted.toLowerCase())
    default: return false
  }
}

/** The rows every filter keeps. */
export function applyPageFilters<T extends Record<string, unknown>>(rows: readonly T[], filters: readonly PageFilter[]): T[] {
  if (filters.length === 0) return rows as T[]
  return rows.filter(row => filters.every(filter => matchesPageFilter(row, filter)))
}

type Translate = (key: string) => string

export function pageFilterOperatorLabel(t: Translate, operator: FilterOperator, numeric: boolean): string {
  if (numeric) return operator
  switch (operator) {
    case '=': return t('explore.columnFilterEquals')
    case '!=': return t('explore.columnFilterNotEquals')
    case '~': return t('explore.columnFilterContains')
    case '!~': return t('explore.columnFilterNotContains')
    default: return operator
  }
}

export function pageFilterLabel(t: Translate, filter: PageFilter): string {
  return `${filter.column} ${pageFilterOperatorLabel(t, filter.operator, typeof filter.value === 'number')} ${filter.value}`
}
