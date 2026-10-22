import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createApp, defineComponent, h, nextTick } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { controlledApi } from '@/test/controlledApi'
import { useSourcesStore } from '../sources'
import { useContextStore } from '../context'
import { useSavedQueriesStore } from '../savedQueries'
import { useSavedQueries } from '@/composables/useSavedQueries'

function source(id: number) {
  return { id, name: `Source ${id}`, columns: [{ name: 'timestamp', type: 'DateTime' }] }
}
function query(id: number, sourceId = id) {
  return { id, source_id: sourceId, name: `Query ${id}` }
}

describe('navigation request ownership', () => {
  let pinia: ReturnType<typeof createPinia>
  let network: ReturnType<typeof controlledApi>
  let app: ReturnType<typeof createApp> | undefined
  let root: HTMLElement | undefined

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

  it('keeps the newest request for the same team', async () => {
    const context = useContextStore()
    context.selectTeam(1)
    const store = useSourcesStore()
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    const latest = store.loadTeamSources(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    network.requests[1].succeed([source(2)])
    await latest
    network.requests[0].succeed([source(1)])
    await vi.waitFor(() => expect(store.isLoadingTeamSources).toBe(false))
    expect(store.teamSources.map(s => s.id)).toEqual([2])
  })

  it('does not let an unrelated team request supersede the selected team', async () => {
    useContextStore().selectTeam(1)
    const store = useSourcesStore()
    await vi.waitFor(() => expect(network.requests).toHaveLength(1))
    const unrelated = store.loadTeamSources(2)
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    network.requests[0].succeed([source(1)])
    await vi.waitFor(() => expect(store.teamSources.map(s => s.id)).toEqual([1]))
    network.requests[1].succeed([source(2)])
    await unrelated
    expect(store.teamSources.map(s => s.id)).toEqual([1])
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

  it('keeps the newest saved-query list for the same source', async () => {
    const store = useSavedQueriesStore()
    const old = store.list(1)
    const current = store.list(1)
    await vi.waitFor(() => expect(network.requests).toHaveLength(2))
    network.requests[1].succeed([query(2, 1)])
    await current
    network.requests[0].succeed([query(1)])
    await old
    expect(store.queries.map(q => q.id)).toEqual([2])
  })

  it('keeps saved queries empty when context leaves and returns during a request', async () => {
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
    expect(store.queries).toEqual([])
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
    expect(queries.queries.value?.map(q => q.id)).toEqual([2])
    expect(queries.isLoading.value).toBe(false)
  })

})
