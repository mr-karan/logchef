import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createApp, defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { sourcesApi, type Source } from '@/api/sources'
import { teamsApi } from '@/api/teams'
import { exploreApi } from '@/api/explore'
import { useExplorerNavigator } from '@/composables/useExplorerNavigator'
import { useExploreStore } from '../explore'

const source: Source = {
  id: 3, name: 'Loki', source_type: 'loki', query_languages: ['logql'], capabilities: ['schema_inspection', 'source_inspection'],
  connection: { base_url: 'http://loki.test:3100', selector: '{service_name=~".+"}' },
  _meta_ts_field: '_time', _meta_is_auto_created: false, ttl_days: 0,
  created_at: '', updated_at: '', is_connected: true, columns: [{ name: '_time', type: 'DateTime' }],
}
const ok = <T>(data: T) => ({ status: 'success' as const, data })

describe('native LogQL runs', () => {
  let pinia: ReturnType<typeof createPinia>
  let app: ReturnType<typeof createApp> | undefined

  beforeEach(() => {
    localStorage.clear()
    pinia = createPinia()
    setActivePinia(pinia)
  })
  afterEach(() => {
    app?.unmount()
    app = undefined
    disposePinia(pinia)
    vi.restoreAllMocks()
  })

  it.each([
    ['', '{service_name=~".+"}'],
    ['{service_name="payments-api"}', '{service_name="payments-api"}'],
  ])('runs %j as %s with the selected limit', async (text, sent) => {
    vi.spyOn(teamsApi, 'listUserTeams').mockResolvedValue(ok([
      { id: 1, name: 'Team', description: '', created_at: '', updated_at: '', role: 'member', member_count: 1 },
    ]))
    vi.spyOn(sourcesApi, 'listTeamSources').mockResolvedValue(ok([source]))
    vi.spyOn(sourcesApi, 'getTeamSource').mockResolvedValue(ok(source))
    const run = vi.spyOn(exploreApi, 'getLogs').mockResolvedValue(ok({ logs: [], columns: [], stats: { execution_time_ms: 0, rows_read: 0, bytes_read: 0 } }))
    let navigator: ReturnType<typeof useExplorerNavigator> | undefined
    const Explorer = defineComponent({ setup() { navigator = useExplorerNavigator(); return () => null } })
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/logs/explore', component: Explorer }] })
    await router.push({ path: '/logs/explore', query: { team: '1', source: '3', mode: 'native', sql: text, limit: '250' } })
    app = createApp({ render: () => h(RouterView) }).use(pinia).use(router)
    app.mount(document.createElement('div'))
    await vi.waitFor(() => expect(navigator?.status.value).toBe('ready'))
    if (!run.mock.calls.length) await navigator?.run()
    await vi.waitFor(() => expect(run).toHaveBeenCalledTimes(1))
    expect(run.mock.calls[0][1]).toMatchObject({ query_text: sent, limit: 250 })
    expect(useExploreStore().templateDialect).toBe('logql')
  })
})
