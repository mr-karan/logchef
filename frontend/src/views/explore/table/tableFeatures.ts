import {
  tableFeatures, columnFilteringFeature, globalFilteringFeature,
  rowSortingFeature, rowPaginationFeature, rowExpandingFeature,
  columnVisibilityFeature, columnOrderingFeature, columnSizingFeature,
  columnResizingFeature, rowSelectionFeature,
  createFilteredRowModel, createSortedRowModel, createPaginatedRowModel,
  createExpandedRowModel, filterFns, sortFns,
  type ColumnDef as TableColumnDef, type Column as TableColumn,
  type ColumnMeta as TableColumnMeta, type Row as TableRow,
  type Table as TableInstance, type RowData,
} from '@tanstack/vue-table'

export const logTableFeatures = tableFeatures({
  columnFilteringFeature, globalFilteringFeature, rowSortingFeature,
  rowPaginationFeature, rowExpandingFeature, columnVisibilityFeature,
  columnOrderingFeature, columnSizingFeature, columnResizingFeature,
  rowSelectionFeature,
  filteredRowModel: createFilteredRowModel(),
  sortedRowModel: createSortedRowModel(),
  paginatedRowModel: createPaginatedRowModel(),
  expandedRowModel: createExpandedRowModel(),
  filterFns, sortFns,
})

type Features = typeof logTableFeatures
export type ColumnDef<TData extends RowData, TValue = unknown> = TableColumnDef<Features, TData, TValue>
export type Column<TData extends RowData, TValue = unknown> = TableColumn<Features, TData, TValue>
export type ColumnMeta<TData extends RowData, TValue = unknown> = TableColumnMeta<Features, TData, TValue>
export type Row<TData extends RowData> = TableRow<Features, TData>
export type Table<TData extends RowData> = TableInstance<Features, TData>
