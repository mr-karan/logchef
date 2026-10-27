import { createApp, h, nextTick, reactive } from 'vue'
import { createPinia } from 'pinia'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import DataTable from '../data-table.vue'
import { CONTENT_WIDTH_CAP } from '../contentSizing'

const storageKey = 'logchef-tableState-7-content-sizing'
const canvasDescriptor = Object.getOwnPropertyDescriptor(HTMLCanvasElement.prototype, 'getContext')
const measuredText: string[] = []
const cleanup: (() => void)[] = []

beforeEach(() => {
  localStorage.clear()
  measuredText.length = 0
  Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
    configurable: true,
    value: () => ({
      font: '',
      measureText: (text: string) => {
        measuredText.push(text)
        return { width: text.length * 8 }
      },
    }),
  })
})

afterEach(() => {
  cleanup.splice(0).forEach(stop => stop())
  if (canvasDescriptor) Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', canvasDescriptor)
  localStorage.clear()
})

function mountTable(data: Record<string, unknown>[]) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const state = reactive({ data })
  const columns = Object.keys(data[0] ?? {}).map(name => ({ name, type: 'String' }))
  const app = createApp({ render: () => h(DataTable, {
    columns,
    data: state.data,
    stats: { execution_time_ms: 1, rows_read: state.data.length, bytes_read: 64 },
    sourceId: 'content-sizing', teamId: 7,
    source: { source_type: 'clickhouse', capabilities: [] },
    timestampField: 'time',
  }) })
  app.use(createPinia())
  app.use(i18n)
  app.mount(host)
  let mounted = true
  const stop = () => {
    if (mounted) app.unmount()
    mounted = false
    host.remove()
  }
  cleanup.push(stop)
  function header(name: string): HTMLElement {
    const label = [...host.querySelectorAll('thead .truncate')].find(label => label.textContent === name)
    const element = label?.closest('th')
    if (!element) throw new Error(`Missing header ${name}`)
    return element
  }
  function width(name: string) {
    return parseFloat(header(name).style.width)
  }
  function resizer(name: string): HTMLElement {
    const element = header(name).querySelector<HTMLElement>('.cursor-col-resize')
    if (!element) throw new Error(`Missing resizer ${name}`)
    return element
  }
  return { host, state, stop, width, resizer }
}

it('renders rows immediately, shrinks short values, caps long values and keeps defaults out of storage', async () => {
  const table = mountTable([{ time: '2026-01-01T00:00:00Z', flag: 'ok', payload: 'x'.repeat(2000), end: 'end' }])
  expect(table.host.querySelector('td[data-cell-id]')).not.toBeNull()
  await vi.waitFor(() => expect(table.width('flag')).toBeLessThan(180))
  expect(table.width('payload')).toBe(CONTENT_WIDTH_CAP)
  expect(measuredText.every(text => text.length <= 320)).toBe(true)
  table.stop()
  expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnSizing ?? {}).toEqual({})
})

it('recomputes defaults for each result using only the first fifty rows', async () => {
  const table = mountTable(Array.from({ length: 51 }, (_, index) => ({
    time: '2026-01-01T00:00:00Z', flag: index === 50 ? 'ignored'.repeat(50) : 'ok', end: 'end',
  })))
  await vi.waitFor(() => expect(table.width('flag')).toBeLessThan(180))
  expect(measuredText.some(text => text.includes('ignored'))).toBe(false)
  table.state.data = [{ time: '2026-01-01T00:00:01Z', flag: 'long value '.repeat(60), end: 'end' }]
  await vi.waitFor(() => expect(table.width('flag')).toBe(CONTENT_WIDTH_CAP))
})

it.each(['drag', 'auto-fit'])('keeps a user %s width across a new result and reload', async action => {
  const table = mountTable([{ time: '2026-01-01T00:00:00Z', flag: 'ok', end: 'end' }])
  await vi.waitFor(() => expect(table.width('flag')).toBeLessThan(180))
  if (action === 'drag') {
    table.resizer('flag').dispatchEvent(new MouseEvent('mousedown', { bubbles: true, clientX: 100 }))
    window.dispatchEvent(new MouseEvent('mousemove', { clientX: 170 }))
    window.dispatchEvent(new MouseEvent('mouseup'))
  } else {
    table.resizer('flag').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
  }
  await nextTick()
  const userWidth = table.width('flag')
  table.state.data = [{ time: '2026-01-01T00:00:01Z', flag: 'long value '.repeat(60), end: 'end' }]
  await nextTick()
  expect(table.width('flag')).toBe(userWidth)
  table.stop()
  expect(JSON.parse(localStorage.getItem(storageKey) ?? '{}').columnSizing).toEqual({ flag: userWidth })
  const reloaded = mountTable(table.state.data)
  await nextTick()
  expect(reloaded.width('flag')).toBe(userWidth)
})

it('auto-fits the current model page with an 800 pixel cap beyond the automatic sample', async () => {
  const table = mountTable(Array.from({ length: 51 }, (_, index) => ({
    time: '2026-01-01T00:00:00Z', flag: index === 50 ? 'long value '.repeat(120) : 'ok', end: 'end',
  })))
  await vi.waitFor(() => expect(table.width('flag')).toBeLessThan(180))
  const next = [...table.host.querySelectorAll('button')].find(button => button.textContent?.includes('Go to next page'))
  if (!next) throw new Error('Next page button was not rendered')
  next.click()
  await nextTick()
  table.resizer('flag').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
  await nextTick()
  expect(table.width('flag')).toBe(800)
  const previous = [...table.host.querySelectorAll('button')].find(button => button.textContent?.includes('Go to previous page'))
  if (!previous) throw new Error('Previous page button was not rendered')
  previous.click()
  await nextTick()
  table.resizer('flag').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
  await nextTick()
  expect(table.width('flag')).toBeLessThan(180)
})

it('measures hidden columns only after they become visible', async () => {
  localStorage.setItem(storageKey, JSON.stringify({ columnVisibility: { flag: false } }))
  const table = mountTable([{ time: '2026-01-01T00:00:00Z', flag: 'hidden marker', end: 'end' }])
  await vi.waitFor(() => expect(table.width('time')).not.toBe(180))
  expect(measuredText).not.toContain('hidden marker')
  const columns = table.host.querySelector<HTMLButtonElement>('button[title="Select columns"]')
  if (!columns) throw new Error('Column selector was not rendered')
  columns.click()
  await nextTick()
  document.querySelector<HTMLButtonElement>('#flag')?.click()
  await vi.waitFor(() => expect(measuredText).toContain('hidden marker'))
})

it('waits for fonts before the first canvas measurement', async () => {
  const descriptor = Object.getOwnPropertyDescriptor(document, 'fonts')
  let finish: () => void = () => {}
  const ready = new Promise<void>(resolve => { finish = resolve })
  Object.defineProperty(document, 'fonts', { configurable: true, value: { ready } })
  try {
    const table = mountTable([{ time: '2026-01-01T00:00:00Z', flag: 'font marker', end: 'end' }])
    await new Promise(resolve => setTimeout(resolve, 50))
    expect(measuredText).toEqual([])
    finish()
    await vi.waitFor(() => expect(measuredText).toContain('font marker'))
    expect(table.width('flag')).toBeLessThan(180)
  } finally {
    if (descriptor) Object.defineProperty(document, 'fonts', descriptor)
    else Reflect.deleteProperty(document, 'fonts')
  }
})
