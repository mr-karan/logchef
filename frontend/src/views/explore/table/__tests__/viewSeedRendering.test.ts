import { createApp, h, nextTick, reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { useSavedViewsStore } from '@/stores/savedViews'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import type { SavedViewLayout, TableViewSeed } from '@/lib/savedViewLayout'
import type { SavedView } from '@/api/savedViews'
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
  const state = reactive({ viewSeed, data: rows, layoutResetToken: 0 })
  const layouts: SavedViewLayout[] = []
  const tableLayouts: SavedViewLayout[] = []
  const app = createApp({ render: () => h(DataTable, {
    columns,
    data: state.data,
    sourceId: 'view-seed',
    teamId: 7,
    source: { source_type: 'clickhouse', capabilities: [] },
    timestampField: 'clock',
    severityField: 'level',
    viewSeed: state.viewSeed,
    layoutResetToken: state.layoutResetToken,
    onWorkingLayout: (layout: SavedViewLayout) => layouts.push(layout),
    onTableLayout: (layout: SavedViewLayout) => tableLayouts.push(layout),
  }) })
  app.use(createPinia())
  app.use(i18n)
  app.mount(host)
  disposeTables.push(() => {
    app.unmount()
    host.remove()
  })
  return { host, state, layouts, tableLayouts }
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

it('reports the layout on screen without a view, for Save as new view, and stops under a view', async () => {
  const table = mountTable(null)
  await nextTick()
  expect(table.tableLayouts.at(-1)?.columns?.map(column => column.name)).toEqual(['clock', 'level', 'host', 'msg'])
  const reported = table.tableLayouts.length
  table.state.viewSeed = { viewId: 4, revision: 1, definition: { version: 1, columns: [{ name: 'msg' }] } }
  await nextTick()
  expect(table.tableLayouts).toHaveLength(reported)
  expect(table.layouts.at(-1)?.columns).toEqual([{ name: 'msg' }])
})

it('discards the working copy and seeds from the view when the reset token changes', async () => {
  const seed: TableViewSeed = { viewId: 4, revision: 1, definition: { version: 1, columns: [{ name: 'msg' }, { name: 'host' }] } }
  localStorage.setItem('logchef-viewState-7-view-seed', JSON.stringify({
    viewId: 4, revision: 1, columnOrder: ['clock', 'level', 'msg', 'host'],
    columnSizing: {}, columnVisibility: { clock: true, level: true, msg: false, host: false }, sorting: [],
  }))
  const table = mountTable(seed)
  await nextTick()
  expect(headerNames(table.host)).toEqual(['clock', 'level'])
  table.state.layoutResetToken++
  await nextTick()
  await nextTick()
  expect(headerNames(table.host)).toEqual(['msg', 'host'])
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

it('keeps a reset to the view when the reset also unmounts the table', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)
  const store = useSavedViewsStore()
  const view: SavedView = {
    id: 4, source_id: 5, scope: 'personal', name: 'compact view', description: '',
    definition: { version: 1, columns: [{ name: 'msg', width: 300 }, { name: 'host' }], display_mode: 'compact' },
    is_default: false, managed: false, revision: 1, created_at: '', updated_at: '', can_edit: true, can_set_default: true,
  }
  // The working copy: switched to table and widened msg.
  localStorage.setItem('logchef-viewState-7-5', JSON.stringify({
    viewId: 4, revision: 1, columnOrder: ['msg', 'host', 'clock', 'level'],
    columnSizing: { msg: 500 }, columnVisibility: { msg: true, host: true, clock: false, level: false }, sorting: [],
  }))
  store.enter(7, 5, { views: [view], default_view_id: null }, 4)
  store.setDisplayMode('table')
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp({ render: () => store.displayMode === 'table'
    ? h(DataTable, {
      columns, data: rows, sourceId: '5', teamId: 7,
      source: { source_type: 'clickhouse', capabilities: [] }, timestampField: 'clock', severityField: 'level',
      viewSeed: store.activeSeed, layoutResetToken: store.layoutResetToken,
    })
    : h('div', 'compact') })
  app.use(pinia)
  app.use(i18n)
  app.mount(host)
  disposeTables.push(() => {
    app.unmount()
    host.remove()
  })
  await nextTick()
  // The restored copy is waiting for its debounced write when Reset unmounts the table.
  store.resetActive()
  await nextTick()
  await nextTick()
  expect(host.textContent).toBe('compact')
  store.setDisplayMode('table')
  await nextTick()
  vi.runAllTimers()
  expect(JSON.parse(localStorage.getItem('logchef-viewState-7-5') ?? '{}').columnSizing).toEqual({ msg: 300 })
})
