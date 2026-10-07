import type { RouteLocationNormalized } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useContextStore } from '@/stores/context'
import { useTeamsStore } from '@/stores/teams'

export async function contextRouterGuard(to: Pick<RouteLocationNormalized, 'params' | 'query'>) {
  const authStore = useAuthStore()
  const contextStore = useContextStore()
  const teamsStore = useTeamsStore()

  const parseId = (value: unknown): number | null => {
    if (value == null) return null
    const parsed = parseInt(String(value), 10)
    return Number.isNaN(parsed) ? null : parsed
  }

  let teamId = parseId(to.params.teamId) ?? parseId(to.query.team)
  const sourceId = parseId(to.params.sourceId) ?? parseId(to.query.source)

  if (!teamId) {
    // A persisted team may have been revoked since it was saved. Before the
    // first selection, check it against fresh membership so the source loaders
    // never request a forbidden team. Global admins can open any team.
    if (contextStore.teamId === null && authStore.user?.role !== 'admin') {
      const result = await teamsStore.loadUserTeams()
      if (!result.success) return
      contextStore.restoreTeam(teamsStore.userTeams.map((team) => team.id))
      teamId = contextStore.teamId
    } else {
      teamId = contextStore.getStoredDefaults().teamId ?? teamsStore.teams?.[0]?.id ?? null
    }
  }

  contextStore.setFromRoute(teamId, sourceId)
}
