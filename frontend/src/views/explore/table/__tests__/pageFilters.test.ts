import { createApp, nextTick } from 'vue'
import { createPinia } from 'pinia'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import DataTable from '../data-table.vue'
import { applyPageFilters, matchesPageFilter } from '../pageFilters'

describe('page filters', () => {
  const rows = [
    { status: 200, method: 'GET', path: '/api/Users' },
    { status: 404, method: 'POST', path: '/missing' },
    { status: '500', method: 'GET', path: null },
  ]

  it('compares numbers, including numeric strings', () => {
    expect(applyPageFilters(rows, [{ column: 'status', operator: '>=', value: 400 }]).map(row => row.status)).toEqual([404, '500'])
    expect(applyPageFilters(rows, [{ column: 'status', operator: '!=', value: 200 }])).toHaveLength(2)
    expect(matchesPageFilter({ status: 'n/a' }, { column: 'status', operator: '>', value: 1 })).toBe(false)
  })

  it('matches text exactly, or as a case-insensitive substring', () => {
    expect(applyPageFilters(rows, [{ column: 'method', operator: '=', value: 'GET' }])).toHaveLength(2)
    expect(applyPageFilters(rows, [{ column: 'path', operator: '~', value: 'users' }])).toHaveLength(1)
    expect(applyPageFilters(rows, [{ column: 'path', operator: '!~', value: 'users' }])).toHaveLength(2)
  })

  it('keeps rows every filter matches', () => {
    expect(applyPageFilters(rows, [
      { column: 'method', operator: '=', value: 'GET' },
      { column: 'status', operator: '<', value: 300 },
    ])).toEqual([rows[0]])
  })
})

describe('header filters in the table', () => {
  afterEach(() => {
    localStorage.clear()
    document.body.innerHTML = ''
  })

  it('filters the loaded rows without emitting a query change, and clears', async () => {
    const onDrillDown = vi.fn()
    const host = document.createElement('div')
    document.body.appendChild(host)
    const app = createApp(DataTable, {
      sourceId: 'page-filter-test',
      teamId: null,
      columns: [{ name: 'timestamp', type: 'DateTime64(3)' }, { name: 'method', type: 'String' }],
      data: [
        { timestamp: '2026-01-01T10:00:00.000Z', method: 'GET' },
        { timestamp: '2026-01-01T10:00:01.000Z', method: 'POST' },
        { timestamp: '2026-01-01T10:00:02.000Z', method: 'GET' },
      ],
      onDrillDown,
    })
    app.use(createPinia())
    app.use(i18n)
    app.mount(host)
    try {
      await nextTick()
      const bodyRows = () => host.querySelectorAll('tbody tr[data-row-index], tbody > tr').length
      const before = bodyRows()
      const trigger = [...host.querySelectorAll<HTMLButtonElement>('[data-column-filter]')].find(button => button.getAttribute('aria-label')?.includes('method'))
      if (!trigger) throw new Error('Missing method header filter')
      trigger.click()
      await nextTick()
      await nextTick()
      const input = document.body.querySelector<HTMLInputElement>('[data-testid="column-filter-value"]')!
      input.value = 'POST'
      input.dispatchEvent(new Event('input'))
      await nextTick()
      document.body.querySelector<HTMLButtonElement>('button[type=submit]')!.click()
      await nextTick()
      await nextTick()
      expect(host.querySelector('[data-testid="page-filter-count"]')?.textContent).toBe('1 of 3 rows')
      expect(bodyRows()).toBeLessThan(before)
      expect(onDrillDown).not.toHaveBeenCalled()
      host.querySelector<HTMLButtonElement>('[data-testid="page-filter-clear-all"]')!.click()
      await nextTick()
      await nextTick()
      expect(host.querySelector('[data-testid="page-filters"]')).toBeNull()
      expect(bodyRows()).toBe(before)
    } finally {
      app.unmount()
    }
  })
})
