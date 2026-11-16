import type { ColumnSizingState, ColumnVisibilityState, SortingState } from '@tanstack/vue-table'

// The saved view definition, as the server stores it (RFC 0007).
export interface SavedViewColumn {
  name: string
  width?: number
}

export interface SavedViewSort {
  column: string
  desc: boolean
}

export interface SavedViewDefinition {
  version: 1
  columns?: SavedViewColumn[]
  sort?: SavedViewSort[]
  display_mode?: 'table' | 'compact' | 'json'
  filter?: string
  time_range?: string
}

/** The parts of a definition that the table owns. */
export type SavedViewLayout = Pick<SavedViewDefinition, 'columns' | 'sort'>

/** The view the table seeds from. The id and revision decide whether a stored working copy still applies. */
export interface TableViewSeed {
  viewId: number
  revision: number
  definition: SavedViewDefinition
}

export interface TableColumnLayout {
  columnOrder: string[]
  columnSizing: ColumnSizingState
  columnVisibility: ColumnVisibilityState
}

// The server's definition bounds.
const MIN_COLUMN_WIDTH = 50
const MAX_COLUMN_WIDTH = 3000
const MAX_COLUMNS = 200
const MAX_SORT_ENTRIES = 3

export function timestampFirst(order: string[], timestampField: string): string[] {
  return order.includes(timestampField)
    ? [timestampField, ...order.filter(id => id !== timestampField)]
    : order
}

/**
 * Maps a view's columns to table state: the view's columns in order, then every
 * other column in the automatic order. Only the view's columns are visible, and
 * the sizing holds the view's widths. Names absent from the result keep their
 * place, so saving the layout again does not drop them.
 */
export function seedColumnLayout(
  columns: SavedViewColumn[],
  automaticOrder: string[],
  timestampField: string,
): TableColumnLayout {
  const names = new Set(columns.map(column => column.name))
  const order = timestampFirst([...names, ...automaticOrder.filter(id => !names.has(id))], timestampField)
  const columnSizing: ColumnSizingState = {}
  for (const column of columns) {
    if (column.width !== undefined) columnSizing[column.name] = column.width
  }
  return {
    columnOrder: order,
    columnSizing,
    columnVisibility: Object.fromEntries(order.map(id => [id, names.has(id)])),
  }
}

export function seedSorting(sort: SavedViewSort[]): SortingState {
  return sort.map(entry => ({ id: entry.column, desc: entry.desc }))
}

/**
 * Turns table state back into the layout part of a definition, within the
 * server's bounds. A part with no entry is left out.
 */
export function tableLayoutToDefinition(layout: TableColumnLayout, sorting: SortingState): SavedViewLayout {
  const columns = layout.columnOrder
    .filter(id => layout.columnVisibility[id] !== false)
    .slice(0, MAX_COLUMNS)
    .map((name): SavedViewColumn => {
      const width = layout.columnSizing[name]
      return width === undefined
        ? { name }
        : { name, width: Math.min(MAX_COLUMN_WIDTH, Math.max(MIN_COLUMN_WIDTH, Math.round(width))) }
    })
  const sort = sorting.slice(0, MAX_SORT_ENTRIES).map(entry => ({ column: entry.id, desc: entry.desc }))
  return {
    ...(columns.length ? { columns } : {}),
    ...(sort.length ? { sort } : {}),
  }
}

/** Whether a result shows at least one of the layout's visible columns. */
export function layoutHasResultColumn(layout: TableColumnLayout, resultColumnIds: readonly string[]): boolean {
  return resultColumnIds.some(id => layout.columnVisibility[id] === true)
}

/**
 * The sort to apply to one result. Entries on columns the result lacks are
 * dropped; when none is left, the table sorts by timestamp descending.
 */
export function sortingForResult(sorting: SortingState, resultColumnIds: readonly string[], timestampField: string): SortingState {
  if (!sorting.length) return sorting
  const present = sorting.filter(entry => resultColumnIds.includes(entry.id))
  if (present.length) return present
  return resultColumnIds.includes(timestampField) ? [{ id: timestampField, desc: true }] : []
}

/**
 * The column and sort names of a definition that a result lacks, each once,
 * columns first. The table skips them for this result; the view keeps them.
 */
export function missingViewColumns(definition: SavedViewDefinition, resultColumnIds: readonly string[]): string[] {
  const present = new Set(resultColumnIds)
  const names = [...(definition.columns ?? []).map(column => column.name), ...(definition.sort ?? []).map(entry => entry.column)]
  return [...new Set(names.filter(name => !present.has(name)))]
}

/** The definition without the named columns in `columns` and `sort`. A part left empty is dropped. */
export function withoutColumns(definition: SavedViewDefinition, names: readonly string[]): SavedViewDefinition {
  const removed = new Set(names)
  const { columns, sort, ...rest } = definition
  const keptColumns = columns?.filter(column => !removed.has(column.name))
  const keptSort = sort?.filter(entry => !removed.has(entry.column))
  return {
    ...rest,
    ...(keptColumns?.length ? { columns: keptColumns } : {}),
    ...(keptSort?.length ? { sort: keptSort } : {}),
  }
}
