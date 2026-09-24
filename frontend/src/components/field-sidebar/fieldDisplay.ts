import { Calendar, Database, Hash, Type } from 'lucide-vue-next'
import { isFilterableField } from '@/lib/sourceFields'

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
