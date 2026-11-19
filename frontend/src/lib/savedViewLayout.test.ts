import { describe, expect, it } from 'vitest'
import {
  layoutHasResultColumn,
  missingViewColumns,
  seedColumnLayout,
  seedSorting,
  sortingForResult,
  tableLayoutToDefinition,
  withoutColumns,
  type SavedViewColumn,
} from './savedViewLayout'

const viewColumns: SavedViewColumn[] = [{ name: 'timestamp', width: 190 }, { name: 'level' }, { name: 'msg', width: 640 }]

describe('seedColumnLayout', () => {
  it('shows the view columns in order, then hides the rest in automatic order', () => {
    expect(seedColumnLayout(viewColumns, ['host', 'msg', 'timestamp', 'status'], 'timestamp')).toEqual({
      columnOrder: ['timestamp', 'level', 'msg', 'host', 'status'],
      columnSizing: { timestamp: 190, msg: 640 },
      columnVisibility: { timestamp: true, level: true, msg: true, host: false, status: false },
    })
  })

  it('keeps the timestamp column first, as the table always does', () => {
    const layout = seedColumnLayout([{ name: 'msg' }, { name: 'timestamp' }], ['timestamp', 'msg', 'host'], 'timestamp')
    expect(layout.columnOrder).toEqual(['timestamp', 'msg', 'host'])
    expect(layout.columnVisibility).toEqual({ timestamp: true, msg: true, host: false })
  })

  it('keeps view columns that the result lacks in their place', () => {
    const layout = seedColumnLayout(viewColumns, ['timestamp', 'msg', 'host'], 'timestamp')
    expect(layout.columnOrder).toEqual(['timestamp', 'level', 'msg', 'host'])
    expect(layout.columnVisibility.level).toBe(true)
    expect(tableLayoutToDefinition(layout, []).columns).toEqual(viewColumns)
  })
})

describe('tableLayoutToDefinition', () => {
  it('round-trips a definition through table state', () => {
    const sort = [{ column: 'level', desc: false }, { column: 'timestamp', desc: true }]
    const layout = seedColumnLayout(viewColumns, ['host', 'timestamp', 'level', 'msg'], 'timestamp')
    expect(tableLayoutToDefinition(layout, seedSorting(sort))).toEqual({ columns: viewColumns, sort })
  })

  it('maps table edits to the server bounds', () => {
    const layout = {
      columnOrder: ['timestamp', 'host', 'msg', 'level'],
      columnSizing: { timestamp: 12.4, host: 220.6, msg: 9000 },
      columnVisibility: { timestamp: true, host: true, msg: true, level: false },
    }
    const sorting = [{ id: 'a', desc: true }, { id: 'b', desc: false }, { id: 'c', desc: true }, { id: 'd', desc: false }]
    expect(tableLayoutToDefinition(layout, sorting)).toEqual({
      columns: [{ name: 'timestamp', width: 50 }, { name: 'host', width: 221 }, { name: 'msg', width: 3000 }],
      sort: [{ column: 'a', desc: true }, { column: 'b', desc: false }, { column: 'c', desc: true }],
    })
  })

  it('leaves out parts with no entry', () => {
    const layout = { columnOrder: ['a'], columnSizing: {}, columnVisibility: { a: false } }
    expect(tableLayoutToDefinition(layout, [])).toEqual({})
  })

  it('caps the columns at 200', () => {
    const ids = Array.from({ length: 201 }, (_, i) => `c${i}`)
    const layout = { columnOrder: ids, columnSizing: {}, columnVisibility: {} }
    expect(tableLayoutToDefinition(layout, []).columns).toHaveLength(200)
  })
})

describe('sortingForResult', () => {
  it('drops a sort entry on a column the result lacks', () => {
    const sorting = [{ id: 'gone', desc: false }, { id: 'level', desc: true }]
    expect(sortingForResult(sorting, ['timestamp', 'level'], 'timestamp')).toEqual([{ id: 'level', desc: true }])
  })

  it('sorts by timestamp descending when no entry is left', () => {
    expect(sortingForResult([{ id: 'gone', desc: false }], ['timestamp', 'level'], 'timestamp'))
      .toEqual([{ id: 'timestamp', desc: true }])
    expect(sortingForResult([{ id: 'gone', desc: false }], ['level'], 'timestamp')).toEqual([])
  })

  it('leaves a sort the user cleared alone', () => {
    expect(sortingForResult([], ['timestamp'], 'timestamp')).toEqual([])
  })
})

describe('layoutHasResultColumn', () => {
  it('is false when the result shows none of the visible columns', () => {
    const layout = seedColumnLayout([{ name: 'gone' }], ['timestamp', 'level'], 'timestamp')
    expect(layoutHasResultColumn(layout, ['timestamp', 'level'])).toBe(false)
    expect(layoutHasResultColumn(layout, ['timestamp', 'gone'])).toBe(true)
  })
})

describe('missing view columns', () => {
  const definition = { version: 1 as const, columns: viewColumns, sort: [{ column: 'msg', desc: false }, { column: 'host', desc: true }], filter: 'level="error"' }

  it('lists the column and sort names a result lacks, once each', () => {
    expect(missingViewColumns(definition, ['timestamp', 'level', 'msg', 'host'])).toEqual([])
    expect(missingViewColumns(definition, ['timestamp', 'level'])).toEqual(['msg', 'host'])
  })

  it('removes the names from columns and sort and keeps the other parts', () => {
    expect(withoutColumns(definition, ['msg'])).toEqual({
      version: 1,
      columns: [{ name: 'timestamp', width: 190 }, { name: 'level' }],
      sort: [{ column: 'host', desc: true }],
      filter: 'level="error"',
    })
  })

  it('drops a part that has no entry left', () => {
    expect(withoutColumns(definition, ['timestamp', 'level', 'msg', 'host'])).toEqual({ version: 1, filter: 'level="error"' })
  })
})
