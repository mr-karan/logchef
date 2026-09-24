import type { ColumnDef } from './tableFeatures'
import type { Source } from '@/api/sources'
import {
  MESSAGE_FIELD_ALIASES,
  normalizeFieldName,
  isContextFieldName,
  isSystemField,
} from '@/lib/sourceFields'
const MAX_VICTORIALOGS_VISIBLE_COLUMNS = 6

// Before the visible-column cap, schemaless sources saved every field any query
// returned as visible. Saved visibility without this version is not the user's
// choice, so schemaless sources discard it. Order and widths are kept.
export const COLUMN_VISIBILITY_VERSION = 2

function isSchemalessSource(source: Pick<Source, 'source_type'> | null | undefined): boolean {
  return source?.source_type === 'victorialogs'
}

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

  if (!isSchemalessSource(source)) {
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

export function getTrustedSavedVisibility(
  source: Pick<Source, 'source_type'> | null | undefined,
  savedVisibility: Record<string, boolean> | undefined,
  savedVersion: number | undefined,
): Record<string, boolean> {
  if (isSchemalessSource(source) && savedVersion !== COLUMN_VISIBILITY_VERSION) {
    return {}
  }
  return savedVisibility ?? {}
}

/**
 * Merges saved visibility with the current column set. Saved entries win, including
 * entries for columns absent from this result, so choices survive queries that
 * return a different field set. Columns without a saved entry take their default,
 * except that a schemaless source stops making new columns visible once the visible
 * count reaches the default cap: schemaless results expose a different field set on
 * every query, and without the cap every field ever seen ends up rendered.
 */
export function resolveColumnVisibility(
  columns: ColumnDef<Record<string, any>>[],
  source: Pick<Source, 'source_type'> | null | undefined,
  savedVisibility: Record<string, boolean>,
  timestampField?: string,
  severityField?: string,
): Record<string, boolean> {
  const columnIds = getColumnIds(columns)
  const defaults = getDefaultColumnVisibility(columns, source, timestampField, severityField)
  const capNewColumns = isSchemalessSource(source)
  const visibility: Record<string, boolean> = { ...savedVisibility }
  // Count every saved visible column, not only those in this result: a later
  // query can return the union of all fields seen so far and render them all.
  let visibleCount = Object.values(savedVisibility).filter(Boolean).length

  for (const columnId of columnIds) {
    if (savedVisibility[columnId] !== undefined) {
      continue
    }

    const visible = defaults[columnId] && (!capNewColumns || visibleCount < MAX_VICTORIALOGS_VISIBLE_COLUMNS)
    visibility[columnId] = visible
    if (visible) {
      visibleCount += 1
    }
  }

  return visibility
}
