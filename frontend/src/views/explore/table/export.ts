import type { Row, Table, Column, ColumnMeta } from '@tanstack/vue-table'

// Define extended column meta interface that includes our custom properties
interface ExtendedColumnMeta extends ColumnMeta<any, unknown> {
  name?: string;
  columnType?: string;
  [key: string]: any;
}

/**
 * Utility to generate and download a CSV file from table data
 */
export function generateCSV<T>(
  table: Table<T>,
  options?: {
    fileName?: string
    includeHiddenColumns?: boolean
    exportType?: 'all' | 'visible' | 'filtered' | 'page'
  }
) {
  const {
    fileName = 'export',
    includeHiddenColumns = false,
    exportType = 'all'
  } = options || {}

  // Get rows based on export type
  let rows: Row<T>[] = []
  switch (exportType) {
    case 'all':
      rows = table.getCoreRowModel().rows
      break
    case 'filtered':
      rows = table.getFilteredRowModel().rows
      break
    case 'page':
      rows = table.getPaginationRowModel().rows
      break
    case 'visible':
    default:
      rows = table.getRowModel().rows
      break
  }

  // Get columns based on visibility option
  const columns = includeHiddenColumns
    ? table.getAllColumns()
    : table.getVisibleFlatColumns()

  // Create headers row
  const headers = columns.map((column: Column<T, unknown>) => {
    // Try to get the display name from meta properties or fall back to header or id
    const meta = column.columnDef.meta as ExtendedColumnMeta | undefined;
    const displayName =
      // First try meta.name (custom property)
      meta?.name ||
      // Then try to get a plain string header
      (typeof column.columnDef.header === 'string' ? column.columnDef.header :
      // Finally fall back to column ID
      column.id);

    return `"${String(displayName).replace(/"/g, '""')}"`
  })

  // Create data rows
  const csvRows = [
    // Headers row
    headers.join(','),
    // Data rows
    ...rows.map(row => {
      return columns
        .map((column: Column<T, unknown>) => {
          // Get cell value - use getValue to get the processed value
          const value = row.getValue(column.id)

          // Format the value properly for CSV
          if (value === null || value === undefined) {
            return ''
          } else if (typeof value === 'object') {
            return `"${JSON.stringify(value).replace(/"/g, '""')}"`
          }
          return `"${String(value).replace(/"/g, '""')}"`
        })
        .join(',')
    })
  ].join('\n')

  // Create and download the CSV file
  const csvBlob = new Blob([csvRows], { type: 'text/csv;charset=utf-8;' })
  const csvUrl = URL.createObjectURL(csvBlob)
  const link = document.createElement('a')
  link.href = csvUrl
  link.setAttribute('download', `${fileName}.csv`)
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(csvUrl)

  return {
    success: true,
    message: 'CSV export completed'
  }
}

/**
 * Utility to export table data based on specified options
 */
export function exportTableData<T>(
  table: Table<T>,
  options?: {
    fileName?: string
    includeHiddenColumns?: boolean
    exportType?: 'all' | 'visible' | 'filtered' | 'page'
    format?: 'csv'
  }
) {
  const {
    fileName = 'export',
    includeHiddenColumns = false,
    exportType = 'all',
    format = 'csv'
  } = options || {}

  switch (format) {
    case 'csv':
    default:
      return generateCSV(table, { fileName, includeHiddenColumns, exportType })
  }
}