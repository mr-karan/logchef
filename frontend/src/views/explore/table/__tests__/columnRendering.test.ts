import { createApp, h, nextTick, reactive } from 'vue'
import { createPinia } from 'pinia'
import { expect, it } from 'vitest'
import DataTable from '../data-table.vue'

it('updates literal highlights and query markers without changing columns', async () => {
  const state = reactive<{
    queryFields: string[];
    regexHighlights: Record<string, { pattern: string; isNegated: boolean }>;
  }>({
    queryFields: [],
    regexHighlights: { body: { pattern: 'a.b', isNegated: false } },
  })
  const host = document.createElement('div')
  document.body.appendChild(host)
  const columns = [{ name: 'timestamp', type: 'DateTime64(9)' }, { name: 'body', type: 'String' }]
  const data = [
    { timestamp: '2026-01-01T00:00:00.8Z', body: 'a.b aXb second' },
    { timestamp: '2026-01-01T00:00:00.760000001Z', body: 'a.b second' },
  ]
  const app = createApp({ render: () => h(DataTable, {
    columns,
    data,
    queryFields: state.queryFields,
    regexHighlights: state.regexHighlights,
    stats: { execution_time_ms: 1, rows_read: 2, bytes_read: 64 },
    sourceId: 'highlight-test', teamId: null,
  }) })
  app.use(createPinia())
  try {
    app.mount(host)
    await nextTick()
    expect([...host.querySelectorAll('.search-highlight')].map(node => node.textContent)).toEqual(['a.b', 'a.b'])
    state.regexHighlights = { body: { pattern: 'second', isNegated: false } }
    state.queryFields = ['body']
    await nextTick()
    expect([...host.querySelectorAll('.search-highlight')].map(node => node.textContent)).toEqual(['second', 'second'])
    expect(host.querySelector('[title="This column is referenced in your query"]')).not.toBeNull()
    state.regexHighlights = { body: { pattern: 'second', isNegated: true } }
    await nextTick()
    expect(host.querySelectorAll('.search-highlight')).toHaveLength(0)
    const header = host.querySelector<HTMLElement>('thead [title="timestamp"]')
    if (!header) throw new Error('Timestamp header was not rendered')
    header.click()
    await nextTick()
    expect(host.querySelector('tbody tr td:nth-child(2) span')?.getAttribute('title')).toBe('2026-01-01T00:00:00.760000001Z')
  } finally {
    app.unmount()
    host.remove()
  }
})
