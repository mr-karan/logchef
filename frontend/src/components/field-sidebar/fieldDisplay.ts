import { Calendar, Database, Hash, Type } from 'lucide-vue-next'
import { isFilterableField, isPrimaryMessageField } from '@/lib/sourceFields'
import type { FieldValuesResult } from '@/api/sources'

export interface FieldInfo {
  name: string
  type: string
  isTimestamp?: boolean
  isSeverity?: boolean
}

// Type name without LowCardinality/Nullable wrappers, for display.
export const getCleanType = (type: string): string => {
  return type
    .replace(/LowCardinality\(([^)]+)\)/g, '$1')
    .replace(/Nullable\(([^)]+)\)/g, '$1')
}

export const getTypeIcon = (type: string) => {
  const cleanType = getCleanType(type).toLowerCase()
  if (cleanType.includes('datetime') || cleanType.includes('date')) {
    return Calendar
  }
  if (cleanType.includes('int') || cleanType.includes('float') || cleanType.includes('decimal')) {
    return Hash
  }
  if (cleanType.includes('map')) {
    return Database
  }
  return Type
}

export const getTypeColorClass = (field: FieldInfo): string => {
  if (field.isTimestamp) return 'text-blue-500'
  if (field.isSeverity) return 'text-amber-500'
  if (field.type.includes('LowCardinality')) return 'text-emerald-500'
  if (isFilterableField(field.type)) return 'text-sky-500'
  return 'text-muted-foreground'
}

export const formatCount = (count: number): string => {
  if (count >= 1000000) return `${(count / 1000000).toFixed(1)}M`
  if (count >= 1000) return `${(count / 1000).toFixed(1)}K`
  return count.toString()
}

// Past this many distinct values a top-values list is mostly fragments with tiny counts.
export const TOP_VALUES_DISTINCT_LIMIT = 1000

// Message fields are skipped without a query: every line is its own value.
export const skipsValueLoading = (field: Pick<FieldInfo, 'name'>): boolean => isPrimaryMessageField(field.name)

export const hasTooManyValues = (field: Pick<FieldInfo, 'name'>, values?: FieldValuesResult): boolean =>
  skipsValueLoading(field) || (values?.total_distinct ?? 0) > TOP_VALUES_DISTINCT_LIMIT
