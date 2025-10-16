import type { RouteLocationNormalized } from 'vue-router'
import { useContextStore } from '@/stores/context'
import { useTeamsStore } from '@/stores/teams'

/**
 * Router guard that keeps context store in sync with route params.
 * 
 * Route is the single source of truth for team/source selection.
 * This guard updates the context store based on route params.
 */

export function contextRouterGuard(to: RouteLocationNormalized) {
  const contextStore = useContextStore()
  const teamsStore = useTeamsStore()
  
  // Parse team and source from route params/query
  let teamId: number | null = null
  let sourceId: number | null = null
  
  // Try route params first (for routes like /team/:teamId/source/:sourceId)
  if (to.params.teamId) {
    const parsed = parseInt(String(to.params.teamId))
    if (!isNaN(parsed)) teamId = parsed
  }
  
  if (to.params.sourceId) {
    const parsed = parseInt(String(to.params.sourceId))
    if (!isNaN(parsed)) sourceId = parsed
  }
  
  // Fallback to query params (for routes like /explore?team=1&source=2)
  if (!teamId && to.query.team) {
    const parsed = parseInt(String(to.query.team))
    if (!isNaN(parsed)) teamId = parsed
  }
  
  if (!sourceId && to.query.source) {
    const parsed = parseInt(String(to.query.source))
    if (!isNaN(parsed)) sourceId = parsed
  }
  
  // Auto-select first team for explore routes if no team specified and teams are available
  if (!teamId && to.path.startsWith('/logs/') && teamsStore.teams && teamsStore.teams.length > 0) {
    teamId = teamsStore.teams[0].id
    console.log(`ContextGuard: Auto-selected first team ${teamId} for explore route`)
  }

  // Update context store (always set, even if teamId is null)
  // The individual stores will handle the null case appropriately
  contextStore.setFromRoute(teamId, sourceId)
  
  // IMPORTANT: Also update the old teams store for API compatibility
  if (teamId && teamsStore.currentTeamId !== teamId) {
    teamsStore.setCurrentTeam(teamId)
    console.log(`ContextGuard: Updated teamsStore.currentTeamId to ${teamId}`)
  }
  
  console.log(`ContextGuard: Route changed - team: ${teamId}, source: ${sourceId}`)
}
