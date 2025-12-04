import type { RouteLocationNormalized } from 'vue-router'
import { useContextStore } from '@/stores/context'
import { useTeamsStore } from '@/stores/teams'

/**
 * Keep the context store in sync with route params. The route remains the
 * single source of truth; we merely reflect it into stores and apply sensible
 * defaults without doing extra work such as fetching.
 */
export function contextRouterGuard(to: RouteLocationNormalized) {
  const contextStore = useContextStore()
  const teamsStore = useTeamsStore()

  // Parse team/source from params or query
  let teamId: number | null = null
  let sourceId: number | null = null

  const parseId = (value: unknown): number | null => {
    if (value == null) return null
    const parsed = parseInt(String(value), 10)
    return Number.isNaN(parsed) ? null : parsed
  }

  teamId = parseId(to.params.teamId) ?? parseId(to.query.team)
  sourceId = parseId(to.params.sourceId) ?? parseId(to.query.source)

  // If no team provided, fall back to the first known team (user or admin)
  if (!teamId) {
    const fallbackTeam = teamsStore.teams?.[0]
    if (fallbackTeam) {
      teamId = fallbackTeam.id
      console.log(`ContextGuard: defaulted team to ${teamId}`)
    }
  }

  // Keep old teams store in sync for legacy consumers
  if (teamId && teamsStore.currentTeamId !== teamId) {
    teamsStore.setCurrentTeam(teamId)
  }

  // Reflect into context store (allows nulls)
  contextStore.setFromRoute(teamId, sourceId)

  console.log(`ContextGuard: Route changed - team: ${teamId}, source: ${sourceId}`)
}
