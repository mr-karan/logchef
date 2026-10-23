import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createApp, defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { sourcesApi, type Source } from '@/api/sources'
import { teamsApi } from '@/api/teams'
import { savedQueriesApi } from '@/api/savedQueries'
import { exploreApi } from '@/api/explore'
import { useExplorerNavigator } from '@/composables/useExplorerNavigator'
import { useExploreStore } from '../explore'
import { useSourcesStore } from '../sources'
import { useVariableStore } from '../variables'

const source: Source = {
  id: 2, name: 'Logs', source_type: 'victorialogs',
  connection: { url: 'http://logs.test' },
  _meta_ts_field: '_time', _meta_is_auto_created: false, ttl_days: 7,
  created_at: '', updated_at: '', is_connected: true, columns: [{ name: '_time', type: 'DateTime' }],
}
const query = 'service:my--svc [[ level:={{level}} ]]'
const ok = <T>(data: T) => ({ status: 'success' as const, data })

describe('cold template dialect hydration', () => {
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

  it.each(['url', 'share', 'draft', 'saved link', 'saved pick'] as const)(
    'detects native %s variables after cold source details arrive', async path => {
      let release: () => void = () => {}
      const details = new Promise<void>(resolve => { release = resolve })
      vi.spyOn(teamsApi, 'listUserTeams').mockResolvedValue(ok([
        { id: 1, name: 'Team', description: '', created_at: '', updated_at: '', role: 'member', member_count: 1 },
      ]))
      vi.spyOn(sourcesApi, 'listTeamSources').mockResolvedValue(ok([source]))
      const loadSource = vi.spyOn(sourcesApi, 'getTeamSource').mockImplementation(async () => {
        await details
        return ok(source)
      })
      vi.spyOn(savedQueriesApi, 'resolve').mockResolvedValue(ok({
        id: 1, source_id: 2, resolved_team_id: 1, name: 'Template', description: '',
        query_language: 'logsql', editor_mode: 'native', created_at: '', updated_at: '',
        query_content: JSON.stringify({ content: query }),
      }))
      vi.spyOn(exploreApi, 'getQueryShare').mockResolvedValue(ok({
        token: 'template', source_id: 2, team_id: 1, share_url: '/s/template',
        created_at: '', expires_at: '', created_by: 1,
        payload: { version: 1, mode: 'sql', query, limit: 100 },
      }))
      const run = vi.spyOn(exploreApi, 'getLogs').mockResolvedValue(ok({ logs: [], columns: [], stats: { execution_time_ms: 0, rows_read: 0, bytes_read: 0 } }))
      vi.spyOn(exploreApi, 'getHistogramData').mockResolvedValue(ok({ data: [], granularity: '1m' }))
      if (path === 'draft') localStorage.setItem('logchef.explore.draft.1.2', JSON.stringify({
        version: 2, mode: 'native', nativeQuery: query,
      }))
      const store = useExploreStore()
      let navigator: ReturnType<typeof useExplorerNavigator> | undefined
      const Explorer = defineComponent({ setup() { navigator = useExplorerNavigator(); return () => null } })
      const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/logs/explore', component: Explorer }] })
      await router.push({ path: '/logs/explore', query: {
        team: '1', source: '2',
        ...(path === 'url' ? { mode: 'native', sql: query } : {}),
        ...(path === 'share' ? { share: 'template' } : {}),
        ...(path === 'saved link' ? { id: '1' } : {}),
      } })
      app = createApp({ render: () => h(RouterView) }).use(pinia).use(router)
      app.mount(document.createElement('div'))
      if (!navigator) throw new Error('explorer did not mount')
      if (path === 'saved pick') await navigator.openSavedQuery({ id: 1, source_id: 2 })
      await vi.waitFor(() => expect(loadSource).toHaveBeenCalled())
      expect(useSourcesStore().currentSourceDetails).toBeNull()
      expect(run).not.toHaveBeenCalled()
      release()
      await vi.waitFor(() => expect(navigator?.status.value).toBe('ready'))
      await vi.waitFor(() => expect(run).toHaveBeenCalledTimes(1))
      expect(store.templateDialect).toBe('logsql')
      expect(useVariableStore().allVariables).toMatchObject([{ name: 'level', isOptional: true }])
      expect(store.nativeQuery).toBe(query)
      if (path === 'share') expect(store.activeShareToken).toBe('template')
    },
  )
})
