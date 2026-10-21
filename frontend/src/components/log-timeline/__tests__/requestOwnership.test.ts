import { createApp, h, nextTick, reactive } from 'vue'
import { createPinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import { controlledApi } from '@/test/controlledApi'
import LogTimelineModal from '../LogTimelineModal.vue'

function context(message: string) {
  return { before_logs: [{ timestamp: '2026-01-01T00:00:00Z', message }], after_logs: [] }
}

describe('log context request ownership', () => {
  let network: ReturnType<typeof controlledApi>
  let unmount: () => void
  let state: { isOpen: boolean; log: { timestamp: string } }
  const closed = vi.fn()
  beforeEach(() => {
    network = controlledApi()
    closed.mockClear()
    state = reactive({ isOpen: true, log: { timestamp: '2026-01-01T00:00:00Z' } })
    const host = document.createElement('div')
    document.body.appendChild(host)
    const app = createApp({ render: () => h(LogTimelineModal, {
      ...state, teamId: 1, sourceId: '1', 'onUpdate:isOpen': closed,
    }) })
    app.use(createPinia()).use(i18n).mount(host)
    unmount = () => { app.unmount(); host.remove() }
  })
  afterEach(() => { unmount(); network.restore() })

  async function reopen() {
    state.isOpen = false
    await nextTick()
    state.log = { timestamp: '2026-01-02T00:00:00Z' }
    state.isOpen = true
    await nextTick()
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
  }

  it.each(['success', 'failure'] as const)('ignores an old %s after another row opens', async outcome => {
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    await reopen()
    network.requests[1].succeed(context('Current context'))
    await vi.waitFor(() => expect(document.body.textContent).toContain('Current context'))
    if (outcome === 'success') network.requests[0].succeed(context('Stale context'))
    else network.requests[0].fail('Stale request failed')
    await new Promise(resolve => setTimeout(resolve, 0))
    await nextTick()
    expect(document.body.textContent).toContain('Current context')
    expect(document.body.textContent).not.toContain('Stale context')
    expect(closed).not.toHaveBeenCalled()
  })

  it('does not append old pagination into a newly opened row', async () => {
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    network.requests[0].succeed(context('First context'))
    await vi.waitFor(() => expect(document.body.textContent).toContain('First context'))
    const button = [...document.querySelectorAll('button')].find(button => button.textContent?.includes('Load 20 Before'))
    if (!button) throw new Error('Pagination button missing')
    button.click()
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    state.isOpen = false
    await nextTick()
    state.isOpen = true
    state.log = { timestamp: '2026-01-02T00:00:00Z' }
    await nextTick()
    await vi.waitFor(() => expect(network.requests).toHaveLength(3))
    network.requests[2].succeed(context('Current context'))
    await vi.waitFor(() => expect(document.body.textContent).toContain('Current context'))
    network.requests[1].succeed(context('Stale pagination'))
    await new Promise(resolve => setTimeout(resolve, 0))
    await nextTick()
    expect(document.body.textContent).not.toContain('Stale pagination')
    expect(document.body.textContent).toContain('Current context')
  })
})
