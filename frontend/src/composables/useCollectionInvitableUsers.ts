import { computed, ref } from "vue";
import { teamsApi } from "@/api/teams";
import type { Collection, CollectionMember } from "@/api/collections";
import type { SearchableItem } from "@/components/ui/searchable-select";
import { useTeamsStore } from "@/stores/teams";
import { useUsersStore } from "@/stores/users";
import { useTeamPermissions } from "@/composables/useTeamPermissions";

export interface InvitableUser {
  id: number;
  email: string;
  full_name?: string;
}

/**
 * Resolves the users a collection owner can invite directly.
 *
 * Global admins and team admins can browse the full user directory
 * (`/api/v1/users`). Regular collection owners who belong to at least one team
 * can still invite their teammates via `/api/v1/teams/:id/members`, which is
 * readable by any team member, without needing admin privileges.
 *
 * Adding a collection member is strictly direct-owner only on the backend
 * (`caller_role === "owner"`), so global admins who do not own the collection
 * are excluded.
 */
export function useCollectionInvitableUsers() {
  const teamsStore = useTeamsStore();
  const usersStore = useUsersStore();
  const { isGlobalAdmin, isAnyTeamAdmin } = useTeamPermissions();

  const canListAllUsers = computed(() => isGlobalAdmin.value || isAnyTeamAdmin.value);
  const canListTeammates = computed(() => (teamsStore.userTeams?.length ?? 0) > 0);
  const teammateCandidates = ref<InvitableUser[]>([]);
  const isLoading = ref(false);

  function canInviteToCollection(collection: Collection | null | undefined): boolean {
    if (!collection || collection.is_personal) return false;
    if (collection.caller_role !== "owner") return false;
    return canListAllUsers.value || canListTeammates.value;
  }

  const allCandidates = computed<InvitableUser[]>(() => {
    if (canListAllUsers.value) {
      return (usersStore.users ?? [])
        .filter((u) => u.account_type !== "service")
        .map((u) => ({
          id: Number(u.id),
          email: u.email,
          full_name: u.full_name || undefined,
        }))
        .filter((u) => Number.isFinite(u.id) && u.id > 0);
    }
    return teammateCandidates.value;
  });

  async function loadCandidates(): Promise<void> {
    if (canListAllUsers.value) {
      if ((usersStore.users?.length ?? 0) > 0) return;
      isLoading.value = true;
      try {
        await usersStore.loadUsers();
      } finally {
        isLoading.value = false;
      }
      return;
    }

    const teams = teamsStore.userTeams ?? [];
    if (teams.length === 0) {
      teammateCandidates.value = [];
      return;
    }

    isLoading.value = true;
    try {
      const results = await Promise.allSettled(
        teams.map((team) => teamsApi.listTeamMembers(team.id)),
      );
      const byId = new Map<number, InvitableUser>();
      for (const result of results) {
        if (result.status !== "fulfilled" || result.value.status !== "success") continue;
        for (const member of result.value.data ?? []) {
          if (member.account_type === "service") continue;
          if (!byId.has(member.user_id)) {
            byId.set(member.user_id, {
              id: member.user_id,
              email: member.email,
              full_name: member.full_name || undefined,
            });
          }
        }
      }
      teammateCandidates.value = [...byId.values()].sort((a, b) =>
        (a.email || a.full_name || "").localeCompare(b.email || b.full_name || ""),
      );
    } finally {
      isLoading.value = false;
    }
  }

  function availableUserItems(existingMembers: readonly CollectionMember[]): SearchableItem[] {
    const memberIds = new Set(existingMembers.map((m) => String(m.user_id)));
    return allCandidates.value
      .filter((u) => !memberIds.has(String(u.id)))
      .map((u) => ({
        value: String(u.id),
        label: u.email,
        sublabel: u.full_name || undefined,
      }));
  }

  return {
    canListAllUsers,
    canListTeammates,
    canInviteToCollection,
    allCandidates,
    isLoading,
    loadCandidates,
    availableUserItems,
  };
}
