import { createApp, nextTick } from 'vue'
import { i18n } from '@/i18n'
import { createPinia } from 'pinia'
import { expect, it } from 'vitest'
import DataTable from '../data-table.vue'

it('mounts cell actions only for the hovered or focused cell', async () => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(DataTable, {
    columns: [{ name: 'timestamp', type: 'DateTime' }, { name: 'body', type: 'String' }, { name: 'level', type: 'String' }],
    data: [
      { timestamp: '2026-01-01T00:00:00Z', body: 'first', level: 'info' },
      { timestamp: '2026-01-01T00:00:01Z', body: 'second', level: 'warn' },
    ],
    stats: { execution_time_ms: 1, rows_read: 2, bytes_read: 64 },
    sourceId: 'cell-actions-test',
    teamId: null,
  })
  app.use(createPinia())
  app.use(i18n)
  try {
    app.mount(host)
    await nextTick()
    const cells = [...host.querySelectorAll<HTMLTableCellElement>('td[data-cell-id]')]
    expect(cells).toHaveLength(6)
    expect(host.querySelectorAll('.cell-actions')).toHaveLength(0)

    const bodyCell = cells.find(cell => cell.textContent?.includes('first'))
    if (!bodyCell) throw new Error('Body cell was not rendered')
    const inner = bodyCell.querySelector('span') ?? bodyCell
    inner.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
    await nextTick()
    expect(host.querySelectorAll('.cell-actions')).toHaveLength(1)
    expect(bodyCell.querySelectorAll('.cell-actions button')).toHaveLength(3)

    const timestampCell = cells[0]
    timestampCell.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
    await nextTick()
    expect(host.querySelectorAll('.cell-actions')).toHaveLength(1)
    expect(timestampCell.querySelectorAll('.cell-actions button')).toHaveLength(1)

    host.querySelector('tbody')?.dispatchEvent(new MouseEvent('mouseleave'))
    await nextTick()
    expect(host.querySelectorAll('.cell-actions')).toHaveLength(0)

    bodyCell.focus()
    await nextTick()
    const buttons = bodyCell.querySelectorAll<HTMLButtonElement>('.cell-actions button')
    expect(buttons).toHaveLength(3)
    buttons[1]?.focus()
    await nextTick()
    expect(bodyCell.querySelectorAll('.cell-actions button')).toHaveLength(3)
    buttons[1]?.blur()
    await nextTick()
    expect(host.querySelectorAll('.cell-actions')).toHaveLength(0)
  } finally {
    app.unmount()
    host.remove()
  }
})
