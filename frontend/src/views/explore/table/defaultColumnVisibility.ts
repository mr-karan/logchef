import type { ColumnDef } from '@tanstack/vue-table'
import type { Source } from '@/api/sources'
import {
  MESSAGE_FIELD_ALIASES,
  normalizeFieldName,
  isContextFieldName,
  isSystemField,
} from '@/lib/sourceFields'
const MAX_VICTORIALOGS_VISIBLE_COLUMNS = 6

function getColumnIds(columns: ColumnDef<Record<string, any>>[]): string[] {
  return columns.map(column => column.id).filter((id): id is string => Boolean(id))
}

function addFirstMatchingColumn(
  columnIds: string[],
  visible: Set<string>,
  aliases: readonly string[],
): void {
  for (const alias of aliases) {
    const match = columnIds.find(columnId => {
      return !visible.has(columnId) && normalizeFieldName(columnId) === alias
    })

    if (match) {
      visible.add(match)
      return
    }
  }
}

export function getDefaultColumnVisibility(
  columns: ColumnDef<Record<string, any>>[],
  source: Pick<Source, 'source_type'> | null | undefined,
  timestampField?: string,
  severityField?: string,
): Record<string, boolean> {
  const columnIds = getColumnIds(columns)
  const defaultVisibility = Object.fromEntries(columnIds.map(columnId => [columnId, true]))

  if (source?.source_type !== 'victorialogs') {
    return defaultVisibility
  }

  const nonInternalIds = columnIds.filter(columnId => !isSystemField(source, columnId))
  const visibleColumnIds = new Set<string>()

  if (timestampField && nonInternalIds.includes(timestampField)) {
    visibleColumnIds.add(timestampField)
  }

  if (severityField && nonInternalIds.includes(severityField)) {
    visibleColumnIds.add(severityField)
  }

  addFirstMatchingColumn(nonInternalIds, visibleColumnIds, MESSAGE_FIELD_ALIASES)

  if (nonInternalIds.length <= MAX_VICTORIALOGS_VISIBLE_COLUMNS) {
    nonInternalIds.forEach(columnId => visibleColumnIds.add(columnId))
  } else {
    const contextIds = nonInternalIds.filter(columnId => isContextFieldName(columnId))
    for (const columnId of contextIds) {
      if (visibleColumnIds.size >= MAX_VICTORIALOGS_VISIBLE_COLUMNS) {
        break
      }
      visibleColumnIds.add(columnId)
    }

    for (const columnId of nonInternalIds) {
      if (visibleColumnIds.size >= MAX_VICTORIALOGS_VISIBLE_COLUMNS) {
        break
      }

      visibleColumnIds.add(columnId)
    }
  }

  return Object.fromEntries(
    columnIds.map(columnId => [columnId, visibleColumnIds.has(columnId) && !isSystemField(source, columnId)]),
  )
}
