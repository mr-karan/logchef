import { createApp, h, nextTick } from 'vue'
import { i18n } from '@/i18n'
import { createPinia } from 'pinia'
import { afterEach, expect, it } from 'vitest'
import DataTable from '../data-table.vue'

afterEach(() => {
  localStorage.clear()
  document.body.innerHTML = ''
})

function headerNames(host: HTMLElement): string[] {
  return [...host.querySelectorAll<HTMLElement>('thead th .truncate')].map(label => label.textContent ?? '')
}

it('hides a column from its header and enables header filters for LogsQL', async () => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const shown: string[][] = []
  const names = ['_time', '_msg', 'level', 'service']
  const app = createApp({
    render: () => h(DataTable, {
      columns: names.map(name => ({ name, type: 'String' })),
      data: [Object.fromEntries(names.map(name => [name, name === '_time' ? '2026-01-01T00:00:00Z' : `${name}-value`]))],
      sourceId: 'header-actions',
      teamId: 7,
      source: { source_type: 'victorialogs', capabilities: [] },
      timestampField: '_time',
      severityField: 'level',
      activeMode: 'native',
      filterLanguage: 'logsql',
      onShownColumns: (columns: string[]) => shown.push(columns),
    }),
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    const before = headerNames(host)
    expect(before).toContain('level')
    expect(shown.at(-1)).toEqual(expect.arrayContaining(['level']))
    expect(host.querySelector('[data-column-filter][aria-disabled="true"]')).toBeNull()

    const levelHeader = [...host.querySelectorAll('thead th')].find(th => th.querySelector('.truncate')?.textContent === 'level')
    levelHeader?.querySelector<HTMLButtonElement>('[data-hide-column]')?.click()
    await nextTick()
    expect(headerNames(host)).toEqual(before.filter(name => name !== 'level'))
    expect(shown.at(-1)).not.toContain('level')
  } finally {
    app.unmount()
  }
})
