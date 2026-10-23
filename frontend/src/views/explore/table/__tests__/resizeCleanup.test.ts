import { createApp, h, nextTick } from 'vue'
import { createPinia } from 'pinia'
import { expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import DataTable from '../data-table.vue'

const dragEvents = ['mousemove', 'touchmove', 'mouseup', 'touchend', 'touchcancel']

it.each(['mousedown', 'touchstart'])('removes window drag listeners when unmounted after %s', async (eventType) => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp({ render: () => h(DataTable, {
    columns: [{ name: 'timestamp', type: 'DateTime' }, { name: 'message', type: 'String' }],
    data: [{ timestamp: '2026-01-01T00:00:00Z', message: 'hello' }],
    stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 32 },
    sourceId: 'resize-test', teamId: null,
  }) })
  app.use(createPinia())
  app.use(i18n)
  app.mount(host)
  await nextTick()
  const addListener = vi.spyOn(window, 'addEventListener')
  const removeListener = vi.spyOn(window, 'removeEventListener')
  const resizer = host.querySelector('thead .cursor-col-resize')
  if (!resizer) throw new Error('Column resizer was not rendered')
  resizer.dispatchEvent(eventType === 'mousedown'
    ? new MouseEvent('mousedown', { bubbles: true, clientX: 100 })
    : new TouchEvent('touchstart', { bubbles: true, touches: [] }))
  const listeners = addListener.mock.calls.filter(([type]) => dragEvents.includes(type))
  app.unmount()
  try {
    expect(listeners.map(([type]) => type).sort()).toEqual([...dragEvents].sort())
    for (const [type, listener] of listeners) {
      expect(removeListener).toHaveBeenCalledWith(type, listener)
    }
  } finally {
    for (const [type, listener] of listeners) window.removeEventListener(type, listener)
    vi.restoreAllMocks()
    host.remove()
  }
})
