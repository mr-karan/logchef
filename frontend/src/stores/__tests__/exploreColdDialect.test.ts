import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { controlledApi } from '@/test/controlledApi'
import { useExploreStore } from '../explore'
import { useSourcesStore } from '../sources'
import { useContextStore } from '../context'
import { useVariableStore } from '../variables'
import type { Source } from '@/api/sources'

const source: Source = {
  id: 2, name: 'Logs', source_type: 'victorialogs',
  connection: { url: 'http://localhost:9428' },
  _meta_ts_field: '_time', _meta_is_auto_created: false, ttl_days: 7,
  created_at: '', updated_at: '', is_connected: true, columns: [{ name: "_time", type: "DateTime" }],
}
const query = 'service:my--svc [[ level:={{level}} ]]'

describe('cold template dialect hydration', () => {
  let pinia: ReturnType<typeof createPinia>
  let network: ReturnType<typeof controlledApi>
  beforeEach(() => {
    localStorage.clear()
    pinia = createPinia()
    setActivePinia(pinia)
    network = controlledApi()
    const context = useContextStore()
    context.selectTeam(1)
    context.selectSource(2)
  })
  afterEach(() => { disposePinia(pinia); network.restore() })

  it('uses saved LogsQL metadata before source details are available', () => {
    const store = useExploreStore()
    expect(useSourcesStore().currentSourceDetails).toBeNull()
    store.hydrateFromResolvedQuery({
      id: 1, name: 'Template', query_language: 'logsql', editor_mode: 'native',
      query_content: JSON.stringify({ content: query }),
    })
    expect(useVariableStore().allVariables).toMatchObject([{ name: 'level', isOptional: true }])
  })

  it.each(['url', 'share', 'draft'] as const)('detects native %s variables after cold source details arrive', async path => {
    const store = useExploreStore()
    const sources = useSourcesStore()
    expect(sources.currentSourceDetails).toBeNull()
    if (path === 'url') store.initializeFromUrl({ mode: 'native', sql: query })
    else if (path === 'share') store.hydrateFromQueryShare({
      token: 'template', source_id: 2, team_id: 1, share_url: '/s/template',
      created_at: '', expires_at: '', created_by: 1,
      payload: { version: 1, mode: 'native', query, limit: 100 },
    })
    else {
      localStorage.setItem('logchef.explore.draft.1.2', JSON.stringify({ version: 2, mode: 'native', nativeQuery: query }))
      store.initializeFromUrl({})
    }
    await vi.waitFor(() => expect(network.requests.length).toBeGreaterThan(0))
    for (const request of network.requests) request.succeed([source])
    await vi.waitFor(() => expect(sources.currentSourceDetails?.id).toBe(2))
    await nextTick()
    expect(useVariableStore().allVariables).toMatchObject([{ name: 'level', isOptional: true }])
    expect(store.nativeQuery).toBe(query)
    if (path === 'share') expect(store.activeShareToken).toBe('template')
  })
})
