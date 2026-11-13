import { createApp, nextTick, type Component } from 'vue'
import { i18n } from '@/i18n'
import { createPinia } from 'pinia'
import { afterEach, expect, it, vi } from 'vitest'
import DataTable from '../data-table.vue'
import CompactLogList from '../CompactLogListSimple.vue'

const COLUMNS = [
  { name: 'timestamp', type: 'DateTime64(3)' },
  { name: 'msg', type: 'String' },
  { name: 'job_name', type: 'String' },
]
const ROWS = [
  { timestamp: '2026-01-01T10:00:00.123Z', msg: 'request served', job_name: 'checkout-api' },
  { timestamp: '2026-01-01T10:00:00.456Z', msg: 'cache miss', job_name: 'search-worker' },
]

function mount(component: Component, props: Record<string, unknown>) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(component, { sourceId: 'loaded-rows-test', teamId: null, columns: COLUMNS, data: ROWS, ...props })
  app.use(createPinia())
  app.use(i18n)
  app.mount(host)
  return { host, unmount: () => { app.unmount(); host.remove() } }
}

async function search(host: HTMLElement, term: string) {
  const input = host.querySelector<HTMLInputElement>('input[placeholder="Search loaded rows"]')
  if (!input) throw new Error('Loaded-rows search was not rendered')
  input.value = term
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  await nextTick()
}

afterEach(() => localStorage.clear())

it.each([
  ['table', DataTable],
  ['compact', CompactLogList],
])('labels the %s search as loaded rows and adds the term to the query as a contains filter', async (_name, component) => {
  const onDrillDown = vi.fn()
  const { host, unmount } = mount(component, { onDrillDown })
  try {
    await nextTick()
    expect(host.querySelector('[data-add-search-to-query]')).toBeNull()
    await search(host, 'cache')
    const button = host.querySelector<HTMLButtonElement>('[data-add-search-to-query]')
    if (!button) throw new Error('Add to query was not offered')
    button.click()
    expect(onDrillDown).toHaveBeenCalledExactlyOnceWith({ column: 'msg', operator: '~', value: 'cache' })
  } finally {
    unmount()
  }
})

it('does not offer adding the search to a SQL query', async () => {
  const { host, unmount } = mount(DataTable, { activeMode: 'clickhouse-sql' })
  try {
    await nextTick()
    await search(host, 'cache')
    expect(host.querySelector('[data-add-search-to-query]')).toBeNull()
  } finally {
    unmount()
  }
})

it('shows milliseconds in compact timestamps and the stored prefix field before each line', async () => {
  localStorage.setItem('logchef.explore.compactPrefix.loaded-rows-test', 'job_name')
  const { host, unmount } = mount(CompactLogList, {})
  try {
    await nextTick()
    const prefixes = [...host.querySelectorAll('[data-row-prefix]')].map(element => element.textContent)
    expect(prefixes).toEqual(['checkout-api', 'search-worker'])
    const first = host.querySelector('[data-row-id]')
    expect(first?.textContent).toMatch(/\d{2}:\d{2}:\d{2}\.123/)
    // The prefix field is not repeated in the logfmt tail.
    expect(first?.textContent).not.toContain('job_name=')
  } finally {
    unmount()
  }
})
