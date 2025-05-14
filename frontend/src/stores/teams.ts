import { defineStore } from "pinia";
import { computed, watch, inject } from "vue";
import { useBaseStore } from "./base";
import {
  teamsApi,
  type Team,
  type CreateTeamRequest,
  type TeamMember,
  type UpdateTeamRequest,
  type TeamWithMemberCount as ApiTeamWithMemberCount,
  type UserTeamMembership,
} from "@/api/teams";
import type { Source } from "@/api/sources";
import { reactive } from "vue";
import type {
  APIErrorResponse,
  APIResponse,
  APISuccessResponse
} from "@/api/types";
import { useToast } from "@/components/ui/toast/use-toast";
import { useAuthStore } from "./auth";

export interface TeamWithMemberCount extends Team {
  memberCount: number;
}

interface TeamsState {
  userTeams: UserTeamMembership[];
  adminTeams: ApiTeamWithMemberCount[];
  currentTeamId: number | null;
  teamSourcesMap: Record<number, Source[]>;
  teamMembersMap: Record<number, TeamMember[]>;
}

export const useTeamsStore = defineStore("teams", () => {
  const state = useBaseStore<TeamsState>({
    userTeams: [],
    adminTeams: [],
    currentTeamId: null,
    teamSourcesMap: {},
    teamMembersMap: {},
  });

  const { toast } = useToast();
  const authStore = useAuthStore();

  // Computed properties
  const userTeams = computed(() => state.data.value.userTeams);
  const adminTeams = computed(() => state.data.value.adminTeams);
  const currentTeamId = computed(() => state.data.value.currentTeamId);

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

  // Get team sources helper
  const getTeamSources = computed(() => (teamId: number) => {
    return state.data.value.teamSourcesMap?.[teamId] || [];
  });

  // Return teams with default values for empty fields
  const getTeamsWithDefaults = computed(() => {
    return teams.value.map(team => ({
      ...team,
      name: team.name || `Team ${team.id}`,
      description: team.description || '',
      memberCount: team.member_count || 0
    }));
  });

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

  // Clear store state - helpful when switching contexts
  function clearState() {
    state.data.value.currentTeamId = null;
    state.data.value.teamMembersMap = {};
    state.data.value.teamSourcesMap = {};
  }

  // Reset user teams data - useful when switching contexts
  function resetUserTeams() {
    state.data.value.userTeams = [];
  }

  // Reset admin teams data
  function resetAdminTeams() {
    state.data.value.adminTeams = [];
  }

  async function loadUserTeams() {
    console.log("[teamsStore] loadUserTeams: Starting"); // Log start
    return await state.withLoading("loadUserTeams", async () => {
      return await state.callApi({
        apiCall: teamsApi.listUserTeams,
        operationKey: "loadUserTeams",
        onSuccess: async (response) => {
          console.log("[teamsStore] loadUserTeams onSuccess: Raw API response:", JSON.parse(JSON.stringify(response))); // Log raw response

          // Simple, focused approach: Always work with the data array from the standard API response
          if (Array.isArray(response)) {
            state.data.value.userTeams = response;
            console.log("[teamsStore] loadUserTeams onSuccess: state.data.value.userTeams after assignment:",
              JSON.parse(JSON.stringify(state.data.value.userTeams)));

            // Set current team if none is selected and we have teams
            if (!state.data.value.currentTeamId && response.length > 0) {
              state.data.value.currentTeamId = response[0].id;
              console.log("[teamsStore] loadUserTeams onSuccess: Set currentTeamId to default:", state.data.value.currentTeamId);
            }
            return { success: true, data: state.data.value.userTeams };
          } else {
            console.error("[teamsStore] loadUserTeams onSuccess: Response is not an array", response);
            return { success: false, error: { message: "Failed to load user teams: Invalid response format." } };
          }
        },
        onError: (error) => {
          console.error("[teamsStore] loadUserTeams onError: API call failed:", JSON.parse(JSON.stringify(error)));
          // No need to return here, callApi handles it.
        }
      });
    });
  }

  async function loadAdminTeams(forceReload = false) {
    return await state.withLoading('loadAdminTeams', async () => {
      try {
        // Skip loading if we already have teams and not forcing reload
        if (!forceReload && state.data.value.adminTeams.length > 0) {
          return { success: true, data: state.data.value.adminTeams };
        }

        const response = await teamsApi.listAllTeams();

        // Extract data from API response
        if (response.status === 'success' && response.data) {
          const teamsData = response.data;

          state.data.value.adminTeams = teamsData.map((team: ApiTeamWithMemberCount) => ({
            ...team,
            memberCount: team.member_count ?? 0,
          }));

          return { success: true, data: state.data.value.adminTeams };
        }

        return { success: false, error: { message: "Failed to load admin teams" } as APIErrorResponse };
      } catch (error) {
        return state.handleError(error as Error, 'loadAdminTeams');
      }
    });
  }

  // Load appropriate teams based on context
  async function loadTeams(forceReload = false, useAdminEndpoint = false) {
    console.log("[teamsStore] loadTeams: Starting with useAdminEndpoint =", useAdminEndpoint);

    try {
      // Simple approach: just delegate to the appropriate loader function
      return useAdminEndpoint
        ? await loadAdminTeams(forceReload)
        : await loadUserTeams();
    } catch (error) {
      console.error("[teamsStore] loadTeams: Error loading teams", error);
      return {
        success: false,
        error: {
          message: "Failed to load teams",
          error_type: "LoadingError"
        }
      };
    }
  }

  // Set the current team
  function setCurrentTeam(teamId: number | string) {
    state.data.value.currentTeamId = typeof teamId === 'string' ? parseInt(teamId) : teamId;
  }

  async function createTeam(data: CreateTeamRequest) {
    return await state.withLoading('createTeam', async () => {
      return await state.callApi({
        apiCall: () => teamsApi.createTeam(data),
        successMessage: "Team created successfully",
        operationKey: 'createTeam',
        onSuccess: (response) => {
          if (response) {
            // Create the team object with proper defaults
            const newTeam: TeamWithMemberCount = {
              ...response as Team, // Cast to ensure TS understands this has Team properties
              memberCount: 0,
              member_count: 0
            };

            // Add to admin teams only
            state.data.value.adminTeams.push(newTeam);

            // If we're in admin view and no team is selected, select the newly created one
            if (!state.data.value.currentTeamId && state.data.value.adminTeams.length > 0) {
              setCurrentTeam(newTeam.id);
            }
          }
        }
      });
    });
  }

  async function getTeam(teamId: number) {
    return await state.withLoading(`getTeam-${teamId}`, async () => {
      try {
        const response = await teamsApi.getTeam(teamId);

        // Extract team data from the response
        if (response.status === 'success' && response.data) {
          const teamData = response.data;
          const teamWithCount: TeamWithMemberCount = {
            ...teamData,
            memberCount: teamData.member_count || 0
          };

          // Update in user teams if it exists there
          const userIndex = state.data.value.userTeams.findIndex(t => t.id === teamId);
          if (userIndex >= 0) {
            state.data.value.userTeams[userIndex] = teamWithCount;
          }

          // Update in admin teams if it exists there
          const adminIndex = state.data.value.adminTeams.findIndex(t => t.id === teamId);
          if (adminIndex >= 0) {
            state.data.value.adminTeams[adminIndex] = teamWithCount;
          }

          // If not found in either list, add to admin teams
          if (userIndex < 0 && adminIndex < 0) {
            state.data.value.adminTeams.push(teamWithCount);
          }

          return { success: true, data: teamData };
        }

        return { success: false, error: { message: 'Team not found' } as APIErrorResponse };
      } catch (error) {
        return state.handleError(error as Error, `getTeam-${teamId}`);
      }
    });
  }

  async function updateTeam(teamId: number, data: UpdateTeamRequest) {
    return await state.withLoading(`updateTeam-${teamId}`, async () => {
      return await state.callApi({
        apiCall: () => teamsApi.updateTeam(teamId, data),
        successMessage: "Team updated successfully",
        operationKey: `updateTeam-${teamId}`,
        onSuccess: (response) => {
          if (response) {
            const teamResponse = response as Team;
            // Update in both user and admin teams if they exist
            const userIndex = state.data.value.userTeams.findIndex(t => t.id === teamId);
            if (userIndex >= 0) {
              state.data.value.userTeams[userIndex] = {
                ...state.data.value.userTeams[userIndex],
                ...teamResponse,
                memberCount: teamResponse.member_count || state.data.value.userTeams[userIndex].memberCount
              };
            }

            const adminIndex = state.data.value.adminTeams.findIndex(t => t.id === teamId);
            if (adminIndex >= 0) {
              state.data.value.adminTeams[adminIndex] = {
                ...state.data.value.adminTeams[adminIndex],
                ...teamResponse,
                memberCount: teamResponse.member_count || state.data.value.adminTeams[adminIndex].memberCount
              };
            }
          }
        }
      });
    });
  }

  async function deleteTeam(teamId: number) {
    return await state.withLoading(`deleteTeam-${teamId}`, async () => {
      return await state.callApi({
        apiCall: () => teamsApi.deleteTeam(teamId),
        successMessage: "Team deleted successfully",
        operationKey: `deleteTeam-${teamId}`,
        onSuccess: () => {
          // Remove from both user and admin teams
          state.data.value.userTeams = state.data.value.userTeams.filter(
            (t) => t.id !== teamId
          );
          state.data.value.adminTeams = state.data.value.adminTeams.filter(
            (t) => t.id !== teamId
          );

          // If this was the current team, reset current team
          if (state.data.value.currentTeamId === teamId) {
            state.data.value.currentTeamId =
              state.data.value.userTeams.length > 0
                ? state.data.value.userTeams[0].id
                : (state.data.value.adminTeams.length > 0
                  ? state.data.value.adminTeams[0].id
                  : null);
          }

          // Clean up related data
          delete state.data.value.teamMembersMap[teamId];
          delete state.data.value.teamSourcesMap[teamId];
        }
      });
    });
  }

  async function listTeamMembers(teamId: number) {
    return await state.withLoading(`listTeamMembers-${teamId}`, async () => {
      try {
        const response = await teamsApi.listTeamMembers(teamId);

        // Extract members data from the response
        if (response.status === 'success' && response.data) {
          // Store in our map for later use
          state.data.value.teamMembersMap[teamId] = response.data;
          return { success: true, data: response.data };
        }

        return { success: false, error: { message: "Failed to load team members" } as APIErrorResponse };
      } catch (error) {
        return state.handleError(error as Error, `listTeamMembers-${teamId}`);
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

    return await state.withLoading(`addTeamMember-${teamId}`, async () => {
      return await state.callApi({
        apiCall: () => teamsApi.addTeamMember(teamId, data),
        successMessage: "Member added successfully",
        operationKey: `addTeamMember-${teamId}`,
        onSuccess: async (response) => {
          // Update member count in both team lists
          const updateMemberCount = (team: TeamWithMemberCount) => {
            team.memberCount = (team.memberCount || 0) + 1;
            team.member_count = team.memberCount;
          };

          const userTeam = state.data.value.userTeams.find(t => t.id === teamId);
          if (userTeam) updateMemberCount(userTeam);

          const adminTeam = state.data.value.adminTeams.find(t => t.id === teamId);
          if (adminTeam) updateMemberCount(adminTeam);

          // Refresh the members list to ensure we have the latest data
          await listTeamMembers(teamId);

          // Check if the current user is the one being added to the team
          // If so, refresh the user's teams list to update the UI
          const currentUserId = authStore.user?.id;
          if (currentUserId && data.user_id === Number(currentUserId)) {
            await loadUserTeams(); // Force reload user teams
          }
        }
      });
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

    return await state.withLoading(`removeTeamMember-${teamId}-${userId}`, async () => {
      return await state.callApi({
        apiCall: () => teamsApi.removeTeamMember(teamId, userId),
        successMessage: "Member removed successfully",
        operationKey: `removeTeamMember-${teamId}-${userId}`,
        onSuccess: async () => {
          try {
            // Remove member from our cache directly
            if (state.data.value.teamMembersMap[teamId]) {
              state.data.value.teamMembersMap[teamId] = state.data.value.teamMembersMap[teamId].filter(
                m => m.user_id !== userId
              );
            }

            // Update member count in both team lists
            const updateMemberCount = (team: TeamWithMemberCount) => {
              // Ensure memberCount is a number and greater than 0 before decrementing
              if (typeof team.memberCount === 'number' && team.memberCount > 0) {
                team.memberCount--;
                team.member_count = team.memberCount; // Also update the optional member_count
              }
            };

            const userTeam = state.data.value.userTeams.find(t => t.id === teamId);
            if (userTeam) updateMemberCount(userTeam);

            const adminTeam = state.data.value.adminTeams.find(t => t.id === teamId);
            if (adminTeam) updateMemberCount(adminTeam);

            // If the current user was removed, update userTeams list immediately
            // This is especially important for the LogExplorer view
            if (isCurrentUser) {
              // First remove the team from userTeams list directly
              state.data.value.userTeams = state.data.value.userTeams.filter(t => t.id !== teamId);

              // Then handle selecting a new current team if needed
              if (state.data.value.currentTeamId === teamId) {
                state.data.value.currentTeamId = state.data.value.userTeams.length > 0
                  ? state.data.value.userTeams[0].id
                  : null;
              }

              // Also reload to ensure consistency
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
    });
  }

  async function listTeamSources(teamId: number) {
    return await state.withLoading(`listTeamSources-${teamId}`, async () => {
      try {
        const response = await teamsApi.listTeamSources(teamId);

        // Get the sources array from the response
        if (response.status === 'success' && response.data) {
          // Store in our map
          state.data.value.teamSourcesMap[teamId] = response.data;
          return { success: true, data: response.data };
        }

        return { success: false, error: { message: "Failed to load team sources" } as APIErrorResponse };
      } catch (error) {
        return state.handleError(error as Error, `listTeamSources-${teamId}`);
      }
    });
  }

  async function addTeamSource(teamId: number, sourceId: number) {
    return await state.withLoading(`addTeamSource-${teamId}-${sourceId}`, async () => {
      return await state.callApi({
        apiCall: () => teamsApi.addTeamSource(teamId, sourceId),
       successMessage: "Source added successfully",
       operationKey: `addTeamSource-${teamId}-${sourceId}`,
       onSuccess: async () => {
         // Always refresh the list after successfully adding a source
         // to ensure we have the complete source object with connection details.
         await listTeamSources(teamId);
       }
     });
    });
  }

  async function removeTeamSource(teamId: number, sourceId: number) {
    return await state.withLoading(`removeTeamSource-${teamId}-${sourceId}`, async () => {
      return await state.callApi({
        apiCall: () => teamsApi.removeTeamSource(teamId, sourceId),
        successMessage: "Source removed successfully",
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
    });
  }

  // Get team source IDs - helper method for filtering
  async function getTeamSourceIds(teamId: number) {
    // Check if we already have the sources cached
    if (state.data.value.teamSourcesMap[teamId]?.length > 0) {
      return state.data.value.teamSourcesMap[teamId].map(source => source.id);
    }

    // Otherwise fetch them
    const result = await listTeamSources(teamId);

    if (result.success && result.data) {
      return result.data.map((source: Source) => source.id);
    }

    return [];
  }

  // Invalidate team cache
  async function invalidateTeamCache(teamId: number) {
    // Remove team from both user and admin teams
    state.data.value.userTeams = state.data.value.userTeams.filter(t => t.id !== teamId);
    state.data.value.adminTeams = state.data.value.adminTeams.filter(t => t.id !== teamId);

    // Remove team sources from cache
    delete state.data.value.teamSourcesMap[teamId];

    // Reset current team if it was the invalidated one
    if (state.data.value.currentTeamId === teamId) {
      state.data.value.currentTeamId = state.data.value.userTeams.length > 0
        ? state.data.value.userTeams[0].id
        : (state.data.value.adminTeams.length > 0
          ? state.data.value.adminTeams[0].id
          : null);
    }

    // Clear team members cache
    delete state.data.value.teamMembersMap[teamId];
  }

  // Get sources not in team (for filtering in add source dialog)
  function getSourcesNotInTeam(teamSourceIds: (string | number)[]) {
    const normalizedIds = teamSourceIds.map(id => typeof id === 'string' ? parseInt(id) : id);
    return [] as Source[]; // Implement or delegate to sources store
  }

  // Add a new method to check if the current user belongs to a specific team
  function userBelongsToTeam(teamId: number): boolean {
    return state.data.value.userTeams.some(team => team.id === teamId);
  }

  // Add a new getter to get the current user's role in a specific team
  const getUserRoleInTeam = computed(() =>
    (teamId: number): UserTeamMembership["role"] | undefined => {
      const team = state.data.value.userTeams.find(t => t.id === teamId);
      return team?.role; // team.role is now directly available from UserTeamMembership
    }
  );

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
    isLoadingTeams: () => state.isLoadingOperation('loadTeams') ||
                          state.isLoadingOperation('loadUserTeams') ||
                          state.isLoadingOperation('loadAdminTeams'),
    isLoadingTeam: (teamId: number) => state.isLoadingOperation(`getTeam-${teamId}`),
    isLoadingTeamMembers: (teamId: number) => state.isLoadingOperation(`listTeamMembers-${teamId}`),
    isLoadingTeamSources: (teamId: number) => state.isLoadingOperation(`listTeamSources-${teamId}`),

    // Getters
    getTeamById: (id: number) => getTeamById.value(id),
    getTeamSources: (teamId: number) => getTeamSources.value(teamId),
    getTeamMembersByTeamId: (teamId: number) => getTeamMembersByTeamId.value(teamId),
    getTeamSourcesByTeamId: (teamId: number) => getTeamSourcesByTeamId.value(teamId),
    getTeamsWithDefaults: () => getTeamsWithDefaults.value,
    getLastCreatedTeam: () => getLastCreatedTeam.value,
    getSourcesNotInTeam,
    userBelongsToTeam,
    getUserRoleInTeam,

    // State Management
    clearState,
    resetUserTeams,
    resetAdminTeams,

    // Actions
    loadTeams,
    loadUserTeams,
    loadAdminTeams,
    createTeam,
    getTeam,
    updateTeam,
    deleteTeam,
    setCurrentTeam,
    listTeamMembers,
    addTeamMember,
    removeTeamMember,
    listTeamSources,
    addTeamSource,
    removeTeamSource,
    getTeamSourceIds,
    invalidateTeamCache,
  };
});
