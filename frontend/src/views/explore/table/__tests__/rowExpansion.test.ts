import { createApp, nextTick } from 'vue'
import { i18n } from '@/i18n'
import { createPinia } from 'pinia'
import { expect, it } from 'vitest'
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
    stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 32 },
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
