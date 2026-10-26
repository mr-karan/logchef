import { onScopeDispose, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { exploreApi } from '@/api/explore'
import type { Source } from '@/api/sources'
import { savedQueriesApi, type SavedQuery, type SavedQueryContent } from '@/api/savedQueries'
import { teamsApi } from '@/api/teams'
import { getErrorMessage, unwrap, type APIErrorResponse } from '@/api/types'
import { hasShownErrorToast, isAbortError } from '@/api/error-handler'
import { i18n } from '@/i18n'
import { useToast } from '@/composables/useToast'
import { TOAST_DURATION } from '@/lib/constants'
import { EXPLORE_PATH, parseExploreLocation, sameExploreLocation, type ExploreLocation } from '@/lib/exploreLocation'
import { pickSource, pickTeam, useContextStore } from '@/stores/context'
import { useExploreStore, type OpenedLocation, type RunQuery, type RunRequest } from '@/stores/explore'
import { useExploreRuns, type Execution } from '@/stores/exploreRuns'
import type { Paging } from '@/stores/exploreWindowed'
import { useSourcesStore } from '@/stores/sources'
import { useTeamsStore } from '@/stores/teams'
import type { TimeWindow } from '@/utils/timeWindow'

/** loading and error: nothing shown yet. navigating: a location shows while the next one loads. */
export type ExplorerStatus = 'loading' | 'error' | 'navigating' | 'ready'

function noTeamsMessage(error: APIErrorResponse | null): string {
  if (!error) return i18n.global.t('explore.noTeams')
  if (error.error_type === 'ServerError') return i18n.global.t('access.loadTeamsFailed')
  if (error.error_type === 'AuthenticationError') return i18n.global.t('explore.loginForTeams')
  return error.message || i18n.global.t('access.loadTeamsFailed')
}

const navigationRequest = { suppressErrorToast: true, suppressForbiddenRedirect: true }

function navigationError(error: unknown, resource: 'team' | 'source'): unknown {
  if (error && typeof error === 'object' && 'error_type' in error) {
    if (error.error_type === 'AuthorizationError') {
      return { ...error, message: i18n.global.t(resource === 'team' ? 'explore.teamAccessDenied' : 'explore.sourceAccessDenied') }
    }
    if (resource === 'team' && error.error_type === 'NotFoundError') {
      return { ...error, message: i18n.global.t('explore.teamNotFound') }
    }
  }
  return error
}

/**
 * The one owner of Explorer navigation. It opens every Explorer URL, runs at
 * most one query per navigation, is the only caller of the runs store and the
 * only writer of the Explorer URL. Other code changes what Explorer shows by
 * navigating to it.
 */
export function useExplorerNavigator() {
  const route = useRoute()
  const router = useRouter()
  const explore = useExploreStore()
  const runs = useExploreRuns()
  const context = useContextStore()
  const sources = useSourcesStore()
  const teams = useTeamsStore()
  const { toast } = useToast()

  const status = ref<ExplorerStatus>('loading')
  const error = ref<string | null>(null)
  const pending = ref<Pick<ExploreLocation, 'teamId' | 'sourceId'> | null>(null)

  // Every route change, window, limit and New query takes the next id, which supersedes
  // a navigation still loading or a Share or Save in flight.
  let navId = 0
  // The URL Explorer last recorded. Null until a location opens.
  let shown: Record<string, string> | null = null
  // A window or limit chosen while a navigation loads applies to it.
  let chosen: { window?: TimeWindow; limit?: number } = {}
  // A Run that showed results while Explorer could not record them.
  let unrecorded: Execution | null = null

  watch(() => route.fullPath, () => openRoute(), { immediate: true })
  // Nothing loading or running may record once Explorer is gone.
  onScopeDispose(() => {
    navId++
    runs.enter(null)
  })

  // Back to it, the echo of our own write, or a return while other pages left the context alone.
  function isShown() {
    const current = explore.urlQueryParameters
    return shown !== null && sameExploreLocation(route.query, shown) && current.team === shown.team && current.source === shown.source
  }

  function isHere() {
    return route.path === EXPLORE_PATH && status.value === 'ready' && isShown()
  }

  /** Stops the running query or a Run still validating. A pending navigation, Share or Save goes on. */
  function cancel() {
    runs.stop()
  }

  /** Opens the route's location unless Explorer already shows it. */
  async function openRoute(reopen = false) {
    if (!reopen && isHere()) {
      recordDeferred()
      return
    }
    const id = ++navId
    chosen = {}
    pending.value = null
    if (route.path !== EXPLORE_PATH) {
      if (status.value === 'navigating') status.value = 'ready'
      return
    }
    if (!reopen && isShown()) {
      status.value = 'ready'
      recordDeferred()
      return
    }
    status.value = shown ? 'navigating' : 'loading'
    error.value = null
    try {
      const target = parseExploreLocation(route.query)
      pending.value = target
      for (let attempt = 0; attempt < 3; attempt++) {
        try {
          const resolved = await resolve(target, id, attempt > 0)
          if (id !== navId) return
          if (!resolved) continue
          const { list, details, generation, sourceNotice, ...location } = resolved
          if (!sources.holdResolved(location.teamId, list, details, generation)) continue
          const shouldRun = explore.open(sourceNotice ? { ...location, content: { kind: 'draft' } } : location)
          applyChoice()
          status.value = 'ready'
          runs.enter(shouldRun && !sourceNotice ? editorAsk() : null)
          if (sourceNotice) toast({ title: i18n.global.t('explore.linkOpenFailed'), description: sourceNotice, variant: 'destructive', duration: TOAST_DURATION.ERROR })
          if (sourceNotice) void record('replace', { team: String(location.teamId), ...(location.sourceId === null ? {} : { source: String(location.sourceId) }) })
          else replay()
          return
        } catch (err) {
          if (id !== navId) return
          if (!isAbortError(err)) throw err
        }
      }
      throw new Error(i18n.global.t('explore.sourcesChanged'))
    } catch (err) {
      if (id !== navId) return
      console.error('error opening explorer location', err)
      chosen = {}
      const message = getErrorMessage(err)
      if (shown) {
        status.value = 'ready'
        await record('replace', shown)
        if (!hasShownErrorToast(err)) toast({ title: i18n.global.t('explore.linkOpenFailed'), description: message, variant: 'destructive', duration: TOAST_DURATION.ERROR })
      } else {
        error.value = message
        status.value = 'error'
      }
    } finally {
      if (id === navId) pending.value = null
    }
  }

  function recordDeferred() {
    const ran = unrecorded
    unrecorded = null
    if (ran && ran === runs.shown) void record('replace', explore.queryParameters(ran))
  }

  /** Loads everything the location needs and resolves its team and source. */
  async function resolve({ teamId, sourceId, target }: ExploreLocation, id: number, refresh = false): Promise<(OpenedLocation & { list: Source[]; details: Source | null; generation: number; sourceNotice: string | null }) | null> {
    // A link that names its team and source loads them while the teams load.
    const early = teamId === null || target.kind === 'share' || target.kind === 'saved' ? null : loadNamedSources(teamId, sourceId, refresh)
    if (teams.teams.length === 0) {
      const loaded = await teams.loadTeams(false, false)
      if (id !== navId) return null
      if (!loaded.success) {
        if (isAbortError(loaded.error)) throw loaded.error
        throw new Error(noTeamsMessage(teams.error))
      }
    }

    let content: OpenedLocation['content']
    if (target.kind === 'share') {
      const share = (await exploreApi.getQueryShare(target.token, navigationRequest).catch(error => { throw navigationError(error, 'source') })).data
      if (!share) throw new Error(i18n.global.t('explore.shareNotFound'))
      teamId = share.team_id
      sourceId = share.source_id
      content = { kind: 'share', share }
    } else if (target.kind === 'saved') {
      const saved = (await savedQueriesApi.resolve(target.queryId, teamId, navigationRequest).catch(error => { throw navigationError(error, 'source') })).data
      if (!saved) throw new Error(i18n.global.t('pages.savedQueryNotFound'))
      teamId = saved.resolved_team_id
      sourceId = saved.source_id
      const queryContent: SavedQueryContent = JSON.parse(saved.query_content)
      content = { kind: 'saved', saved, queryContent }
    } else {
      content = target
    }

    if (id !== navId) return null
    const team = teamId ?? pickTeam(teams.teams.map((t) => t.id), null)
    if (team === null) throw new Error(noTeamsMessage(teams.error))
    const generation = early?.generation ?? sources.getMetadataGeneration()
    const list = await (early?.list ?? sources.loadTeamSources(team, refresh, navigationRequest)).catch(error => { throw navigationError(error, 'team') })
    if (id !== navId || generation !== sources.getMetadataGeneration()) return null
    if (!teams.getTeamById(team)) {
      const confirmed = await teamsApi.getTeam(team, navigationRequest).catch(error => { throw navigationError(error, 'team') })
      if (!unwrap(confirmed)) throw new Error(i18n.global.t('explore.teamNotFound'))
    }
    if (id !== navId || generation !== sources.getMetadataGeneration()) return null
    const source = pickSource(team, list, sourceId)
    const details = source === null ? null : await (source === sourceId && early?.details ? early.details : loadDetails(team, source, refresh)).catch(error => { throw navigationError(error, 'source') })
    const sourceNotice = sourceId !== null && sourceId !== source && details
      ? i18n.global.t('explore.sourceUnavailableFallback', { sourceId, sourceName: details.name })
      : null
    return { teamId: team, sourceId: source, content, list, details, generation, sourceNotice }
  }

  function loadDetails(team: number, source: number, refresh: boolean) {
    return sources.loadSourceDetails(team, source, refresh || (team === context.teamId && source === context.sourceId && sources.currentSourceDetails?.is_connected === false), navigationRequest)
  }

  /** Starts loading a team's sources and a source's details. resolve reports their errors only if it uses them. */
  function loadNamedSources(teamId: number, sourceId: number | null, refresh: boolean) {
    const generation = sources.getMetadataGeneration()
    const list = sources.loadTeamSources(teamId, refresh, navigationRequest)
    const details = sourceId === null ? null : loadDetails(teamId, sourceId, refresh)
    list.catch(() => undefined)
    details?.catch(() => undefined)
    return { generation, list, details }
  }

  // Runs what last ran here, or the opened query, and records it. The URL
  // write comes last, so its echo equals what was recorded and opens nothing.
  function replay() {
    const execution = runs.replay()
    showDefaultSql(execution)
    void record('replace', explore.queryParameters(execution ?? explore.editorRun))
  }

  function editorAsk(): RunQuery {
    const query = explore.editorQuery
    return query.mode === 'native' && explore.showsDefaultSql ? { mode: 'native', text: '' } : query
  }

  function showDefaultSql(execution: Execution | null) {
    const asked = editorAsk()
    if (execution && execution.query !== execution.asked && asked.mode === 'native' && !asked.text.trim()) explore.showDefaultSql(execution.query.text)
  }

  function record(history: 'push' | 'replace', query: Record<string, string>) {
    shown = query
    if (!sameExploreLocation(route.query, query)) return router[history]({ path: EXPLORE_PATH, query })
  }

  /** Pushes a history entry for the team and runs its stored or first connected source's draft. */
  async function selectTeam(teamId: number) {
    if (teamId !== context.teamId) await router.push({ path: EXPLORE_PATH, query: { team: teamId } })
    else if (status.value === 'navigating' && shown) await router.push({ path: EXPLORE_PATH, query: shown })
  }

  /** Pushes a history entry for the source and runs its draft. */
  async function selectSource(sourceId: number) {
    if (sourceId !== context.sourceId) await router.push({ path: EXPLORE_PATH, query: { team: context.teamId ?? undefined, source: sourceId } })
    else if (status.value === 'navigating' && shown) await router.push({ path: EXPLORE_PATH, query: shown })
  }

  /** Pushes a history entry for the saved query and runs it. Reopens it when already shown. */
  async function openSavedQuery(saved: Pick<SavedQuery, 'id' | 'source_id'>) {
    const query = { team: context.teamId ?? undefined, source: saved.source_id, id: saved.id }
    if (sameExploreLocation(route.query, query)) return openRoute(true)
    await router.push({ path: EXPLORE_PATH, query })
  }

  /** Runs with the window and records it in the URL, even when unchanged. */
  function setWindow(window: TimeWindow) {
    chosen.window = window
    if (status.value === 'ready') commitChoice()
  }

  /** Runs with the limit and records it in the URL. */
  function setLimit(limit: number) {
    chosen.limit = limit
    if (status.value === 'ready') commitChoice()
  }

  function commitChoice() {
    navId++
    applyChoice()
    replay()
  }

  function applyChoice() {
    if (chosen.window) explore.setWindow(chosen.window)
    if (chosen.limit) explore.setLimit(chosen.limit)
    chosen = {}
  }

  /** Resets the editor to the defaults and runs them. */
  function newQuery() {
    if (status.value !== 'ready') return
    navId++
    explore.resetQueryToDefaults()
    runs.enter(explore.editorQuery)
    replay()
  }

  /**
   * Runs a Share or Save of the shown location, ignored while a navigation
   * loads. Unless a navigation started meanwhile, marks the location with
   * the result and records what the editor held when it began.
   */
  async function recordAfter<T>(request: () => Promise<T | null>, mark: (result: NoInfer<T>) => void): Promise<T | null> {
    if (status.value !== 'ready') return null
    const id = navId
    const captured: RunRequest = explore.editorRun
    const result = await request()
    if (result === null || id !== navId) return null
    mark(result)
    await record('replace', explore.queryParameters(captured))
    return result
  }

  /**
   * Runs the editor's query and pushes a history entry with what ran once it
   * shows results. Shown while the user is away, it records on their return.
   */
  function run() {
    if (status.value !== 'ready' || runs.isRunning) return
    const onShown = (ran: Execution) => {
      if (isHere()) void record('push', explore.queryParameters(ran))
      else unrecorded = ran
    }
    showDefaultSql(runs.run(editorAsk(), { validate: true, onShown }))
  }

  /** Pages the windowed search on screen, unless a navigation loads or a Run validates. */
  function page(action: Paging) {
    if (status.value === 'ready' && runs.current?.status !== 'validating') void runs.page(action)
  }

  return { status, error, pending, selectTeam, selectSource, openSavedQuery, setWindow, setLimit, newQuery, run, cancel, page, recordAfter, retry: () => openRoute() }
}
