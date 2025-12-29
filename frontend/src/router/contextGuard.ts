import type { RouteLocationNormalized } from 'vue-router'
import { useContextStore } from '@/stores/context'
import { useTeamsStore } from '@/stores/teams'

export function contextRouterGuard(to: RouteLocationNormalized) {
  const contextStore = useContextStore()
  const teamsStore = useTeamsStore()

  const parseId = (value: unknown): number | null => {
    if (value == null) return null
    const parsed = parseInt(String(value), 10)
    return Number.isNaN(parsed) ? null : parsed
  }

  let teamId = parseId(to.params.teamId) ?? parseId(to.query.team)
  const sourceId = parseId(to.params.sourceId) ?? parseId(to.query.source)

  const storedDefaults = contextStore.getStoredDefaults()

  if (!teamId) {
    teamId = storedDefaults.teamId ?? teamsStore.teams?.[0]?.id ?? null
    console.log(`ContextGuard: No team in URL, using default: ${teamId}`)
  }

  contextStore.setFromRoute(teamId, sourceId)
  console.log(`ContextGuard: team=${teamId}, source=${sourceId}`)
}
