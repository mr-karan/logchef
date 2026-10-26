import { createApp, h, nextTick, reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { i18n } from '@/i18n'
import { usePreferencesStore } from '@/stores/preferences'
import { afterEach, expect, it } from 'vitest'
import LiveTailPanel from '../LiveTailPanel.vue'

const originalTZ = process.env.TZ

afterEach(() => {
  if (originalTZ === undefined) delete process.env.TZ
  else process.env.TZ = originalTZ
  localStorage.clear()
})

function mountPanel(props: () => { rows: Record<string, unknown>[]; timestampField?: string }) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const host = document.createElement('div')
  const app = createApp({ render: () => h(LiveTailPanel, {
    status: 'streaming', notice: null, droppedCount: 0, endReason: null, error: null, ...props(),
  }) })
  app.use(pinia).use(i18n).mount(host)
  return { app, host, preferences: usePreferencesStore() }
}

it('shows live timestamps in the zone the user picked', async () => {
  process.env.TZ = 'Asia/Kolkata'
  const { app, host, preferences } = mountPanel(() => ({
    rows: [{ timestamp: '2026-01-01T00:00:00Z', message: 'hello' }],
    timestampField: 'timestamp',
  }))
  try {
    await preferences.updatePreferences({ timezone: 'local' })
    await nextTick()
    expect(host.textContent).toContain('2026-01-01T05:30:00.000+05:30')

    await preferences.updatePreferences({ timezone: 'utc' })
    await nextTick()
    expect(host.textContent).toContain('2026-01-01T00:00:00.000Z')
  } finally { app.unmount() }
})

it('reuses existing row elements across prepends, including duplicate content and a full buffer', async () => {
  const duplicateA = { timestamp: '2026-01-01T00:00:00Z', message: 'Duplicate' }
  const duplicateB = { ...duplicateA }
  const state = reactive({ rows: [duplicateA, duplicateB, ...Array.from({ length: 498 }, (_, id) => ({ message: `Row ${id}` }))] })
  const { app, host } = mountPanel(() => ({ rows: state.rows }))
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
