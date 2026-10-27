import { createApp, h, nextTick, reactive } from 'vue'
import { createPinia } from 'pinia'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import { COLUMN_SIZING_VERSION } from '@/composables/useColumnState'
import DataTable from '../data-table.vue'

const storageKey = 'logchef-tableState-7-default-order'
const disposeTables: (() => void)[] = []
const columns = [
  { name: 'opaque', type: 'String' },
  { name: 'mixed', type: 'String' },
  { name: 'clock', type: 'DateTime' },
  { name: 'json', type: 'String' },
  { name: 'context', type: 'String' },
  { name: 'priority', type: 'String' },
  { name: 'other_clock', type: 'DateTime' },
  { name: 'native_json', type: 'JSON' },
  { name: 'empty', type: 'String' },
  { name: 'count', type: 'UInt64' },
]
const originalOrder = ['clock', 'other_clock', 'priority', 'opaque', 'mixed', 'json', 'context', 'native_json', 'empty', 'count']
const readableOrder = ['clock', 'other_clock', 'priority', 'mixed', 'context', 'empty', 'count', 'opaque', 'json', 'native_json']
const rows: Record<string, unknown>[] = [
  {
    clock: '2026-01-01T00:00:00Z', other_clock: '2026-01-01T00:00:01Z', priority: 'info',
    opaque: '0123456789abcdef', mixed: '0123456789abcdef', json: '{"ok":true}', context: 'ready',
    native_json: { step: 1 }, empty: '', count: 42,
  },
  {
    clock: '2026-01-01T00:00:02Z', other_clock: '2026-01-01T00:00:03Z', priority: 'warn',
    opaque: 'fedcba9876543210', mixed: 'readable context', json: '[1,2]', context: 'done',
    native_json: [1, 2], empty: null, count: 23,
  },
]

const readableRows = rows.map(row => ({ ...row, opaque: 'readable context' }))

function headerNames(host: HTMLElement): string[] {
  return [...host.querySelectorAll('thead th .truncate')].map(label => label.textContent ?? '')
}

function header(host: HTMLElement, name: string): HTMLElement {
  const label = [...host.querySelectorAll('thead th .truncate')].find(label => label.textContent === name)
  const cell = label?.closest('th')
  if (!cell) throw new Error(`Header ${name} was not rendered`)
  return cell
}

function drag(host: HTMLElement, from: string, to: string) {
  header(host, from).dispatchEvent(new Event('dragstart', { bubbles: true, cancelable: true }))
  header(host, to).dispatchEvent(new Event('drop', { bubbles: true, cancelable: true }))
}

function mountTable(data: Record<string, unknown>[] = [], initialColumns = columns) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const state = reactive({ data, columns: initialColumns, sourceId: 'default-order' })
  const app = createApp({ render: () => h(DataTable, {
    columns: state.columns,
    data: state.data,
    stats: { execution_time_ms: 1, rows_read: state.data.length, bytes_read: 64 },
    sourceId: state.sourceId,
    teamId: 7,
    source: { source_type: 'clickhouse', capabilities: [] },
    timestampField: 'clock',
    severityField: 'priority',
  }) })
  app.use(createPinia())
  app.use(i18n)
  app.mount(host)
  const dispose = () => {
    app.unmount()
    host.remove()
  }
  disposeTables.push(dispose)
  return { host, state }
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

it('waits for the first non-empty result before persisting automatic order', async () => {
  const table = mountTable()
  await nextTick()
  await vi.advanceTimersByTimeAsync(300)
  expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnOrder ?? []).toEqual([])
  table.state.data = rows
  await nextTick()
  expect(headerNames(table.host)).toEqual(readableOrder)
  expect(table.host.querySelector('tbody tr td:first-child')?.querySelector('svg')).not.toBeNull()
  const container = table.host.querySelector<HTMLElement>('.overflow-auto')
  if (!container) throw new Error('Scroll container was not rendered')
  Object.defineProperty(container, 'clientWidth', { value: 4000 })
  container.dispatchEvent(new Event('scroll'))
  await vi.advanceTimersByTimeAsync(20)
  await nextTick()
  const cells = [...table.host.querySelectorAll('tbody tr:first-child td[data-cell-id]')]
  expect(cells.map(cell => cell.getAttribute('data-cell-id')?.slice(2))).toEqual(readableOrder)
  expect(table.host.querySelectorAll('thead th')).toHaveLength(columns.length + 1)
  await vi.advanceTimersByTimeAsync(300)
  expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').orderSource).toBe('auto')
})

it('keeps a drag when new rows arrive and restores the explicit order on reload', async () => {
  const table = mountTable()
  await nextTick()
  table.state.data = rows.map(row => ({ ...row, opaque: 'readable initially', json: 'readable initially', native_json: 'readable initially' }))
  await nextTick()
  expect(headerNames(table.host)).toEqual(originalOrder)
  table.state.data = rows
  // A query can deliver rows in the same tick as a user's drag.
  drag(table.host, 'context', 'opaque')
  await nextTick()
  const manualOrder = ['clock', 'other_clock', 'priority', 'context', 'opaque', 'mixed', 'json', 'native_json', 'empty', 'count']
  expect(headerNames(table.host)).toEqual(manualOrder)
  await vi.advanceTimersByTimeAsync(300)
  const reloaded = mountTable(rows)
  await nextTick()
  expect(headerNames(reloaded.host)).toEqual(manualOrder)
  expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').orderSource).toBe('explicit')
})

it('keeps legacy saved ordering and visibility while new data arrives', async () => {
  localStorage.setItem(storageKey, JSON.stringify({
    columnOrder: originalOrder,
    columnVisibility: { context: false },
    columnSizing: { mixed: 315 },
    sizingVersion: COLUMN_SIZING_VERSION,
  }))
  const table = mountTable()
  table.state.data = rows
  await nextTick()
  expect(headerNames(table.host)).toEqual(originalOrder.filter(id => id !== 'context'))
  expect(table.host.querySelector('td[data-cell-id="0_mixed"]')?.getAttribute('style')).toContain('width: 315px')
})

it('uses defaults for another source and restores manual order when switching back', async () => {
  const table = mountTable(rows)
  await nextTick()
  drag(table.host, 'opaque', 'mixed')
  await nextTick()
  const manualOrder = ['clock', 'other_clock', 'priority', 'opaque', 'mixed', 'context', 'empty', 'count', 'json', 'native_json']
  expect(headerNames(table.host)).toEqual(manualOrder)
  await vi.advanceTimersByTimeAsync(300)
  table.state.sourceId = 'another-source'
  await nextTick()
  expect(headerNames(table.host)).toEqual(readableOrder)
  table.state.sourceId = 'default-order'
  await nextTick()
  expect(headerNames(table.host)).toEqual(manualOrder)
})

it('keeps automatic column positions across consecutive results and reloads', async () => {
  const table = mountTable(rows)
  await nextTick()
  expect(headerNames(table.host)).toEqual(readableOrder)
  table.state.data = []
  await nextTick()
  table.state.data = readableRows
  await nextTick()
  expect(headerNames(table.host)).toEqual(readableOrder)
  await vi.advanceTimersByTimeAsync(300)
  expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnOrder).toEqual(readableOrder)
  const reloaded = mountTable(readableRows)
  await nextTick()
  expect(headerNames(reloaded.host)).toEqual(readableOrder)
})

it('recomputes automatic order from current rows only when reset', async () => {
  const table = mountTable(rows)
  await nextTick()
  table.state.data = readableRows
  await nextTick()
  expect(headerNames(table.host)).toEqual(readableOrder)
  const columnsButton = [...table.host.querySelectorAll('button')].find(button => button.textContent?.includes('Columns'))
  if (!columnsButton) throw new Error('Columns button was not rendered')
  columnsButton.click()
  await nextTick()
  const resetButton = [...document.querySelectorAll('button')].find(button => button.textContent?.includes('Reset'))
  if (!resetButton) throw new Error('Reset columns button was not rendered')
  resetButton.click()
  await nextTick()
  const resetOrder = ['clock', 'other_clock', 'priority', 'opaque', 'mixed', 'context', 'empty', 'count', 'json', 'native_json']
  expect(headerNames(table.host)).toEqual(resetOrder)
  table.state.data = rows
  await nextTick()
  expect(headerNames(table.host)).toEqual(resetOrder)
})

it('appends new readable and opaque columns without reshuffling existing columns', async () => {
  const table = mountTable(rows)
  await nextTick()
  table.state.columns = [...columns, { name: 'new_opaque', type: 'String' }, { name: 'new_context', type: 'String' }]
  table.state.data = rows.map(row => ({ ...row, new_opaque: '0123456789abcdef', new_context: 'ready' }))
  await nextTick()
  expect(headerNames(table.host)).toEqual([...readableOrder.slice(0, 7), 'new_context', ...readableOrder.slice(7), 'new_opaque'])
  table.state.columns = table.state.columns.filter(column => column.name !== 'mixed')
  await nextTick()
  expect(headerNames(table.host)).toEqual([...readableOrder.slice(0, 7).filter(id => id !== 'mixed'), 'new_context', ...readableOrder.slice(7), 'new_opaque'])
})

it('restores every column to its original position after a subset query', async () => {
  const table = mountTable(rows)
  await nextTick()
  table.state.columns = columns.filter(column => ['clock', 'context', 'opaque'].includes(column.name))
  await nextTick()
  await vi.advanceTimersByTimeAsync(300)
  expect(headerNames(table.host)).toEqual(['clock', 'context', 'opaque'])
  table.state.columns = columns
  await nextTick()
  expect(headerNames(table.host)).toEqual(readableOrder)
})

const boundaryColumns = ['clock', 'message', 'context', 'trace_id', 'parent_span_id'].map(name => ({ name, type: 'String' }))
const boundaryRow = { clock: '2026-01-01T00:00:00Z', message: 'hello', context: 'readable', trace_id: '0123456789abcdef', parent_span_id: 'fedcba9876543210' }
const boundaryOrder = boundaryColumns.map(column => column.name)

it('retains absent readable classes through subset persistence and full results', async () => {
  const table = mountTable([boundaryRow], boundaryColumns)
  await nextTick()
  await vi.advanceTimersByTimeAsync(300)
  table.state.columns = [...boundaryColumns.filter(column => ['clock', 'trace_id'].includes(column.name)), { name: 'region', type: 'String' }]
  table.state.data = [{ clock: boundaryRow.clock, trace_id: boundaryRow.trace_id, region: 'west' }]
  await nextTick()
  await vi.advanceTimersByTimeAsync(300)
  const expected = ['clock', 'message', 'context', 'region', 'trace_id', 'parent_span_id']
  expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnOrder).toEqual(expected)
  const subsetColumns = [...boundaryColumns.filter(column => ['clock', 'trace_id'].includes(column.name)), { name: 'region', type: 'String' }]
  const reloaded = mountTable([{ clock: boundaryRow.clock, trace_id: boundaryRow.trace_id, region: 'west' }], subsetColumns)
  await nextTick()
  expect(headerNames(reloaded.host)).toEqual(['clock', 'region', 'trace_id'])
  reloaded.state.columns = [...boundaryColumns, { name: 'region', type: 'String' }]
  reloaded.state.data = [{ ...boundaryRow, region: 'west' }]
  await nextTick()
  expect(headerNames(reloaded.host)).toEqual(expected)
})

it('uses the last opaque class when a sample becomes null', async () => {
  const table = mountTable([boundaryRow], boundaryColumns)
  await nextTick()
  await vi.advanceTimersByTimeAsync(300)
  table.state.columns = [...boundaryColumns, { name: 'region', type: 'String' }]
  table.state.data = [{ ...boundaryRow, parent_span_id: null, region: 'west' }]
  await nextTick()
  expect(headerNames(table.host)).toEqual(['clock', 'message', 'context', 'region', 'trace_id', 'parent_span_id'])
})

it('does not persist new columns until an empty result has sample rows', async () => {
  const table = mountTable([boundaryRow], boundaryColumns)
  await nextTick()
  await vi.advanceTimersByTimeAsync(300)
  table.state.columns = [...boundaryColumns, { name: 'region', type: 'String' }]
  table.state.data = []
  await nextTick()
  await vi.advanceTimersByTimeAsync(300)
  expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnOrder).toEqual(boundaryOrder)
  table.state.data = [{ ...boundaryRow, region: 'west' }]
  await nextTick()
  expect(headerNames(table.host)).toEqual(['clock', 'message', 'context', 'region', 'trace_id', 'parent_span_id'])
})
