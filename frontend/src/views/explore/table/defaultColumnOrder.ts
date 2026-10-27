import { classifyCellValue } from './cellTypography'

const SAMPLE_ROWS = 50

interface OrderColumn {
  id?: string
  meta?: { columnType?: string }
}

export type ColumnClass = 'readable' | 'opaque' | 'unknown'

export interface DefaultColumnOrder {
  order: string[]
  columnClasses: Record<string, ColumnClass>
}

export function getDefaultColumnOrder(
  columns: readonly OrderColumn[],
  rows: readonly Record<string, unknown>[],
): DefaultColumnOrder {
  const sample = rows.slice(0, SAMPLE_ROWS)
  const readable: string[] = []
  const opaque: string[] = []
  const columnClasses: Record<string, ColumnClass> = {}

  for (const column of columns) {
    const id = column.id
    if (!id) continue
    const values = sample.map(row => row[id]).filter(value =>
      value !== null && value !== undefined && !(typeof value === 'string' && value.trim() === ''))
    if (!values.length) {
      columnClasses[id] = 'unknown'
      readable.push(id)
      continue
    }
    const defer = column.meta?.columnType !== 'timestamp' && column.meta?.columnType !== 'severity'
      && values.every(value => {
        const kind = typeof value === 'object' ? 'structured' : classifyCellValue(value)
        return kind === 'identifier' || kind === 'structured'
      })
    columnClasses[id] = defer ? 'opaque' : 'readable'
    if (defer) opaque.push(id)
    else readable.push(id)
  }

  return { order: [...readable, ...opaque], columnClasses }
}
