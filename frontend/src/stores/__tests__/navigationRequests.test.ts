import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createApp, defineComponent, h, nextTick } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { controlledApi } from '@/test/controlledApi'
import { useSourcesStore } from '../sources'
import { useContextStore } from '../context'
import { useSavedQueriesStore } from '../savedQueries'
import { useExploreStore } from '../explore'
import { useVariableStore } from '../variables'
import { useSavedQueries } from '@/composables/useSavedQueries'
import { contextRouterGuard } from '@/router/contextGuard'

function source(id: number) {
  return { id, name: `Source ${id}`, columns: [{ name: 'timestamp', type: 'DateTime' }] }
}
function query(id: number, sourceId = id) {
  return { id, source_id: sourceId, name: `Query ${id}` }
}
const content = { version: 1, sourceId: 1, timeRange: null, limit: 100, content: '' }

describe('navigation request ownership', () => {
  let pinia: ReturnType<typeof createPinia>
  let network: ReturnType<typeof controlledApi>
  let app: ReturnType<typeof createApp> | undefined
  let root: HTMLElement | undefined

  async function settle(pending: Promise<unknown>, later: () => ReturnType<typeof controlledApi>['requests'], data: unknown) {
    let settled = false
    void pending.then(() => { settled = true })
    await vi.waitFor(() => {
      for (const request of later()) request.succeed(data)
      expect(settled).toBe(true)
    })
    await pending
  }

  beforeEach(() => {
    localStorage.clear()
    pinia = createPinia()
    setActivePinia(pinia)
    network = controlledApi(config => !!config.url?.match(/saved-queries|teams\/\d+\/sources$/))
  })
  afterEach(() => {
    app?.unmount()
    root?.remove()
    app = undefined
    root = undefined
    disposePinia(pinia)
    network.restore()
  })

  it('clears team loading when context is cleared during a request', async () => {
    const store = useSourcesStore()
    const context = useContextStore()
    context.selectTeam(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    context.clear()
    await nextTick()
    network.requests[0].succeed([source(1)])
    await vi.waitFor(() => expect(store.isLoadingOperation('loadTeamSources-1')).toBe(false))
    expect(store.isLoadingTeamSources).toBe(false)
  })

  async function openDeepLink(url: string) {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { render: () => h('div') } }] })
    router.beforeEach(contextRouterGuard)
    await router.push(url)
  }

  it.each([
    ['falls back to the remembered source for a stale', '/?team=1&source=99999', 3],
    ['remembers a valid', '/?team=1&source=2', 2],
  ])('%s deep-linked source', async (_name, url, expected) => {
    localStorage.setItem('logchef_context', JSON.stringify({ lastTeamId: 1, sourcePerTeam: { 1: 3 } }))
    const store = useSourcesStore()
    const context = useContextStore()
    await openDeepLink(url)
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    network.requests[0].succeed([source(2), source(3)])
    await vi.waitFor(() => expect(store.isLoadingTeamSources).toBe(false))
    await vi.waitFor(() => expect(context.getStoredSourceForTeam(1)).toBe(expected))
    expect(context.sourceId).toBe(expected)
  })

  it.each([
    ['another team', () => useContextStore().selectTeam(2), () => useContextStore().selectTeam(1)],
    ['no team', () => useContextStore().clear(), () => useContextStore().selectTeam(1)],
    ['a route without a team', () => useContextStore().setFromRoute(null, null), () => useContextStore().restoreTeam([1])],
  ])('finishes the team load when context visits %s and returns in one tick', async (_name, leave, back) => {
    const store = useSourcesStore()
    const context = useContextStore()
    context.selectTeam(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    leave()
    back()
    network.requests[0].succeed([source(1)])
    await vi.waitFor(() => expect(store.isLoadingTeamSources).toBe(false))
    expect(store.teamSources.map(s => s.id)).toEqual([1])
    expect(context.sourceId).toBe(1)
  })

  it('keeps the current team list when the previous team responds late', async () => {
    const store = useSourcesStore()
    const context = useContextStore()
    context.selectTeam(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    context.selectTeam(2)
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    network.requests[1].succeed([source(2)])
    await vi.waitFor(() => expect(store.teamSources.map(s => s.id)).toEqual([2]))
    network.requests[0].succeed([source(1)])
    await vi.waitFor(() => expect(store.isLoadingOperation('loadTeamSources-1')).toBe(false))
    expect(store.teamSources.map(s => s.id)).toEqual([2])
  })

  it('keeps a team loading while an older team request finishes', async () => {
    const store = useSourcesStore()
    const context = useContextStore()
    context.selectTeam(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    context.selectTeam(2)
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    network.requests[0].succeed([source(1)])
    await vi.waitFor(() => expect(store.isLoadingOperation('loadTeamSources-1')).toBe(false))
    expect(store.isLoadingTeamSources).toBe(true)
    expect(store.teamSources).toEqual([])
    network.requests[1].succeed([source(2)])
    await vi.waitFor(() => expect(store.isLoadingTeamSources).toBe(false))
  })

  it('hands a duplicate current-team load to the team watcher', async () => {
    const context = useContextStore()
    context.selectTeam(1)
    const store = useSourcesStore()
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    const duplicate = store.loadCurrentTeamSources()
    expect(store.isLoadingTeamSources).toBe(true)
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    network.requests[1].succeed([source(2)])
    expect((await duplicate).success).toBe(true)
    await vi.waitFor(() => expect(store.isLoadingTeamSources).toBe(false))
    expect(context.sourceId).toBe(2)
    expect(store.teamSources.map(s => s.id)).toEqual([2])
  })

  it.each(['source', 'team', 'clear'] as const)('does not restore saved queries after a %s context change', async change => {
    const context = useContextStore()
    context.selectTeam(1)
    context.selectSource(1)
    const store = useSavedQueriesStore()
    const pending = store.list(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    if (change === 'source') context.selectSource(2)
    else if (change === 'team') context.selectTeam(2)
    else context.clear()
    await nextTick()
    network.requests[0].succeed([query(1)])
    await pending
    expect(store.queries).toEqual([])
  })

  it('gives concurrent same-source callers the latest list', async () => {
    const store = useSavedQueriesStore()
    const first = store.list(1)
    const second = store.list(1)
    await vi.waitFor(() => expect(network.requests.length).toBeGreaterThan(0))
    for (const request of network.requests) request.succeed([query(1)])
    const results = await Promise.all([first, second])
    expect(results.every(result => result.success)).toBe(true)
    expect(store.queries.map(q => q.id)).toEqual([1])
  })

  it('keeps the saved-query response when context leaves and returns in one tick', async () => {
    const context = useContextStore()
    context.selectTeam(1)
    context.selectSource(1)
    const store = useSavedQueriesStore()
    const old = store.list(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    context.selectSource(2)
    context.selectSource(1)
    network.requests[0].succeed([query(1)])
    await old
    expect(store.queries.map(q => q.id)).toEqual([1])
  })

  it.each([
    ['a refresh', true],
    ['no refresh', false],
  ])('keeps a created query when an older list resolves after %s', async (_name, refresh) => {
    const context = useContextStore()
    context.selectTeam(1)
    context.selectSource(1)
    const store = useSavedQueriesStore()
    const stale = store.list(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    const created = store.create(1, 1, 'New', '', content, 'logchefql', 'builder')
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    network.requests[1].succeed(query(5, 1))
    expect((await created).success).toBe(true)
    const pending = Promise.all(refresh ? [stale, store.list(1)] : [stale])
    network.requests[0].succeed([])
    await settle(pending, () => network.requests.slice(2), [query(5, 1)])
    expect(store.queries.map(q => q.id)).toEqual([5])
  })

  it('ignores an old saved-query failure after a new list succeeds', async () => {
    const store = useSavedQueriesStore()
    const old = store.list(1)
    const current = store.list(2)
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    network.requests[1].succeed([query(2)])
    await current
    network.requests[0].fail('Old list failed')
    await old
    expect(store.queries.map(q => q.id)).toEqual([2])
    expect(store.error).toBeNull()
  })

  async function savedQueries() {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { render: () => h('div') } }] })
    let composable: ReturnType<typeof useSavedQueries> | undefined
    app = createApp(defineComponent({ setup() { composable = useSavedQueries(); return () => h('div') } }))
    app.use(pinia).use(router)
    await router.push('/')
    await router.isReady()
    root = document.createElement('div')
    document.body.append(root)
    app.mount(root)
    if (!composable) throw new Error('saved queries did not mount')
    return composable
  }

  it('does not repopulate the composable list after leaving its source', async () => {
    const queries = await savedQueries()
    const context = useContextStore()
    context.selectTeam(1)
    context.selectSource(1)
    await nextTick()
    const pending = queries.loadSourceQueries(1)
    await vi.waitFor(() => expect(network.requests.filter(r => r.config.url?.includes('saved-queries'))).toHaveLength(1))
    context.selectSource(2)
    await nextTick()
    network.requests.find(r => r.config.url?.includes('saved-queries'))?.succeed([query(1)])
    await pending
    expect(queries.queries.value).toEqual([])
    expect(queries.isLoading.value).toBe(false)
  })
  it('keeps the current composable request loading when an old source finishes', async () => {
    const queries = await savedQueries()
    const context = useContextStore()
    context.selectTeam(1)
    context.selectSource(1)
    await nextTick()
    const old = queries.loadSourceQueries(1)
    await vi.waitFor(() => expect(network.requests.filter(r => r.config.url?.includes('saved-queries'))).toHaveLength(1))
    context.selectSource(2)
    await nextTick()
    const current = queries.loadSourceQueries(2)
    await vi.waitFor(() => expect(network.requests.filter(r => r.config.url?.includes('saved-queries'))).toHaveLength(2))
    const requests = network.requests.filter(r => r.config.url?.includes('saved-queries'))
    requests[0].succeed([query(1)])
    await old
    expect(queries.isLoading.value).toBe(true)
    expect(queries.queries.value).toEqual([])
    requests[1].succeed([query(2)])
    await current
    expect(queries.queries.value.map(q => q.id)).toEqual([2])
    expect(queries.isLoading.value).toBe(false)
  })

  it('keeps composable queries when the dropdown loads the same source concurrently', async () => {
    const queries = await savedQueries()
    const first = queries.loadSourceQueries(1)
    const second = useSavedQueriesStore().list(1)
    await vi.waitFor(() => expect(network.requests.length).toBeGreaterThan(0))
    for (const request of network.requests) request.succeed([query(1)])
    const results = await Promise.all([first, second])
    expect(results.every(result => result.success)).toBe(true)
    expect(queries.queries.value.map(q => q.id)).toEqual([1])
  })

  it('shares a composable load between concurrent same-source callers', async () => {
    const queries = await savedQueries()
    const first = queries.loadSourceQueries(1)
    const second = queries.loadSourceQueries(1)
    await vi.waitFor(() => expect(network.requests.length).toBeGreaterThan(0))
    for (const request of network.requests) request.succeed([query(1)])
    const results = await Promise.all([first, second])
    expect(results.every(result => result.success)).toBe(true)
    expect(queries.queries.value.map(q => q.id)).toEqual([1])
  })

  it('shows a query saved while the source list is loading', async () => {
    const queries = await savedQueries()
    const context = useContextStore()
    context.selectTeam(1)
    context.selectSource(1)
    await nextTick()
    const savedQueryRequests = () => network.requests.filter(r => r.config.url?.includes('saved-queries'))
    const loading = queries.loadSourceQueries(1)
    await vi.waitFor(() => expect(savedQueryRequests()).toHaveLength(1))
    const saving = queries.handleSaveQuery({
      name: 'New', description: '', source_id: 1, created_from_team_id: 1,
      query_language: 'logchefql', editor_mode: 'builder', query_content: JSON.stringify(content),
    })
    await vi.waitFor(() => expect(savedQueryRequests()).toHaveLength(2))
    savedQueryRequests()[1].succeed(query(5, 1))
    savedQueryRequests()[0].succeed([])
    await settle(Promise.all([loading, saving]), () => savedQueryRequests().slice(2), [query(5, 1)])
    expect(queries.queries.value.map(q => q.id)).toEqual([5])
  })

  it('restores saved LogsQL variables through the composable without source details', async () => {
    const context = useContextStore()
    context.selectTeam(1)
    context.selectSource(2)
    const queries = await savedQueries()
    await queries.loadSavedQuery({
      id: 1, source_id: 2, name: 'Template', description: '',
      query_language: 'logsql', editor_mode: 'native', created_at: '', updated_at: '',
      query_content: JSON.stringify({ content: 'service:my--svc [[ level:={{level}} ]]' }),
    })
    await nextTick()
    expect(useExploreStore().templateDialect).toBe('logsql')
    expect(useVariableStore().allVariables).toMatchObject([{ name: 'level', isOptional: true }])
  })

})
