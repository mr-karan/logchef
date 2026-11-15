import { createApp, h, nextTick, reactive } from 'vue'
import { createPinia } from 'pinia'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import type { SavedViewLayout, TableViewSeed } from '@/lib/savedViewLayout'
import DataTable from '../data-table.vue'

const disposeTables: (() => void)[] = []
const columns = [
  { name: 'clock', type: 'DateTime' },
  { name: 'level', type: 'String' },
  { name: 'host', type: 'String' },
  { name: 'msg', type: 'String' },
]
const rows: Record<string, unknown>[] = [
  { clock: '2026-01-01T00:00:01Z', level: 'warn', host: 'b', msg: 'second' },
  { clock: '2026-01-01T00:00:03Z', level: 'error', host: 'a', msg: 'third' },
  { clock: '2026-01-01T00:00:02Z', level: 'info', host: 'c', msg: 'first' },
]

function headerNames(host: HTMLElement): string[] {
  return [...host.querySelectorAll('thead th .truncate')].map(label => label.textContent ?? '')
}

function firstColumnCells(host: HTMLElement, column: string): string[] {
  return [...host.querySelectorAll(`tbody td[data-cell-id$="_${column}"]`)].map(cell => cell.textContent?.trim() ?? '')
}

function mountTable(viewSeed: TableViewSeed | null) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const state = reactive({ viewSeed, data: rows })
  const layouts: SavedViewLayout[] = []
  const app = createApp({ render: () => h(DataTable, {
    columns,
    data: state.data,
    sourceId: 'view-seed',
    teamId: 7,
    source: { source_type: 'clickhouse', capabilities: [] },
    timestampField: 'clock',
    severityField: 'level',
    viewSeed: state.viewSeed,
    onWorkingLayout: (layout: SavedViewLayout) => layouts.push(layout),
  }) })
  app.use(createPinia())
  app.use(i18n)
  app.mount(host)
  disposeTables.push(() => {
    app.unmount()
    host.remove()
  })
  return { host, state, layouts }
}

beforeEach(() => {
  vi.useFakeTimers()
  localStorage.clear()
})

afterEach(() => {
  disposeTables.splice(0).forEach(dispose => dispose())
  vi.clearAllTimers()
  vi.useRealTimers()
  localStorage.clear()
})

it('renders the view columns in order with the view sort and emits the working layout', async () => {
  const seed: TableViewSeed = {
    viewId: 4,
    revision: 1,
    definition: { version: 1, columns: [{ name: 'msg' }, { name: 'host', width: 200 }], sort: [{ column: 'host', desc: false }] },
  }
  const table = mountTable(seed)
  await nextTick()
  expect(headerNames(table.host)).toEqual(['msg', 'host'])
  expect(firstColumnCells(table.host, 'host')).toEqual(['a', 'b', 'c'])
  expect(table.layouts.at(-1)).toEqual({ columns: [{ name: 'msg' }, { name: 'host', width: 200 }], sort: [{ column: 'host', desc: false }] })
})

it('sorts by timestamp descending when the view sort names a column the result lacks', async () => {
  const seed: TableViewSeed = {
    viewId: 4,
    revision: 1,
    definition: { version: 1, columns: [{ name: 'clock' }, { name: 'msg' }], sort: [{ column: 'gone', desc: false }] },
  }
  const table = mountTable(seed)
  await nextTick()
  expect(headerNames(table.host)).toEqual(['clock', 'msg'])
  expect(firstColumnCells(table.host, 'msg')).toEqual(['third', 'first', 'second'])
  expect(table.layouts.at(-1)?.sort).toEqual([{ column: 'gone', desc: false }])
})

it('emits nothing without a view', async () => {
  const table = mountTable(null)
  await nextTick()
  expect(headerNames(table.host)).toEqual(['clock', 'level', 'host', 'msg'])
  expect(table.layouts).toEqual([])
})

it('keeps a cleared sort of the working copy on reload', async () => {
  const seed: TableViewSeed = {
    viewId: 4,
    revision: 1,
    definition: { version: 1, columns: [{ name: 'clock' }, { name: 'msg' }], sort: [{ column: 'host', desc: false }] },
  }
  localStorage.setItem('logchef-viewState-7-view-seed', JSON.stringify({
    viewId: 4, revision: 1, columnOrder: ['clock', 'msg', 'level', 'host'],
    columnSizing: {}, columnVisibility: { clock: true, msg: true, level: false, host: false }, sorting: [],
  }))
  const table = mountTable(seed)
  await nextTick()
  expect(firstColumnCells(table.host, 'msg')).toEqual(['second', 'third', 'first'])
  expect(table.layouts.at(-1)?.sort).toBeUndefined()
})
