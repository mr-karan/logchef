import { createApp, nextTick } from 'vue'
import { createPinia } from 'pinia'
import { describe, expect, it } from 'vitest'
import { i18n } from '@/i18n'
import { usePreferencesStore } from '@/stores/preferences'
import DataTable from '../data-table.vue'
import { createColumns } from '../columns'

process.env.TZ = 'America/New_York'

describe('timestamp cells', () => {
  it.each([
    ['DateTime', '2026-01-01T00:00:00.000Z', '2026-01-01 00:00:00'],
    ['DateTime64(3)', '2026-01-01T00:00:00.123Z', '2026-01-01 00:00:00.123'],
    ['DateTime64(3)', 1767225600123, '2026-01-01 00:00:00.123'],
    ['DateTime64(0)', '2026-01-01T00:00:00.123Z', '2026-01-01 00:00:00'],
    ['Nullable(DateTime64(3, \'UTC\'))', '2026-01-01T00:00:00.1Z', '2026-01-01 00:00:00.100'],
    ['DateTime64(9)', '2026-01-01T00:00:00.123456789Z', '2026-01-01 00:00:00.123456789'],
    ['String', '2026-01-01T00:00:00.123456Z', '2026-01-01 00:00:00.123456'],
    ['String', '2026-01-01T00:00:00Z', '2026-01-01 00:00:00'],
    ['DateTime', '2026-01-01T05:30:00.000+05:30', '2026-01-01 00:00:00'],
    ['DateTime', 'invalid', 'invalid'],
  ])('renders %s precision for %s', async (type, value, expected) => {
    const host = document.createElement('div')
    document.body.appendChild(host)
    const app = createApp(DataTable, {
      columns: [{ name: 'timestamp', type }],
      data: [{ timestamp: value }],
      stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 64 },
      sourceId: `timestamp-render-${type}-${value}`,
      teamId: null,
    })
    const pinia = createPinia()
    app.use(pinia)
    usePreferencesStore(pinia).preferences.timezone = 'utc'
    app.use(i18n)
    try {
      app.mount(host)
      await nextTick()
      const cell = host.querySelector('td[data-cell-id] .flex-render-content')
      expect(cell?.textContent).toBe(expected)
      expect(cell?.getAttribute('title')).toBe(String(value))
      if (value !== 'invalid') {
        expect(cell?.querySelector('.table-timestamp-date')?.textContent).toBe('2026-01-01')
        expect(cell?.querySelector('.table-timestamp-time')?.textContent).toBe(expected.slice(11))
      }
    } finally {
      app.unmount()
      host.remove()
    }
  })

  it('groups date-typed columns after the primary one and leaves numeric time fields alone', () => {
    const columns = createColumns([
      { name: 'body', type: 'String' },
      { name: 'request_time', type: 'Float64' },
      { name: 'timestamp', type: 'DateTime64(3)' },
      { name: 'arrival_timestamp', type: 'DateTime' },
      { name: 'created_at', type: "Nullable(DateTime64(3, 'UTC'))" },
    ])
    expect(columns.map(column => column.id)).toEqual(['timestamp', 'arrival_timestamp', 'created_at', 'body', 'request_time'])
  })
})

it.each(['Date', 'Date32', 'Nullable(Date)', 'Nullable(Date32)'].flatMap(type =>
  ['2024-01-15', '2024-01-15T00:00:00Z'].map(value => ({ type, value })),
))('keeps $type value $value as a calendar date in local time', async ({ type, value }) => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(DataTable, {
    columns: [{ name: 'timestamp', type }],
    data: [{ timestamp: value }],
    stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 10 },
    sourceId: 'calendar-date', teamId: null,
  })
  const pinia = createPinia()
  app.use(pinia)
  usePreferencesStore(pinia).preferences.timezone = 'local'
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    const cell = host.querySelector('td[data-cell-id] .flex-render-content')
    expect(cell?.textContent).toBe('2024-01-15')
    expect(cell?.getAttribute('title')).toBe(value)
    expect(cell?.querySelector('.table-timestamp-time')).toBeNull()
  } finally {
    app.unmount()
    host.remove()
  }
})

it.each(['date', 'created', 'updated', 'timestamp'])('keeps String %s dates on the same calendar day in New York', async name => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(DataTable, {
    columns: [{ name, type: 'String' }], data: [{ [name]: '2024-01-15' }],
    stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 10 },
    sourceId: 'string-calendar-date', teamId: null,
  })
  const pinia = createPinia()
  app.use(pinia)
  usePreferencesStore(pinia).preferences.timezone = 'local'
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    const cell = host.querySelector('td[data-cell-id] .flex-render-content')
    expect(cell?.textContent).toBe('2024-01-15')
    expect(cell?.getAttribute('title')).toBe('2024-01-15')
    expect(cell?.querySelector('.table-timestamp-time')).toBeNull()
  } finally {
    app.unmount()
    host.remove()
  }
})
