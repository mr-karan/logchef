import { createApp, nextTick } from 'vue'
import { i18n } from '@/i18n'
import { createPinia } from 'pinia'
import { expect, it, vi } from 'vitest'
import DataTable from '../data-table.vue'

const LONG_LINE = 'processed batch 42 for account placeholder after retrying upstream call to inventory service with backoff'

function mountTable(props: Record<string, unknown>) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(DataTable, { sourceId: 'cell-actions-test', teamId: null, ...props })
  app.use(createPinia())
  app.use(i18n)
  app.mount(host)
  return { host, unmount: () => { app.unmount(); host.remove() } }
}

async function openMenu(cell: Element) {
  cell.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 10, clientY: 10 }))
  await nextTick()
  await nextTick()
  return document.querySelector<HTMLElement>('[data-cell-menu]')
}

function menuItem(menu: HTMLElement, text: string) {
  const item = [...menu.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(element => element.textContent?.includes(text))
  if (!item) throw new Error(`Menu item ${text} was not rendered`)
  return item
}

it('keeps cell values clear on hover and expands the row on click instead of filtering', async () => {
  const onDrillDown = vi.fn()
  const { host, unmount } = mountTable({
    columns: [{ name: 'timestamp', type: 'DateTime' }, { name: 'msg', type: 'String' }],
    data: [{ timestamp: '2026-01-01T00:00:00Z', msg: LONG_LINE }],
    onDrillDown,
  })
  try {
    await nextTick()
    const cell = [...host.querySelectorAll('td[data-cell-id]')].find(td => td.textContent?.includes('processed batch'))
    if (!cell) throw new Error('Message cell was not rendered')
    cell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
    await nextTick()
    expect(cell.querySelectorAll('button')).toHaveLength(0)
    ;(cell as HTMLElement).click()
    await nextTick()
    expect(host.querySelector('.expanded-json-row')).not.toBeNull()
    expect(onDrillDown).not.toHaveBeenCalled()
  } finally {
    unmount()
  }
})

it('offers a contains filter for a long message value from the context menu', async () => {
  const onDrillDown = vi.fn()
  const { host, unmount } = mountTable({
    columns: [{ name: 'timestamp', type: 'DateTime' }, { name: 'msg', type: 'String' }, { name: 'job', type: 'String' }],
    data: [{ timestamp: '2026-01-01T00:00:00Z', msg: LONG_LINE, job: 'checkout-api' }],
    onDrillDown,
  })
  try {
    await nextTick()
    const cells = [...host.querySelectorAll('td[data-cell-id]')]
    const message = cells.find(td => td.textContent?.includes('processed batch'))
    if (!message) throw new Error('Message cell was not rendered')
    const menu = await openMenu(message)
    if (!menu) throw new Error('Context menu did not open')
    expect(menu.textContent).not.toContain('Filter = this value')
    menuItem(menu, 'Does not contain').click()
    await nextTick()
    expect(onDrillDown).toHaveBeenCalledExactlyOnceWith({
      column: 'msg', operator: '!~', value: 'processed batch 42 for account placeholder after retrying',
    })

    const job = cells.find(td => td.textContent?.includes('checkout-api'))
    if (!job) throw new Error('Job cell was not rendered')
    const shortMenu = await openMenu(job)
    if (!shortMenu) throw new Error('Context menu did not open')
    menuItem(shortMenu, 'Filter = this value').click()
    await nextTick()
    expect(onDrillDown).toHaveBeenLastCalledWith({ column: 'job', operator: '=', value: 'checkout-api' })
  } finally {
    unmount()
  }
})

it('leaves the browser menu alone outside a value cell', async () => {
  const { host, unmount } = mountTable({
    columns: [{ name: 'timestamp', type: 'DateTime' }, { name: 'msg', type: 'String' }],
    data: [{ timestamp: '2026-01-01T00:00:00Z', msg: 'short' }],
  })
  try {
    await nextTick()
    const expander = host.querySelector('tbody tr td:not([data-cell-id])')
    if (!expander) throw new Error('Expander cell was not rendered')
    expect(await openMenu(expander)).toBeNull()
  } finally {
    unmount()
  }
})

it('copies the raw identifier from its middle-truncated cell', async () => {
  const value = '0123456789abcdef0123456789abcdef'
  const writeText = vi.fn<(text: string) => Promise<void>>().mockResolvedValue(undefined)
  const clipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(DataTable, {
    columns: [{ name: 'timestamp', type: 'DateTime' }, { name: 'value', type: 'String' }],
    data: [{ timestamp: '2026-01-01T00:00:00Z', value }],
    sourceId: 'identifier-copy-test', teamId: null,
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    const cell = host.querySelector('.table-identifier')?.closest('td')
    if (!cell) throw new Error('Identifier cell was not rendered')
    const menu = await openMenu(cell)
    if (!menu) throw new Error('Context menu did not open')
    menuItem(menu, 'Copy value').click()
    await nextTick()
    expect(writeText).toHaveBeenCalledExactlyOnceWith(value)
  } finally {
    app.unmount()
    host.remove()
    if (clipboard) Object.defineProperty(navigator, 'clipboard', clipboard)
    else Reflect.deleteProperty(navigator, 'clipboard')
  }
})
