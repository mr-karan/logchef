import { useRoute, useRouter } from 'vue-router'
import { useContextStore } from '@/stores/context'
import { useTeamSourceContext } from '@/composables/useTeamSourceContext'

interface ApplyContextSelectionOptions {
  clearSource?: boolean
  syncUrl?: boolean
}

interface RouteContextOptions {
  allowMissingSource?: boolean
}

interface RouteContextSelection {
  teamId: number | null
  sourceId: number | null
}

export function useTeamSourceRouteSync(basePath?: string) {
  const route = useRoute()
  const router = useRouter()
  const contextStore = useContextStore()
  const teamSourceContext = useTeamSourceContext()

  async function syncUrlToContext(
    teamId: number | null = contextStore.teamId,
    sourceId: number | null = contextStore.sourceId
  ): Promise<void> {
    const query: Record<string, string> = {}

    if (teamId) {
      query.team = String(teamId)
    }

    if (sourceId) {
      query.source = String(sourceId)
    }

    const currentTeam = route.query.team
    const currentSource = route.query.source

    const needsUpdate =
      (query.team && currentTeam !== query.team) ||
      (query.source && currentSource !== query.source) ||
      (!query.source && currentSource) ||
      (!query.team && currentTeam)

    if (!needsUpdate) {
      return
    }

    await router.replace({ path: basePath ?? route.path, query })
  }

  async function clearSourceSelection(options: { syncUrl?: boolean } = {}): Promise<void> {
    contextStore.sourceId = null

    if (options.syncUrl) {
      await syncUrlToContext(contextStore.teamId, null)
    }
  }

  async function applyContextSelection(
    teamId: number,
    requestedSourceId: number | null,
    options: ApplyContextSelectionOptions = {}
  ): Promise<number | null> {
    const resolvedSourceId = await teamSourceContext.applyContextSelection(teamId, requestedSourceId)

    if (options.clearSource) {
      await clearSourceSelection({ syncUrl: false })
      if (options.syncUrl) {
        await syncUrlToContext(teamId, null)
      }
      return null
    }

    if (options.syncUrl) {
      await syncUrlToContext(teamId, resolvedSourceId)
    }

    return resolvedSourceId
  }

  async function applyRouteContext(options: RouteContextOptions = {}): Promise<RouteContextSelection> {
    const requestedTeamId = teamSourceContext.parseId(route.query.team)
    const requestedSourceId = teamSourceContext.parseId(route.query.source)
    const resolvedTeamId = teamSourceContext.resolveTeamId(requestedTeamId)

    if (!resolvedTeamId) {
      return { teamId: null, sourceId: null }
    }

    const clearSource = options.allowMissingSource && requestedSourceId == null
    const resolvedSourceId = await applyContextSelection(resolvedTeamId, requestedSourceId, {
      clearSource,
      syncUrl: false,
    })

    return { teamId: resolvedTeamId, sourceId: resolvedSourceId }
  }

  async function selectTeam(
    teamId: number,
    options: ApplyContextSelectionOptions = {}
  ): Promise<number | null> {
    return applyContextSelection(teamId, null, options)
  }

  async function selectSource(
    sourceId: number,
    options: { syncUrl?: boolean } = {}
  ): Promise<number | null> {
    const currentTeamId = contextStore.teamId
    if (!currentTeamId) {
      return null
    }

    return applyContextSelection(currentTeamId, sourceId, { syncUrl: options.syncUrl })
  }

  return {
    applyContextSelection,
    applyRouteContext,
    clearSourceSelection,
    selectTeam,
    selectSource,
    syncUrlToContext,
  }
}
