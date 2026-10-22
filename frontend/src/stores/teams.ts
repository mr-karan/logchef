import { defineStore } from "pinia";
import { computed } from "vue";
import { useBaseStore } from "./base";
import {
  teamsApi,
  type Team,
  type CreateTeamRequest,
  type TeamMember,
  type UpdateTeamRequest,
  type TeamWithMemberCount,
  type UserTeamMembership,
} from "@/api/teams";
import type { Source } from "@/api/sources";
import type { APIErrorResponse } from "@/api/types";
import { isSuccessResponse } from "@/api/types";
import { useAuthStore } from "./auth";
import { useContextStore } from "./context";
import { i18n } from "@/i18n";

interface TeamsState {
  userTeams: UserTeamMembership[];
  adminTeams: TeamWithMemberCount[];
  teamSourcesMap: Record<number, Source[]>;
  teamMembersMap: Record<number, TeamMember[]>;
}

export const useTeamsStore = defineStore("teams", () => {
  const state = useBaseStore<TeamsState>({
    userTeams: [],
    adminTeams: [],
    teamSourcesMap: {},
    teamMembersMap: {},
  });

  const readRequests = { userTeams: 0, adminTeams: 0, team: 0, members: 0, sources: 0 };
  const supersededError: APIErrorResponse = {
    status: "error", message: "Request superseded", error_type: "CanceledError",
  };

  const authStore = useAuthStore();
  const contextStore = useContextStore();

  const userTeams = computed(() => state.data.value.userTeams);
  const adminTeams = computed(() => state.data.value.adminTeams);
  const currentTeamId = computed(() => contextStore.teamId);

  // Active teams - depends on context (admin vs user)
  // Default to user teams, fallback to admin teams if no user teams
  const teams = computed(() => {
    if (state.data.value.userTeams.length > 0) {
      return state.data.value.userTeams;
    }
    return state.data.value.adminTeams;
  });

  // Get team by ID helper
  const getTeamById = computed(() => (id: number | null) => {
    if (!id) return null;

    // First try user teams
    const userTeam = state.data.value.userTeams.find((t) => t.id === id);
    if (userTeam) return userTeam;

    // Then try admin teams
    return state.data.value.adminTeams.find((t) => t.id === id);
  });

  // Current team using the getter
  const currentTeam = computed(() => getTeamById.value(currentTeamId.value));

  // Get the last created team (most recent by created_at date)
  const getLastCreatedTeam = computed(() => {
    if (!teams.value.length) return null;
    return [...teams.value].sort((a, b) =>
      new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
    )[0];
  });

  // Get team members by team ID
  const getTeamMembersByTeamId = computed(() => (teamId: number) => {
    return state.data.value.teamMembersMap[teamId] || [];
  });

  // Get team sources by team ID
  const getTeamSourcesByTeamId = computed(() => (teamId: number) => {
    return state.data.value.teamSourcesMap[teamId] || [];
  });

  function clearState() {
    readRequests.userTeams++;
    readRequests.adminTeams++;
    readRequests.team++;
    readRequests.members++;
    readRequests.sources++;
    state.data.value.teamMembersMap = {};
    state.data.value.teamSourcesMap = {};
  }

  // A team can sit in both userTeams and adminTeams; keep the copies in step.
  function patchTeam(teamId: number, update: (team: { member_count: number }) => Partial<Team>): boolean {
    let found = false;
    const apply = <T extends { id: number; member_count: number }>(list: T[]) => {
      const index = list.findIndex((t) => t.id === teamId);
      if (index < 0) return;
      list[index] = { ...list[index], ...update(list[index]) };
      found = true;
    };
    apply(state.data.value.userTeams);
    apply(state.data.value.adminTeams);
    return found;
  }

  function selectNextTeamIfCurrent(teamId: number, candidates: readonly { id: number }[]) {
    if (contextStore.teamId !== teamId) return;
    const next = candidates.find((t) => t.id !== teamId);
    if (next) {
      contextStore.selectTeam(next.id);
    } else {
      contextStore.clear();
    }
  }

  async function loadUserTeams() {
    const requestId = ++readRequests.userTeams;
    return await state.callApi({
      apiCall: async () => {
        try {
          const response = await teamsApi.listUserTeams();
          if (requestId !== readRequests.userTeams) throw supersededError;
          return response;
        } catch (error) {
          if (requestId !== readRequests.userTeams) throw supersededError;
          throw error;
        }
      },
      operationKey: "loadUserTeams",
      onSuccess: async (response) => {
        if (requestId !== readRequests.userTeams) return;
        // Simple, focused approach: Always work with the data array from the standard API response
        if (Array.isArray(response)) {
          state.data.value.userTeams = response;

          if (!contextStore.teamId) {
            contextStore.restoreTeam(response.map((team) => team.id));
          }
        }
      },
    });
  }

  async function loadAdminTeams(forceReload = false) {
    const requestId = ++readRequests.adminTeams;
    return await state.withLoading('loadAdminTeams', async () => {
      try {
        if (!forceReload && state.data.value.adminTeams.length > 0) {
          return { success: true, data: state.data.value.adminTeams };
        }

        const response = await teamsApi.listAllTeams();
        if (requestId !== readRequests.adminTeams) return state.handleError(supersededError, "loadAdminTeams");

        if (isSuccessResponse(response) && response.data) {
          state.data.value.adminTeams = response.data as TeamWithMemberCount[];
          return { success: true, data: response.data };
        }

        return { success: false, error: { status: 'error', message: "Failed to load admin teams", error_type: "LoadError" } as APIErrorResponse };
      } catch (error) {
        return state.handleError(requestId !== readRequests.adminTeams ? supersededError : error as Error, 'loadAdminTeams');
      }
    });
  }

  // Load appropriate teams based on context
  async function loadTeams(forceReload = false, useAdminEndpoint = false) {
    try {
      // Simple approach: just delegate to the appropriate loader function
      return useAdminEndpoint
        ? await loadAdminTeams(forceReload)
        : await loadUserTeams();
    } catch (error) {
      return {
        success: false,
        error: {
          message: "Failed to load teams",
          error_type: "LoadingError"
        }
      };
    }
  }

  async function createTeam(data: CreateTeamRequest) {
    return await state.callApi({
      apiCall: () => teamsApi.createTeam(data),
      successMessage: i18n.global.t("access.teamCreated"),
      operationKey: 'createTeam',
      onSuccess: (response) => {
        if (response) {
          const teamResponse = response as Team;
          const newTeam: TeamWithMemberCount = {
            ...teamResponse,
            member_count: 0
          };

          state.data.value.adminTeams.push(newTeam);

          if (!contextStore.teamId && state.data.value.adminTeams.length > 0) {
            contextStore.selectTeam(newTeam.id);
          }
        }
      }
    });
  }

  async function getTeam(teamId: number) {
    const requestId = ++readRequests.team;
    return await state.withLoading(`getTeam-${teamId}`, async () => {
      try {
        const response = await teamsApi.getTeam(teamId);
        if (requestId !== readRequests.team) return state.handleError(supersededError, "getTeam");

        if (isSuccessResponse(response) && response.data) {
          const teamData = { ...response.data, member_count: response.data.member_count || 0 };
          if (!patchTeam(teamId, () => teamData)) {
            state.data.value.adminTeams.push(teamData);
          }

          return { success: true, data: teamData };
        }

        return { success: false, error: { status: 'error', message: 'Team not found', error_type: 'NotFound' } as APIErrorResponse };
      } catch (error) {
        return state.handleError(requestId !== readRequests.team ? supersededError : error as Error, `getTeam-${teamId}`);
      }
    });
  }

  async function updateTeam(teamId: number, data: UpdateTeamRequest) {
    return await state.callApi({
      apiCall: () => teamsApi.updateTeam(teamId, data),
      successMessage: i18n.global.t("access.teamUpdated"),
      operationKey: `updateTeam-${teamId}`,
      onSuccess: (response) => {
        if (response) {
          const teamResponse = response as Team;
          patchTeam(teamId, (team) => ({
            ...teamResponse,
            member_count: teamResponse.member_count || team.member_count,
          }));
        }
      }
    });
  }

  async function deleteTeam(teamId: number) {
    return await state.callApi({
      apiCall: () => teamsApi.deleteTeam(teamId),
      successMessage: i18n.global.t("access.teamDeleted"),
      operationKey: `deleteTeam-${teamId}`,
      onSuccess: () => {
        state.data.value.userTeams = state.data.value.userTeams.filter(
          (t) => t.id !== teamId
        );
        state.data.value.adminTeams = state.data.value.adminTeams.filter(
          (t) => t.id !== teamId
        );
        selectNextTeamIfCurrent(teamId, teams.value);

        delete state.data.value.teamMembersMap[teamId];
        delete state.data.value.teamSourcesMap[teamId];
      }
    });
  }

  async function listTeamMembers(teamId: number) {
    const requestId = ++readRequests.members;
    return await state.withLoading(`listTeamMembers-${teamId}`, async () => {
      try {
        const response = await teamsApi.listTeamMembers(teamId);
        if (requestId !== readRequests.members) return state.handleError(supersededError, "listTeamMembers");

        if (isSuccessResponse(response)) {
          const membersData = response.data;
          if (membersData) {
            state.data.value.teamMembersMap[teamId] = membersData;
            return { success: true, data: membersData };
          }
        }

        return { success: false, error: { status: 'error', message: "Failed to load team members", error_type: "LoadError" } as APIErrorResponse };
      } catch (error) {
        return state.handleError(requestId !== readRequests.members ? supersededError : error as Error, `listTeamMembers-${teamId}`);
      }
    });
  }

  async function addTeamMember(
    teamId: number,
    data: { user_id: number; role: "admin" | "member" | "editor" }
  ) {
    // Validate parameters
    if (!teamId || !data.user_id) {
      return state.handleError(
        {
          status: "error",
          message: "Invalid team or user ID",
          error_type: "ValidationError"
        } as APIErrorResponse,
        `addTeamMember-${teamId}`
      );
    }

    return await state.callApi({
      apiCall: () => teamsApi.addTeamMember(teamId, data),
      successMessage: i18n.global.t("access.memberAdded"),
      operationKey: `addTeamMember-${teamId}`,
      onSuccess: async (_response) => {
        patchTeam(teamId, (team) => ({ member_count: (team.member_count || 0) + 1 }));

        await listTeamMembers(teamId);

        const currentUserId = authStore.user?.id;
        if (currentUserId && data.user_id === Number(currentUserId)) {
          await loadUserTeams();
        }
      }
    });
  }

  async function removeTeamMember(teamId: number, userId: number) {
    // Validate parameters
    if (!teamId || !userId) {
      return state.handleError(
        {
          status: "error",
          message: "Invalid team or user ID for removal",
          error_type: "ValidationError"
        } as APIErrorResponse,
        `removeTeamMember-${teamId}-${userId}`
      );
    }

    const currentUserId = authStore.user?.id;
    const isCurrentUser = currentUserId && userId === Number(currentUserId);

    return await state.callApi({
      apiCall: () => teamsApi.removeTeamMember(teamId, userId),
      successMessage: i18n.global.t("access.memberRemoved"),
      operationKey: `removeTeamMember-${teamId}-${userId}`,
      onSuccess: async () => {
        try {
          // Remove member from our cache directly
          if (state.data.value.teamMembersMap[teamId]) {
            state.data.value.teamMembersMap[teamId] = state.data.value.teamMembersMap[teamId].filter(
              m => m.user_id !== userId
            );
          }

          patchTeam(teamId, (team) => ({ member_count: Math.max((team.member_count || 0) - 1, 0) }));

          if (isCurrentUser) {
            state.data.value.userTeams = state.data.value.userTeams.filter(t => t.id !== teamId);
            selectNextTeamIfCurrent(teamId, userTeams.value);

            await loadUserTeams();
          }
        } catch (e) {
          console.error("Error during onSuccess of removeTeamMember:", e);
          // This catch prevents errors in the success handling logic (UI updates, etc.)
          // from causing the main removeTeamMember promise to reject, ensuring
          // the API's success is correctly reported to the UI.
        }
      }
    });
  }

  async function listTeamSources(teamId: number) {
    const requestId = ++readRequests.sources;
    return await state.withLoading(`listTeamSources-${teamId}`, async () => {
      try {
        const response = await teamsApi.listTeamSources(teamId);
        if (requestId !== readRequests.sources) return state.handleError(supersededError, "listTeamSources");

        if (isSuccessResponse(response)) {
          const sourcesData = response.data;
          if (sourcesData) {
            state.data.value.teamSourcesMap[teamId] = sourcesData;
            return { success: true, data: sourcesData };
          }
        }

        return { success: false, error: { status: 'error', message: "Failed to load team sources", error_type: "LoadError" } as APIErrorResponse };
      } catch (error) {
        return state.handleError(requestId !== readRequests.sources ? supersededError : error as Error, `listTeamSources-${teamId}`);
      }
    });
  }

  async function addTeamSource(teamId: number, sourceId: number) {
    return await state.callApi({
      apiCall: () => teamsApi.addTeamSource(teamId, sourceId),
     successMessage: i18n.global.t("access.teamSourceAdded"),
     operationKey: `addTeamSource-${teamId}-${sourceId}`,
     onSuccess: async () => {
       // Always refresh the list after successfully adding a source
       // to ensure we have the complete source object with connection details.
       await listTeamSources(teamId);
     }
   });
  }

  async function removeTeamSource(teamId: number, sourceId: number) {
    return await state.callApi({
      apiCall: () => teamsApi.removeTeamSource(teamId, sourceId),
      successMessage: i18n.global.t("access.teamSourceRemoved"),
      operationKey: `removeTeamSource-${teamId}-${sourceId}`,
      onSuccess: async () => {
        // Update the cache if we have it
        if (state.data.value.teamSourcesMap[teamId]) {
          state.data.value.teamSourcesMap[teamId] = state.data.value.teamSourcesMap[teamId].filter(
            s => s.id !== sourceId
          );
        }
      }
    });
  }

  // Check if user is admin in ANY team (for showing administration sidebar)
  const isAnyTeamAdmin = computed(() => {
    return state.data.value.userTeams.some(team => team.role === 'admin');
  });

  // Get teams the user can manage (for team admins who are not global admins)
  // Global admins should use adminTeams, team admins get filtered userTeams
  const managedTeams = computed(() => {
    // If global admin and adminTeams are loaded, use those
    if (authStore.user?.role === 'admin' && state.data.value.adminTeams.length > 0) {
      return state.data.value.adminTeams;
    }
    // Otherwise, return teams where user is an admin
    return state.data.value.userTeams.filter(team => team.role === 'admin');
  });

  return {
    // State
    userTeams,
    adminTeams,
    teams,
    isLoading: state.isLoading,
    error: state.error,
    currentTeamId,
    currentTeam,
    loadingStates: state.loadingStates,

    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
    isLoadingTeamMembers: (teamId: number) => state.isLoadingOperation(`listTeamMembers-${teamId}`),
    isLoadingTeamSources: (teamId: number) => state.isLoadingOperation(`listTeamSources-${teamId}`),

    // Getters
    getTeamById: (id: number) => getTeamById.value(id),
    getTeamMembersByTeamId: (teamId: number) => getTeamMembersByTeamId.value(teamId),
    getTeamSourcesByTeamId: (teamId: number) => getTeamSourcesByTeamId.value(teamId),
    getLastCreatedTeam: () => getLastCreatedTeam.value,
    isAnyTeamAdmin,
    managedTeams,

    // State Management
    clearState,

    // Actions
    loadTeams,
    loadUserTeams,
    loadAdminTeams,
    createTeam,
    getTeam,
    updateTeam,
    deleteTeam,
    listTeamMembers,
    addTeamMember,
    removeTeamMember,
    listTeamSources,
    addTeamSource,
    removeTeamSource,
  };
});
