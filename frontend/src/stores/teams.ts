import { defineStore } from "pinia";
import { computed } from "vue";
import { useBaseStore } from "./base";
import {
  teamsApi,
  type Team,
  type CreateTeamRequest,
  type TeamMember,
  type UpdateTeamRequest,
} from "@/api/teams";
import type { Source } from "@/api/sources";
import { reactive } from "vue";
import type {
  APIErrorResponse,
  isSuccessResponse,
  APIResponse
} from "@/api/types";
import { useToast } from "@/components/ui/toast/use-toast";

export interface TeamWithMemberCount extends Team {
  memberCount: number;
}

interface TeamsState {
  teams: TeamWithMemberCount[];
  currentTeamId: number | null;
  teamSourcesMap: Record<number, Source[]>;
}

export const useTeamsStore = defineStore("teams", () => {
  const state = useBaseStore<TeamsState>({
    teams: [],
    currentTeamId: null,
    teamSourcesMap: {},
  });

  // Use the centralized error handler from base store

  // Computed properties
  const teams = computed(() => state.data.value.teams);
  const currentTeamId = computed(() => state.data.value.currentTeamId);

  // Get team by ID helper
  const getTeamById = computed(() => (id: number | null) => {
    if (!id) return null;
    return teams.value.find((t) => t.id === id);
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

  async function loadTeams(forceReload = false) {
    return await state.withLoading('loadTeams', async () => {
      try {
        // Skip loading if we already have teams and not forcing reload
        if (!forceReload && state.data.value.teams.length > 0) {
          return { success: true, data: state.data.value.teams };
        }

        const response = await teamsApi.listAllTeams();
        // Directly access the data array from successful response
        const teamsData = response.status === 'success' ? response.data || [] : [];

        if (teamsData.length > 0) {
          state.data.value.teams = teamsData.map((team) => ({
            ...team,
            memberCount: team.member_count ?? 0,
          }));

          // Set current team if none is selected and we have teams
          if (
            !state.data.value.currentTeamId &&
            state.data.value.teams.length > 0
          ) {
            state.data.value.currentTeamId = state.data.value.teams[0].id;
          }
        } else {
          state.data.value.teams = [];
        }

        return {
          success: true,
          data: state.data.value.teams
        };
      } catch (error) {
        return state.handleError(error as Error, 'loadTeams');
      }
    });
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
        onSuccess: (response: Team | null) => {
          if (response) {
            // Add the new team to the local state
            const newTeam: TeamWithMemberCount = {
              ...response,
              memberCount: 0,
              member_count: 0,
              created_by: response.created_by || '',
              created_at: response.created_at || '',
              updated_at: response.updated_at || ''
            };
            state.data.value.teams.push(newTeam);

            // If we have teams and no current team is selected, select the newly created one
            if (
              state.data.value.teams.length > 0 &&
              !state.data.value.currentTeamId &&
              newTeam.id
            ) {
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
        const response = await teamsApi.getTeam(teamId) as APIResponse<Team>;

        // Extract team data from the response and ensure it's typed correctly
        if (response.status === 'success' && response.data) {
          const teamData = response.data;
          const index = state.data.value.teams.findIndex(t => t.id === teamId);
          const teamWithCount: TeamWithMemberCount = {
            ...teamData,
            memberCount: teamData.member_count || 0
          };

          if (index >= 0) {
            state.data.value.teams[index] = teamWithCount;
          } else {
            // Team not in local state yet, add it
            state.data.value.teams.push(teamWithCount);
          }

          return { success: true, data: teamData };
        }

        return { success: false, error: 'Team not found' };
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
            const index = state.data.value.teams.findIndex(t => t.id === teamId);
            if (index >= 0) {
              state.data.value.teams[index] = {
                ...state.data.value.teams[index],
                ...response,
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
          // Remove from local state
          state.data.value.teams = state.data.value.teams.filter(
            (t) => t.id !== teamId
          );

          // If this was the current team, reset current team
          if (state.data.value.currentTeamId === teamId) {
            state.data.value.currentTeamId =
              state.data.value.teams.length > 0
                ? state.data.value.teams[0].id
                : null;
          }
        }
      });
    });
  }

  // Store team members in state
  const teamMembersMap = reactive(new Map<number, TeamMember[]>());

  // Get team members by team ID
  const getTeamMembersByTeamId = computed(() => (teamId: number) => {
    return teamMembersMap.get(teamId) || [];
  });

  async function listTeamMembers(teamId: number) {
    return await state.withLoading(`listTeamMembers-${teamId}`, async () => {
      try {
        const response = await teamsApi.listTeamMembers(teamId) as APIResponse<TeamMember[]>;

        // Ensure we return the array of team members from the data property
        const members = response.status === 'success' && response.data ? response.data : [];

        // Store in our map for later use
        teamMembersMap.set(teamId, members);

        return { success: true, data: members };
      } catch (error) {
        return state.handleError(error as Error, `listTeamMembers-${teamId}`);
      }
    });
  }

  async function addTeamMember(
    teamId: number,
    data: { user_id: number; role: "admin" | "member" }
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
          // Update member count if successful
          const team = getTeamById.value(teamId);
          if (team) {
            team.memberCount = (team.memberCount || 0) + 1;
          }
          // Refresh team members list
          await listTeamMembers(teamId);
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
          message: "Invalid team or user ID",
          error_type: "ValidationError"
        } as APIErrorResponse,
        `removeTeamMember-${teamId}-${userId}`
      );
    }

    return await state.withLoading(`removeTeamMember-${teamId}-${userId}`, async () => {
      return await state.callApi({
        apiCall: () => teamsApi.removeTeamMember(teamId, userId),
        successMessage: "Member removed successfully",
        operationKey: `removeTeamMember-${teamId}-${userId}`,
        onSuccess: async (response) => {
          // Update member count if successful
          const team = getTeamById.value(teamId);
          if (team && team.memberCount > 0) {
            team.memberCount--;
          }
          // Refresh team members list
          await listTeamMembers(teamId);
        }
      });
    });
  }

  // Store team sources in state
  const teamSourcesMap = reactive(new Map<number, Source[]>());

  // Get team sources by team ID
  const getTeamSourcesByTeamId = computed(() => (teamId: number) => {
    return teamSourcesMap.get(teamId) || [];
  });

  async function listTeamSources(teamId: number) {
    return await state.withLoading(`listTeamSources-${teamId}`, async () => {
      try {
        const response = await teamsApi.listTeamSources(teamId) as APIResponse<Source[]>;

        // Get the sources array from the response
        const sources = response.status === 'success' && response.data ? response.data : [];

        // Cache the sources in our maps
        teamSourcesMap.set(teamId, sources);

        // Also keep the old cache for backward compatibility
        state.data.value.teamSourcesMap = {
          ...state.data.value.teamSourcesMap,
          [teamId]: sources
        };

        return { success: true, data: sources };
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
          // Refresh team sources list
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
          // Refresh team sources list
          await listTeamSources(teamId);
        }
      });
    });
  }

  // Get team source IDs - helper method for filtering
  async function getTeamSourceIds(teamId: number) {
    // Check if we already have the sources cached
    if (state.data.value.teamSourcesMap?.[teamId]?.length > 0) {
      return state.data.value.teamSourcesMap[teamId].map(source => source.id);
    }

    // Otherwise fetch them
    const result = await state.withLoading(`getTeamSourceIds-${teamId}`, async () => {
      try {
        const response = await teamsApi.listTeamSources(teamId);

        // Cache the sources
        if (response.status === 'success' && response.data) {
          state.data.value.teamSourcesMap = {
            ...state.data.value.teamSourcesMap,
            [teamId]: response.data
          };
        }

        return { success: true, data: response };
      } catch (error) {
        return state.handleError(error as Error, `getTeamSourceIds-${teamId}`);
      }
    });

    if (result.success && result.data?.status === 'success' && result.data.data) {
      return result.data.data.map((source: Source) => source.id);
    }

    return [];
  }

  // Invalidate team cache
  async function invalidateTeamCache(teamId: number) {
    // Remove team from teams array
    state.data.value.teams = state.data.value.teams.filter(t => t.id !== teamId);

    // Remove team sources from cache
    if (state.data.value.teamSourcesMap?.[teamId]) {
      const { [teamId]: _, ...rest } = state.data.value.teamSourcesMap;
      state.data.value.teamSourcesMap = rest;
    }

    // Reset current team if it was the invalidated one
    if (state.data.value.currentTeamId === teamId) {
      state.data.value.currentTeamId = state.data.value.teams.length > 0
        ? state.data.value.teams[0].id
        : null;
    }
  }

  return {
    teams,
    isLoading: state.isLoading,
    error: state.error,
    currentTeamId,
    currentTeam,
    loadingStates: state.loadingStates,

    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
    isLoadingTeams: () => state.isLoadingOperation('loadTeams'),
    isLoadingTeam: (teamId: number) => state.isLoadingOperation(`getTeam-${teamId}`),
    isLoadingTeamMembers: (teamId: number) => state.isLoadingOperation(`listTeamMembers-${teamId}`),

    // Getters
    getTeamById: (id: number) => getTeamById.value(id),
    getTeamSources: (teamId: number) => getTeamSources.value(teamId),
    getTeamMembersByTeamId: (teamId: number) => getTeamMembersByTeamId.value(teamId),
    getTeamSourcesByTeamId: (teamId: number) => getTeamSourcesByTeamId.value(teamId),
    getTeamsWithDefaults: () => getTeamsWithDefaults.value,
    getLastCreatedTeam: () => getLastCreatedTeam.value,

    // Actions
    loadTeams,
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