import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAlertsStore } from '../alerts'
import { useAlertHistoryStore } from '../alertHistory'
import { controlledApi } from '@/test/controlledApi'

function alert(id: number) {
  return { id, source_id: id, name: `Alert ${id}`, updated_at: '2026-01-01', created_at: '2026-01-01' }
}
function history(alertId: number, id = alertId) {
  return { id, alert_id: alertId, status: 'triggered', created_at: '2026-01-01', triggered_at: '2026-01-01' }
}

describe('alert request ownership', () => {
  let network: ReturnType<typeof controlledApi>
  beforeEach(() => { setActivePinia(createPinia()); network = controlledApi() })
  afterEach(() => network.restore())

  it('keeps the latest source list after an older source responds', async () => {
    const store = useAlertsStore()
    const old = store.fetchAlerts(1)
    const current = store.fetchAlerts(2)
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    network.requests[1].succeed([alert(2)])
    await current
    network.requests[0].succeed([alert(1)])
    await old
    expect(store.alerts.map(a => a.id)).toEqual([2])
    expect(store.isLoading).toBe(false)
  })

  it('does not repopulate a cleared alert list', async () => {
    const store = useAlertsStore()
    const pending = store.fetchAlerts(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    store.clearAlerts()
    network.requests[0].succeed([alert(1)])
    await pending
    expect(store.alerts).toEqual([])
  })

  it.each([false, true])('keeps the latest history, including repeat requests for one alert (%s)', async sameAlert => {
    const store = useAlertHistoryStore()
    const old = store.loadHistory(1)
    const current = store.loadHistory(sameAlert ? 1 : 2)
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    network.requests[1].succeed([history(sameAlert ? 1 : 2, 20)])
    await current
    network.requests[0].succeed([history(1, 10)])
    await old
    expect(store.entries.map(e => e.id)).toEqual([20])
    expect(store.currentAlertId).toBe(sameAlert ? 1 : 2)
  })

  it('does not restore history after its context is cleared', async () => {
    const store = useAlertHistoryStore()
    const pending = store.loadHistory(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    store.setCurrentContext(null)
    network.requests[0].succeed([history(1)])
    await pending
    expect(store.entries).toEqual([])
  })

  it.each(['list', 'history'] as const)('does not publish an old %s failure after the newer request succeeds', async kind => {
    const store = kind === 'list' ? useAlertsStore() : useAlertHistoryStore()
    const load = kind === 'list'
      ? (id: number) => useAlertsStore().fetchAlerts(id)
      : (id: number) => useAlertHistoryStore().loadHistory(id)
    const old = load(1)
    const current = load(2)
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    network.requests[1].succeed(kind === 'list' ? [alert(2)] : [history(2)])
    await current
    network.requests[0].fail('Old request failed')
    await old
    expect(store.error).toBeNull()
    expect(store.isLoading).toBe(false)
  })

  it('does not resolve another alert history after switching context', async () => {
    const store = useAlertHistoryStore()
    const initial = store.loadHistory(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    network.requests[0].succeed([history(1)])
    await initial
    const resolving = store.resolveAlert(1, {})
    const current = store.loadHistory(2)
    await vi.waitFor(() => expect(network.requests).toHaveLength(3))
    network.requests[2].succeed([history(2)])
    await current
    network.requests[1].succeed({ message: 'Resolved' })
    await resolving
    expect(store.entries[0].status).toBe('triggered')
  })
})
