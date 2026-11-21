import { createApp, nextTick } from 'vue'
import { i18n } from '@/i18n'
import { createPinia } from 'pinia'
import { expect, it, vi } from 'vitest'
import DataTable from '../data-table.vue'

it('opens and closes the JSON detail panel when a flat log row is clicked', async () => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const columns = [
    { name: 'timestamp', type: 'DateTime', accessorKey: 'timestamp' },
    { name: 'body', type: 'String', accessorKey: 'body' },
  ]
  const app = createApp(DataTable, {
    columns,
    data: [{ timestamp: '2026-01-01T00:00:00Z', body: 'Expansion regression', nested: { code: 123 } }],
    sourceId: 'expansion-test',
    teamId: null,
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    const row = host.querySelector<HTMLTableRowElement>('tbody tr')
    if (!row) throw new Error('Log row was not rendered')
    expect(host.querySelector('.expanded-json-row')).toBeNull()
    row.click()
    await nextTick()
    expect(host.querySelector('.expanded-json-row')).not.toBeNull()
    expect(host.querySelector('.expanded-json-row')?.textContent).toContain('Expansion regression')
    row.click()
    await nextTick()
    expect(host.querySelector('.expanded-json-row')).toBeNull()
  } finally {
    app.unmount()
    host.remove()
  }
})

it('filters, excludes and adds columns from nested keys in the expanded row', async () => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const onDrillDown = vi.fn()
  const onAddColumn = vi.fn()
  const app = createApp(DataTable, {
    columns: [{ name: 'timestamp', type: 'DateTime' }, { name: 'msg', type: 'String' }],
    data: [{ timestamp: '2026-01-01T00:00:00Z', msg: '{"event":"request_done","http":{"status":500}}' }],
    sourceId: 'expansion-actions-test',
    teamId: null,
    onDrillDown,
    onAddColumn,
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    host.querySelector<HTMLTableRowElement>('tbody tr')?.click()
    await nextTick()
    const fieldRow = (field: string) => {
      const row = [...host.querySelectorAll<HTMLElement>('[data-field-row]')].find(element => element.querySelector('span')?.textContent === field)
      if (!row) throw new Error(`Field ${field} was not listed`)
      return row
    }
    const status = fieldRow('msg.http.status')
    status.querySelector<HTMLButtonElement>('button[aria-label^="Filter ≠"]')?.click()
    expect(onDrillDown).toHaveBeenCalledExactlyOnceWith({ column: 'msg.http.status', operator: '!=', value: '500' })
    status.querySelector<HTMLButtonElement>('button[aria-label^="Add as column"]')?.click()
    expect(onAddColumn).toHaveBeenCalledExactlyOnceWith({ field: 'msg.http.status', shownColumns: ['timestamp', 'msg'] })
    expect(fieldRow('msg.http').querySelector('button[aria-label^="Filter"]')).toBeNull()
    expect(fieldRow('timestamp').querySelector('button[aria-label^="Filter"]')).toBeNull()
  } finally {
    app.unmount()
    host.remove()
  }
})

it('filters top-level LogsQL fields from the expanded row but not keys inside a JSON string', async () => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const onDrillDown = vi.fn()
  const onAddColumn = vi.fn()
  const app = createApp(DataTable, {
    columns: [{ name: '_time', type: 'DateTime' }, { name: '_msg', type: 'String' }],
    data: [{ _time: '2026-01-01T00:00:00Z', _msg: '{"http":{"status":500}}', 'http.method': 'GET' }],
    timestampField: '_time',
    sourceId: 'expansion-logsql-test',
    teamId: null,
    activeMode: 'native',
    filterLanguage: 'logsql',
    onDrillDown,
    onAddColumn,
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    host.querySelector<HTMLTableRowElement>('tbody tr')?.click()
    await nextTick()
    const fieldRow = (field: string) => {
      const row = [...host.querySelectorAll<HTMLElement>('[data-field-row]')].find(element => element.querySelector('span')?.textContent === field)
      if (!row) throw new Error(`Field ${field} was not listed`)
      return row
    }
    fieldRow('"http.method"').querySelector<HTMLButtonElement>('button[aria-label^="Filter = this value"]')?.click()
    expect(onDrillDown).toHaveBeenCalledExactlyOnceWith({ column: '"http.method"', operator: '=', value: 'GET' })
    const nested = fieldRow('_msg.http.status')
    expect(nested.querySelector('button[aria-label^="Filter"]')).toBeNull()
    expect(nested.querySelector('button[aria-label^="Add as column"]')).toBeNull()
    expect(onAddColumn).not.toHaveBeenCalled()
  } finally {
    app.unmount()
    host.remove()
  }
})

it('offers no row filters for a native language without filter support', async () => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(DataTable, {
    columns: [{ name: 'timestamp', type: 'DateTime' }, { name: 'job', type: 'String' }],
    data: [{ timestamp: '2026-01-01T00:00:00Z', job: 'api' }],
    sourceId: 'expansion-sql-test',
    teamId: null,
    activeMode: 'native',
    filterLanguage: null,
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    host.querySelector<HTMLTableRowElement>('tbody tr')?.click()
    await nextTick()
    expect(host.querySelector('[data-field-row] button[aria-label^="Filter"]')).toBeNull()
  } finally {
    app.unmount()
    host.remove()
  }
})

it('pins the expanded panel to the visible width so long values wrap beside their actions', async () => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const clientWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientWidth')
  Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, get: () => 900 })
  const app = createApp(DataTable, {
    columns: [{ name: '_time', type: 'DateTime' }, { name: '_msg', type: 'String' }],
    data: [{ _time: '2026-01-01T00:00:00Z', _msg: 'long '.repeat(400) }],
    timestampField: '_time',
    sourceId: 'expansion-width-test',
    teamId: null,
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    host.querySelector<HTMLTableRowElement>('tbody tr')?.click()
    await nextTick()
    const panel = host.querySelector<HTMLElement>('.expanded-row-panel')
    expect(panel?.style.width).toBe('900px')
    expect(panel?.classList.contains('sticky')).toBe(true)
  } finally {
    app.unmount()
    host.remove()
    if (clientWidth) Object.defineProperty(HTMLElement.prototype, 'clientWidth', clientWidth)
  }
})

it('treats a shown column with a dot in its name as shown', async () => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(DataTable, {
    columns: [{ name: 'timestamp', type: 'DateTime' }, { name: 'http.method', type: 'String' }],
    data: [{ timestamp: '2026-01-01T00:00:00Z', 'http.method': 'GET' }],
    sourceId: 'expansion-dotted-test',
    teamId: null,
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    host.querySelector<HTMLTableRowElement>('tbody tr')?.click()
    await nextTick()
    const fieldRow = (field: string) => [...host.querySelectorAll<HTMLElement>('[data-field-row]')].find(element => element.querySelector('span')?.textContent === field)
    expect(fieldRow('"http.method"')?.querySelector('button[aria-label^="Add as column"]')).toBeNull()
  } finally {
    app.unmount()
    host.remove()
  }
})
