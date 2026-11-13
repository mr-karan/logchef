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
