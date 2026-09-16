import { createApp, h, nextTick, reactive } from 'vue'
import { i18n } from '@/i18n'
import { createPinia } from 'pinia'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import DataTable from '../data-table.vue'

const storageKey = 'logchef-tableState-7-vl-source'

function headerNames(host: HTMLElement): string[] {
  return [...host.querySelectorAll<HTMLElement>('thead th .truncate')].map(label => label.textContent ?? '')
}

function mountTable(initialColumnNames: string[]) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const state = reactive({ columnNames: initialColumnNames })
  const app = createApp({
    render: () => {
      const row = Object.fromEntries(state.columnNames.map(name => [name, name === '_time' ? '2026-01-01T00:00:00Z' : `${name}-value`]))
      return h(DataTable, {
        columns: state.columnNames.map(name => ({ name, type: 'String' })),
        data: [row],
        stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 32 },
        sourceId: 'vl-source',
        teamId: 7,
        source: { source_type: 'victorialogs', capabilities: [] },
        timestampField: '_time',
        severityField: 'level',
      })
    },
  })
  app.use(createPinia())
  app.use(i18n)
  app.mount(host)
  return { host, app, state }
}

beforeEach(() => {
  vi.useFakeTimers()
  localStorage.clear()
})

afterEach(() => {
  vi.useRealTimers()
  localStorage.clear()
})

it('keeps a schemaless source at the default column cap across queries with new fields', async () => {
  localStorage.setItem(storageKey, JSON.stringify({
    columnOrder: ['_time', '_msg', 'level', 'service', 'host', 'pod'],
    columnSizing: {},
    columnVisibility: { _time: true, _msg: true, level: true, service: true, host: true, pod: true, trace_id: false },
  }))
  const newFields = Array.from({ length: 30 }, (_, i) => `attr_${i}`)
  const { host, app, state } = mountTable(['_time', '_msg', '_stream', 'trace_id', ...newFields])
  try {
    await nextTick()
    expect(headerNames(host)).toEqual(['_time', '_msg'])

    // A following query with yet another field set persists the merged state,
    // keeping choices for fields that were absent from both results.
    state.columnNames = ['_time', '_msg', 'level', 'extra_0', 'extra_1']
    await nextTick()
    expect(headerNames(host)).toEqual(['_time', '_msg', 'level'])

    await vi.advanceTimersByTimeAsync(400)
    const persisted = JSON.parse(localStorage.getItem(storageKey) ?? '{}')
    expect(persisted.columnVisibility.service).toBe(true)
    expect(persisted.columnVisibility.trace_id).toBe(false)
    expect(persisted.columnVisibility.extra_0).toBe(false)
    expect(persisted.columnOrder.slice(0, 6)).toEqual(['_time', '_msg', 'level', 'service', 'host', 'pod'])
  } finally {
    app.unmount()
    host.remove()
  }
})

it('lets new fields fill the cap when fewer columns are saved as visible', async () => {
  localStorage.setItem(storageKey, JSON.stringify({
    columnOrder: ['_time', '_msg'],
    columnSizing: {},
    columnVisibility: { _time: true, _msg: true },
  }))
  const { host, app } = mountTable(['_time', '_msg', '_stream', 'level', 'service', 'a', 'b', 'c', 'd'])
  try {
    await nextTick()
    const headers = headerNames(host)
    expect(headers).toHaveLength(6)
    expect(headers).toEqual(expect.arrayContaining(['_time', '_msg', 'level', 'service']))
    expect(headers).not.toContain('_stream')
  } finally {
    app.unmount()
    host.remove()
  }
})
