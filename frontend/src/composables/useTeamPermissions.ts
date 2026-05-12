/**
 * Centralized role-check API for the UI. All gates that key on
 * global / team / collection role should go through this composable so
 * the mapping stays consistent with the backend. See
 * `.plans/spec_roles_consistency.md` for the authoritative role matrix.
 */
import { computed } from "vue";
import { useAuthStore } from "@/stores/auth";
import { useTeamsStore } from "@/stores/teams";
import type { SavedQuery } from "@/api/savedQueries";
import type { Collection } from "@/api/collections";

type TeamRole = "admin" | "editor" | "member";

const TEAM_COLLECTION_MUTATOR_ROLES: ReadonlySet<TeamRole> = new Set(["admin", "editor"] as const);

export function useTeamPermissions() {
  const authStore = useAuthStore();
  const teamsStore = useTeamsStore();

  const isAuthenticated = computed(() => authStore.isAuthenticated);
  const isGlobalAdmin = computed(() => authStore.user?.role === "admin");

  function roleInTeam(teamId: number | null | undefined): TeamRole | null {
    if (teamId == null) return null;
    const team = teamsStore.userTeams.find((t) => t.id === teamId);
    return (team?.role as TeamRole | undefined) ?? null;
  }

  function isTeamAdmin(teamId: number | null | undefined): boolean {
    if (isGlobalAdmin.value) return true;
    return roleInTeam(teamId) === "admin";
  }

  function isTeamCollectionMutator(teamId: number | null | undefined): boolean {
    if (isGlobalAdmin.value) return true;
    const role = roleInTeam(teamId);
    return role != null && TEAM_COLLECTION_MUTATOR_ROLES.has(role);
  }

  const isAnyTeamAdmin = computed(() => {
    if (isGlobalAdmin.value) return true;
    return teamsStore.userTeams.some((t) => t.role === "admin");
  });

  const isAnyTeamCollectionMutator = computed(() => {
    if (isGlobalAdmin.value) return true;
    return teamsStore.userTeams.some((t) => TEAM_COLLECTION_MUTATOR_ROLES.has(t.role as TeamRole));
  });

  // Saved queries: anyone with source access (via any team) can save. Backend
  // enforces source access; the UI only gates on "is the user a team member
  // anywhere" because that's a precondition for having source access.
  const canSaveQuery = computed(() => {
    if (!isAuthenticated.value || !authStore.user) return false;
    if (isGlobalAdmin.value) return true;
    return teamsStore.userTeams.length > 0;
  });

  // Saved-query edit/delete is creator-owned (or global admin).
  function canEditSavedQuery(query: SavedQuery | null | undefined): boolean {
    if (!query || !authStore.user) return false;
    if (isGlobalAdmin.value) return true;
    return query.created_by != null && String(query.created_by) === String(authStore.user.id);
  }

  // Collection mutations require collection ownership at the per-resource
  // level. The cross-team mutator gate is enforced by middleware on the
  // backend, but the UI doesn't need that here — a non-owner viewing a
  // collection just shouldn't see mutation affordances.
  function canManageCollection(collection: Collection | null | undefined): boolean {
    if (!collection) return false;
    if (isGlobalAdmin.value) return true;
    return collection.caller_role === "owner";
  }

  return {
    isAuthenticated,
    isGlobalAdmin,
    isAnyTeamAdmin,
    isAnyTeamCollectionMutator,
    isTeamAdmin,
    isTeamCollectionMutator,
    canSaveQuery,
    canEditSavedQuery,
    canManageCollection,
  };
}
