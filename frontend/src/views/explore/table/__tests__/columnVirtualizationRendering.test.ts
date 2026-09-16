import { createApp, nextTick } from 'vue'
import { i18n } from '@/i18n'
import { createPinia } from 'pinia'
import { expect, it, vi } from 'vitest'
import DataTable from '../data-table.vue'

it('renders only the columns under the horizontal viewport, with spacers keeping alignment', async () => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const columnNames = ['timestamp', ...Array.from({ length: 39 }, (_, i) => `field_${String(i).padStart(2, '0')}`)]
  const row = Object.fromEntries(columnNames.map(name => [name, name === 'timestamp' ? '2026-01-01T00:00:00Z' : `${name}-value`]))
  const app = createApp(DataTable, {
    columns: columnNames.map(name => ({ name, type: 'String' })),
    data: [row, { ...row, timestamp: '2026-01-01T00:00:01Z' }],
    stats: { execution_time_ms: 1, rows_read: 2, bytes_read: 64 },
    sourceId: 'virtualization-test',
    teamId: null,
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    // Unmeasured viewport (jsdom has no layout): every column renders.
    expect(host.querySelectorAll('thead th')).toHaveLength(41)
    expect(host.querySelectorAll('tbody tr:first-child td[data-cell-id]')).toHaveLength(40)
    expect(host.querySelectorAll('.virtual-spacer')).toHaveLength(0)

    const container = host.querySelector<HTMLElement>('.overflow-auto')
    if (!container) throw new Error('Scroll container was not rendered')
    Object.defineProperty(container, 'clientWidth', { value: 600, configurable: true })
    Object.defineProperty(container, 'scrollLeft', { value: 2000, configurable: true, writable: true })
    container.dispatchEvent(new Event('scroll'))
    await vi.waitFor(() => {
      expect(host.querySelectorAll('tbody tr:first-child td[data-cell-id]').length).toBeLessThan(40)
    })

    // Header stays complete so table-layout: fixed keeps column widths.
    expect(host.querySelectorAll('thead th')).toHaveLength(41)
    const firstRow = host.querySelector('tbody tr')
    if (!firstRow) throw new Error('No row rendered')
    const spacers = [...firstRow.querySelectorAll<HTMLTableCellElement>('.virtual-spacer')]
    expect(spacers).toHaveLength(2)
    const rendered = firstRow.querySelectorAll('td[data-cell-id]').length
    const spanned = spacers.reduce((sum, td) => sum + td.colSpan, 0)
    expect(rendered + spanned).toBe(40)
    // Column widths default to 180px, so 2000px of scroll skips the first ~10 columns minus overscan.
    expect(spacers[0].colSpan).toBeGreaterThanOrEqual(6)
    expect(firstRow.querySelector('td[data-cell-id]')?.getAttribute('data-cell-id')).not.toContain('timestamp')
  } finally {
    app.unmount()
    host.remove()
  }
})
