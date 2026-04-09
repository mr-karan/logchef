import type { ColumnDef } from '@tanstack/vue-table'
import type { Source } from '@/api/sources'

const VICTORIALOGS_INTERNAL_FIELDS = new Set(['stream', 'streamid'])
const VICTORIALOGS_MESSAGE_FIELDS = ['msg', 'message', 'log', 'body', 'text', 'content']
const VICTORIALOGS_CONTEXT_FIELDS = [
  'service',
  'app',
  'application',
  'env',
  'environment',
  'namespace',
  'kubernetesnamespace',
  'pipeline',
  'cluster',
  'host',
  'pod',
  'container',
]
const MAX_VICTORIALOGS_VISIBLE_COLUMNS = 6

function normalizeFieldName(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, '')
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

function isVictoriaLogsInternalField(columnId: string): boolean {
  return VICTORIALOGS_INTERNAL_FIELDS.has(normalizeFieldName(columnId))
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

  const nonInternalIds = columnIds.filter(columnId => !isVictoriaLogsInternalField(columnId))
  const visibleColumnIds = new Set<string>()

  if (timestampField && nonInternalIds.includes(timestampField)) {
    visibleColumnIds.add(timestampField)
  }

  if (severityField && nonInternalIds.includes(severityField)) {
    visibleColumnIds.add(severityField)
  }

  addFirstMatchingColumn(nonInternalIds, visibleColumnIds, VICTORIALOGS_MESSAGE_FIELDS)

  if (nonInternalIds.length <= MAX_VICTORIALOGS_VISIBLE_COLUMNS) {
    nonInternalIds.forEach(columnId => visibleColumnIds.add(columnId))
  } else {
    for (const alias of VICTORIALOGS_CONTEXT_FIELDS) {
      if (visibleColumnIds.size >= MAX_VICTORIALOGS_VISIBLE_COLUMNS) {
        break
      }

      addFirstMatchingColumn(nonInternalIds, visibleColumnIds, [alias])
    }

    for (const columnId of nonInternalIds) {
      if (visibleColumnIds.size >= MAX_VICTORIALOGS_VISIBLE_COLUMNS) {
        break
      }

      visibleColumnIds.add(columnId)
    }
  }

  return Object.fromEntries(
    columnIds.map(columnId => [columnId, visibleColumnIds.has(columnId) && !isVictoriaLogsInternalField(columnId)]),
  )
}
