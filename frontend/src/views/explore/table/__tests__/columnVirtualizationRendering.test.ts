import { createApp, nextTick } from 'vue'
import { i18n } from '@/i18n'
import { createPinia } from 'pinia'
import { expect, it, vi } from 'vitest'
import DataTable from '../data-table.vue'
import { COLUMN_VISIBILITY_VERSION } from '../defaultColumnVisibility'
import { useTable } from '@tanstack/vue-table'

vi.mock('@tanstack/vue-table', async importOriginal => {
  const actual = await importOriginal<typeof import('@tanstack/vue-table')>()
  return { ...actual, useTable: vi.fn(actual.useTable) }
})

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
    // Even the initial, unmeasured viewport must not mount the entire schema.
    expect(host.querySelectorAll('thead th')).toHaveLength(41)
    expect(host.querySelectorAll('tbody tr:first-child td[data-cell-id]').length).toBeLessThan(15)
    expect(host.querySelectorAll('.virtual-spacer')).toHaveLength(2)

    const container = host.querySelector<HTMLElement>('.overflow-auto')
    if (!container) throw new Error('Scroll container was not rendered')
    Object.defineProperty(container, 'clientWidth', { value: 600, configurable: true })
    Object.defineProperty(container, 'scrollLeft', { value: 2000, configurable: true, writable: true })
    container.dispatchEvent(new Event('scroll'))
    await vi.waitFor(() => {
      expect(host.querySelector('tbody tr:first-child td[data-cell-id]')?.getAttribute('data-cell-id')).not.toContain('timestamp')
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

it('bounds first-mount work with 268 saved visible columns and does not populate full-row cell caches', async () => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const names = Array.from({ length: 268 }, (_, i) => `field_${String(i).padStart(3, '0')}`)
  const storageKey = 'logchef-tableState-7-wide-mount-test'
  localStorage.setItem(storageKey, JSON.stringify({
    columnOrder: names,
    columnSizing: {},
    columnVisibility: Object.fromEntries(names.map(name => [name, true])),
    visibilityVersion: COLUMN_VISIBILITY_VERSION,
  }))
  const app = createApp(DataTable, {
    columns: names.map(name => ({ name, type: 'String' })),
    data: Array.from({ length: 100 }, (_, i) => Object.fromEntries(names.map(name => [name, `${i}-${name}`]))),
    stats: { execution_time_ms: 1, rows_read: 100, bytes_read: 64 },
    sourceId: 'wide-mount-test',
    teamId: 7,
    source: { source_type: 'victorialogs', capabilities: [] },
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    // Check synchronously, before nextTick/ResizeObserver can hide a costly
    // first mount. Old saved choices must remain enabled without mounting all.
    expect(host.querySelectorAll('thead th')).toHaveLength(269)
    const initialCells = host.querySelectorAll('td[data-cell-id]').length
    expect(initialCells).toBeGreaterThan(0)
    expect(initialCells).toBeLessThan(50 * 15)

    const table = vi.mocked(useTable).mock.results.at(-1)!.value
    // TanStack populates this cache when getAllCells/getVisibleCells is called.
    // DOM-only checks miss those allocations for hidden/offscreen columns.
    const expectNoFullRowCaches = () => {
      for (const row of table.getCoreRowModel().rows) expect(row._cellsCache).toBeUndefined()
    }
    expectNoFullRowCaches()
    await nextTick()

    const container = host.querySelector<HTMLElement>('.overflow-auto')!
    Object.defineProperty(container, 'clientWidth', { value: 600, configurable: true })
    container.scrollLeft = 1_000_000
    container.dispatchEvent(new Event('scroll'))
    await vi.waitFor(() => expect(host.querySelector('td[data-cell-id="0_field_267"]')).not.toBeNull())
    expect(host.querySelector('td[data-cell-id="0_field_000"]')).toBeNull()
    expectNoFullRowCaches()

    host.querySelector<HTMLTableCellElement>('td[data-cell-id]')!.click()
    await nextTick()
    expect(host.querySelector('.expanded-json-row td')?.getAttribute('colspan')).toBe('269')
    expectNoFullRowCaches()

    table.nextPage()
    await nextTick()
    expect(host.querySelector('td[data-cell-id="50_field_267"]')?.textContent).toContain('50-field_267')
    expect(host.querySelector('td[data-cell-id="0_field_267"]')).toBeNull()
    expectNoFullRowCaches()

    table.previousPage()
    await nextTick()
    expect(host.querySelector('td[data-cell-id="0_field_267"]')?.textContent).toContain('0-field_267')
    expectNoFullRowCaches()
  } finally {
    app.unmount()
    host.remove()
    localStorage.removeItem(storageKey)
  }
})
