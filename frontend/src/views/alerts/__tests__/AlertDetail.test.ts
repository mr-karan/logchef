import { createApp, h, nextTick } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import { controlledApi } from '@/test/controlledApi'
import type { Alert, AlertHistoryEntry } from '@/api/alerts'
import { useAlertHistoryStore } from '@/stores/alertHistory'
import AlertDetail from '../AlertDetail.vue'

function alert(id: number): Alert {
  return {
    id, source_id: 1, name: `Alert ${id}`, query_language: 'clickhouse-sql', editor_mode: 'native',
    query: 'SELECT count(*) FROM logs', lookback_seconds: 60, threshold_operator: 'gt',
    threshold_value: 0, frequency_seconds: 60, severity: 'warning',
    destination_ids: [], no_data_policy: 'zero', notify_on_resolve: true, is_active: true, last_state: 'firing',
    created_at: '2026-01-01', updated_at: '2026-01-01',
  }
}

function history(id: number, message: string): AlertHistoryEntry[] {
  return [{
    id, alert_id: id, group_key: '', status: 'triggered', message,
    triggered_at: '2026-01-01T00:00:00Z', created_at: '2026-01-01T00:00:00Z',
  }]
}

describe('AlertDetail route changes', () => {
  let network: ReturnType<typeof controlledApi>
  let unmount: () => void
  let host: HTMLDivElement
  let historyStore: ReturnType<typeof useAlertHistoryStore>
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/alerts', name: 'AlertsOverview', component: { render: () => null } }, { path: '/alerts/:alertID', component: AlertDetail }],
  })

  beforeEach(async () => {
    network = controlledApi(config => /^\/alerts\//.test(config.url ?? ''))
    await router.push('/alerts/1?tab=history')
    const pinia = createPinia()
    host = document.createElement('div')
    document.body.appendChild(host)
    const app = createApp({ render: () => h(AlertDetail) })
    app.use(pinia).use(i18n).use(router).mount(host)
    historyStore = useAlertHistoryStore(pinia)
    unmount = () => { app.unmount(); host.remove() }
  })
  afterEach(() => { unmount(); network.restore() })

  function request(path: string) {
    const pending = network.requests.find(request => request.config.url?.split('?')[0] === path)
    if (!pending) throw new Error(`Missing request ${path}`)
    return pending
  }

  async function loadFirstHistory() {
    await vi.waitFor(() => expect(network.requests.some(r => r.config.url === '/alerts/1')).toBe(true))
    request('/alerts/1').succeed(alert(1))
    await vi.waitFor(() => expect(network.requests.some(r => r.config.url?.startsWith('/alerts/1/history'))).toBe(true))
    request('/alerts/1/history').succeed(history(1, 'Cached history'))
    await vi.waitFor(() => expect(host.textContent).toContain('Cached history'))
  }

  function selectedTab() {
    return host.querySelector('[role="tab"][aria-selected="true"]')?.textContent
  }

  it('follows the tab query when the alert changes', async () => {
    await loadFirstHistory()
    await router.push('/alerts/2?tab=edit')
    await vi.waitFor(() => expect(network.requests.some(r => r.config.url === '/alerts/2')).toBe(true))
    request('/alerts/2').succeed(alert(2))
    await vi.waitFor(() => expect(selectedTab()).toBe('Configuration'))
    expect(network.requests.filter(r => r.config.url?.startsWith('/alerts/2/history'))).toHaveLength(0)
  })

  it('follows query changes and reloads cached history on back and forward', async () => {
    await loadFirstHistory()
    await router.push('/alerts/1?tab=edit')
    await vi.waitFor(() => expect(selectedTab()).toBe('Configuration'))
    const previousRequests = network.requests.length

    router.back()
    await vi.waitFor(() => expect(selectedTab()).toBe('History'))
    await vi.waitFor(() => expect(network.requests.slice(previousRequests).filter(r => r.config.url?.startsWith('/alerts/1/history'))).toHaveLength(1))
    const reload = network.requests.slice(previousRequests).find(r => r.config.url?.startsWith('/alerts/1/history'))
    if (!reload) throw new Error('Missing history reload')
    reload.succeed(history(1, 'Fresh history'))
    await vi.waitFor(() => expect(host.textContent).toContain('Fresh history'))

    router.forward()
    await vi.waitFor(() => expect(selectedTab()).toBe('Configuration'))
  })

  it('writes tab clicks to the query and reloads existing history', async () => {
    await loadFirstHistory()
    const configuration = host.querySelector<HTMLButtonElement>('[role="tab"][data-state="inactive"]')
    if (!configuration) throw new Error('Missing configuration tab')
    configuration.dispatchEvent(new MouseEvent('mousedown', { button: 0, bubbles: true }))
    await vi.waitFor(() => expect(selectedTab()).toBe('Configuration'))
    expect(router.currentRoute.value.query).not.toHaveProperty('tab')
    const previousRequests = network.requests.length
    const historyTab = [...host.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find(tab => tab.textContent === 'History')
    if (!historyTab) throw new Error('Missing history tab')
    historyTab.dispatchEvent(new MouseEvent('mousedown', { button: 0, bubbles: true }))
    await vi.waitFor(() => expect(router.currentRoute.value.query.tab).toBe('history'))
    await vi.waitFor(() => expect(network.requests.slice(previousRequests).filter(r => r.config.url?.startsWith('/alerts/1/history'))).toHaveLength(1))
  })

  it('switches tabs without adding history entries', async () => {
    await loadFirstHistory()
    await router.push('/alerts/1?tab=history&marker=1')
    const configuration = [...host.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find(tab => tab.textContent === 'Configuration')
    if (!configuration) throw new Error('Missing configuration tab')
    configuration.dispatchEvent(new MouseEvent('mousedown', { button: 0, bubbles: true }))
    await vi.waitFor(() => expect(selectedTab()).toBe('Configuration'))

    router.back()
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe('/alerts/1?tab=history'))
  })

  it('shows the groups as loading until they arrive', async () => {
    await loadFirstHistory()
    const groupsCard = () => [...host.querySelectorAll('[data-slot="card"]')].find(card => card.textContent?.startsWith('Groups'))
    expect(groupsCard()?.textContent).not.toContain('No group is firing.')
    expect(groupsCard()?.querySelector('[data-slot="loading-state"]')).not.toBeNull()

    request('/alerts/1/instances').succeed([])
    await vi.waitFor(() => expect(groupsCard()?.textContent).toContain('No group is firing.'))
    expect(groupsCard()?.querySelector('[data-slot="loading-state"]')).toBeNull()
  })

  it('lists the groups and resolves one by its key', async () => {
    await loadFirstHistory()
    request('/alerts/1/instances').succeed([
      { alert_id: 1, group_key: '["checkout"]', group: { service: 'checkout' }, state: 'firing', value: 12, state_since: '2026-01-01T00:00:00Z' },
      { alert_id: 1, group_key: '["search"]', group: { service: 'search' }, state: 'firing', value: 30, state_since: '2026-01-01T00:00:00Z' },
    ])
    await vi.waitFor(() => expect(host.textContent).toContain('service=search'))
    const row = [...host.querySelectorAll('tr')].find(tr => tr.textContent?.includes('service=checkout'))
    const resolve = [...(row?.querySelectorAll('button') ?? [])].find(button => button.textContent?.trim() === 'Resolve')
    if (!resolve) throw new Error('Missing resolve action for checkout')
    resolve.click()
    await vi.waitFor(() => expect(network.requests.some(r => r.config.url === '/alerts/1/resolve')).toBe(true))
    expect(JSON.parse(String(request('/alerts/1/resolve').config.data))).toEqual({ message: 'Manually resolved', group_key: '["checkout"]' })
  })

  it('shows a no-data group and lets it be resolved, but not an ok group', async () => {
    await loadFirstHistory()
    request('/alerts/1/instances').succeed([
      { alert_id: 1, group_key: '["checkout"]', group: { service: 'checkout' }, state: 'no_data', state_since: '2026-01-01T00:00:00Z' },
      { alert_id: 1, group_key: '["search"]', group: { service: 'search' }, state: 'ok', value: 3, state_since: '2026-01-01T00:00:00Z' },
    ])
    await vi.waitFor(() => expect(host.textContent).toContain('service=search'))
    const rowOf = (service: string) => [...host.querySelectorAll('tr')].find(tr => tr.textContent?.includes(`service=${service}`))
    const resolveIn = (row?: Element) => [...(row?.querySelectorAll('button') ?? [])].find(button => button.textContent?.trim() === 'Resolve')
    expect(rowOf('checkout')?.textContent).toContain('No data')
    expect(resolveIn(rowOf('checkout'))).toBeDefined()
    expect(rowOf('search')?.textContent).toContain('OK')
    expect(resolveIn(rowOf('search'))).toBeUndefined()
  })

  it('leaves the tab out of the overview query', async () => {
    await loadFirstHistory()
    await router.push('/alerts/1?tab=history&source=3')
    const back = host.querySelector<HTMLButtonElement>('button[aria-label="Go back"]')
    if (!back) throw new Error('Missing back button')
    back.click()

    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('AlertsOverview'))
    expect(router.currentRoute.value.query).toEqual({ source: '3' })
  })

  it.each(['loaded', 'history', 'details'])('loads the next alert while mounted with old %s responses', async pending => {
    await vi.waitFor(() => expect(network.requests.some(r => r.config.url === '/alerts/1')).toBe(true))
    if (pending !== 'details') request('/alerts/1').succeed(alert(1))
    await vi.waitFor(() => expect(network.requests.some(r => r.config.url?.startsWith('/alerts/1/history'))).toBe(true))
    if (pending === 'loaded') {
      await new Promise(resolve => setTimeout(resolve, 0))
      for (const pending of network.requests.filter(r => r.config.url?.startsWith('/alerts/1/history'))) {
        pending.succeed(history(1, 'First alert history'))
      }
      await vi.waitFor(() => expect(host.textContent).toContain('First alert history'))
    }

    await router.push('/alerts/2?tab=history')
    await nextTick()
    await vi.waitFor(() => expect(network.requests.some(r => r.config.url === '/alerts/2')).toBe(true))
    request('/alerts/2').succeed(alert(2))
    await vi.waitFor(() => expect(network.requests.some(r => r.config.url?.startsWith('/alerts/2/history'))).toBe(true))
    request('/alerts/2/history').succeed(history(2, 'Second alert history'))
    await vi.waitFor(() => expect(host.textContent).toContain('Second alert history'))
    expect(host.textContent).toContain('Alert 2')
    expect(host.textContent).not.toContain('First alert history')

    if (pending !== 'loaded') {
      if (pending === 'details') request('/alerts/1').succeed(alert(1))
      for (const pending of network.requests.filter(r => r.config.url?.startsWith('/alerts/1/history'))) {
        pending.succeed(history(1, 'Late first alert history'))
      }
      await new Promise(resolve => setTimeout(resolve, 0))
      await nextTick()
      expect(host.textContent).not.toContain('Late first alert history')
      expect(host.textContent).toContain('Second alert history')
    }
    expect(historyStore.currentAlertId).toBe(2)
    expect(historyStore.entries.map(entry => entry.alert_id)).toEqual([2])
  })
})
