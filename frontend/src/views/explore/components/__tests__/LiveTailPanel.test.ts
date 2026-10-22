import { createApp, h, nextTick, reactive } from 'vue'
import { i18n } from '@/i18n'
import { expect, it } from 'vitest'
import LiveTailPanel from '../LiveTailPanel.vue'

it('reuses existing row elements across prepends, including duplicate content and a full buffer', async () => {
  const duplicateA = { timestamp: '2026-01-01T00:00:00Z', message: 'Duplicate' }
  const duplicateB = { ...duplicateA }
  const state = reactive({ rows: [duplicateA, duplicateB, ...Array.from({ length: 498 }, (_, id) => ({ message: `Row ${id}` }))] })
  const host = document.createElement('div')
  const app = createApp({ render: () => h(LiveTailPanel, {
    rows: state.rows, status: 'streaming', notice: null, droppedCount: 0, endReason: null, error: null,
  }) })
  app.use(i18n).mount(host)
  const viewport = host.querySelector('.overflow-y-auto')
  if (!viewport) throw new Error('Tail viewport missing')
  const first = viewport.children[0]
  const second = viewport.children[1]
  const third = viewport.children[2]
  try {
    state.rows = [{ message: 'New row' }, ...state.rows].slice(0, 500)
    await nextTick()
    expect(viewport.children[1]).toBe(first)
    expect(viewport.children[2]).toBe(second)
    expect(viewport.children[3]).toBe(third)
    expect(viewport.children).toHaveLength(500)
  } finally { app.unmount() }
})
