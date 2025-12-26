import type { RouteLocationNormalized } from 'vue-router'
import { useContextStore } from '@/stores/context'
import { useTeamsStore } from '@/stores/teams'
import { useSourcesStore } from '@/stores/sources'

export function contextRouterGuard(to: RouteLocationNormalized) {
  const contextStore = useContextStore()
  const teamsStore = useTeamsStore()
  const sourcesStore = useSourcesStore()

  const parseId = (value: unknown): number | null => {
    if (value == null) return null
    const parsed = parseInt(String(value), 10)
    return Number.isNaN(parsed) ? null : parsed
  }

  let teamId = parseId(to.params.teamId) ?? parseId(to.query.team)
  let sourceId = parseId(to.params.sourceId) ?? parseId(to.query.source)

  const storedDefaults = contextStore.getStoredDefaults()

  if (!teamId) {
    teamId = storedDefaults.teamId ?? teamsStore.teams?.[0]?.id ?? null
    console.log(`ContextGuard: No team in URL, using default: ${teamId}`)
  }

  if (!sourceId && teamId) {
    const storedSourceForTeam = contextStore.getStoredSourceForTeam(teamId)
    if (storedSourceForTeam) {
      sourceId = storedSourceForTeam
      console.log(`ContextGuard: Restored source ${sourceId} for team ${teamId}`)
    } else {
      const teamSources = sourcesStore.teamSources
      if (teamSources?.length > 0) {
        sourceId = teamSources[0].id
        console.log(`ContextGuard: Using first available source: ${sourceId}`)
      }
    }
  }

  contextStore.setFromRoute(teamId, sourceId)
  console.log(`ContextGuard: team=${teamId}, source=${sourceId}`)
}
