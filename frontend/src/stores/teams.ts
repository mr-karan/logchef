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
import type { APIErrorResponse } from "@/api/types";
import { useApiQuery } from "@/composables/useApiQuery";

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

  // Use our API query composable
  const { execute } = useApiQuery();

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

  async function loadTeams() {
    return await state.withLoading('loadTeams', async () => {
      return await execute(() => teamsApi.listUserTeams(), {
        onSuccess: (response) => {
          if (response) {
            state.data.value.teams = response.map((team) => ({
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
        },
        defaultData: [],
        showToast: true,
      });
    });
  }

  // Set the current team
  function setCurrentTeam(teamId: number | string) {
    state.data.value.currentTeamId = typeof teamId === 'string' ? parseInt(teamId) : teamId;
  }

  async function createTeam(data: CreateTeamRequest) {
    const result = await state.withLoading('createTeam', async () => {
      return await execute(() => teamsApi.createTeam(data), {
        successMessage: "Team created successfully"
      });
    });

    if (result.success && result.data) {
      // Reload teams to get fresh data with member counts
      await loadTeams();

      // If we have teams and no current team is selected, select the newly created one
      if (
        state.data.value.teams.length > 0 &&
        !state.data.value.currentTeamId
      ) {
        const createdTeam = state.data.value.teams.find(
          (t) => t.id === result.data?.id
        );
        if (createdTeam) {
          setCurrentTeam(createdTeam.id);
        }
      }
    }

    return result;
  }

  async function getTeam(teamId: number) {
    return await state.withLoading(`getTeam-${teamId}`, async () => {
      return await execute(() => teamsApi.getTeam(teamId), {
        showToast: true
      });
    });
  }

  async function updateTeam(teamId: number, data: UpdateTeamRequest) {
    return await state.withLoading(`updateTeam-${teamId}`, async () => {
      const result = await execute(() => teamsApi.updateTeam(teamId, data), {
        successMessage: "Team updated successfully"
      });
      
      if (result.success) {
        // Reload teams to get fresh data
        await loadTeams();
      }
      
      return result;
    });
  }

  async function deleteTeam(teamId: number) {
    return await state.withLoading(`deleteTeam-${teamId}`, async () => {
      return await execute(() => teamsApi.deleteTeam(teamId), {
        successMessage: "Team deleted successfully",
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

  async function listTeamMembers(teamId: number) {
    return await state.withLoading(`listTeamMembers-${teamId}`, async () => {
      return await execute(() => teamsApi.listTeamMembers(teamId), {
        showToast: true
      });
    });
  }

  async function addTeamMember(
    teamId: number,
    data: { user_id: number; role: "admin" | "member" }
  ) {
    // Validate parameters
    if (!teamId || !data.user_id) {
      return { 
        success: false, 
        error: { 
          message: "Invalid team or user ID", 
          error_type: "ValidationError" 
        } as APIErrorResponse 
      };
    }
    
    return await state.withLoading(`addTeamMember-${teamId}`, async () => {
      const result = await execute(() => teamsApi.addTeamMember(teamId, data), {
        successMessage: "Member added successfully"
      });
      
      // Update member count if successful
      if (result.success) {
        const team = getTeamById.value(teamId);
        if (team) {
          team.memberCount = (team.memberCount || 0) + 1;
        }
      }
      
      return result;
    });
  }

  async function removeTeamMember(teamId: number, userId: number) {
    // Validate parameters
    if (!teamId || !userId) {
      return { 
        success: false, 
        error: { 
          message: "Invalid team or user ID", 
          error_type: "ValidationError" 
        } as APIErrorResponse 
      };
    }
    
    return await state.withLoading(`removeTeamMember-${teamId}-${userId}`, async () => {
      const result = await execute(() => teamsApi.removeTeamMember(teamId, userId), {
        successMessage: "Member removed successfully"
      });
      
      // Update member count if successful
      if (result.success) {
        const team = getTeamById.value(teamId);
        if (team && team.memberCount > 0) {
          team.memberCount--;
        }
      }
      
      return result;
    });
  }

  async function listTeamSources(teamId: number) {
    return await state.withLoading(`listTeamSources-${teamId}`, async () => {
      const result = await execute(() => teamsApi.listTeamSources(teamId), {
        showToast: true
      });
      
      // Cache the sources in the teamSourcesMap if successful
      if (result.success && result.data) {
        state.data.value.teamSourcesMap = {
          ...state.data.value.teamSourcesMap,
          [teamId]: result.data
        };
      }
      
      return result;
    });
  }

  async function addTeamSource(teamId: number, sourceId: number) {
    return await state.withLoading(`addTeamSource-${teamId}-${sourceId}`, async () => {
      return await execute(() => teamsApi.addTeamSource(teamId, sourceId), {
        successMessage: "Source added successfully"
      });
    });
  }

  async function removeTeamSource(teamId: number, sourceId: number) {
    return await state.withLoading(`removeTeamSource-${teamId}-${sourceId}`, async () => {
      return await execute(() => teamsApi.removeTeamSource(teamId, sourceId), {
        successMessage: "Source removed successfully"
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
      return await execute(() => teamsApi.listTeamSources(teamId), {
        showToast: false,
        onSuccess: (data) => {
          if (data) {
            // Cache the sources
            state.data.value.teamSourcesMap = {
              ...state.data.value.teamSourcesMap,
              [teamId]: data
            };
          }
        }
      });
    });

    if (result.success && result.data) {
      return result.data.map((source) => source.id);
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
