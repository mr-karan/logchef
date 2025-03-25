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
import type { 
  APIErrorResponse, 
  isSuccessResponse 
} from "@/api/types";

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

  // Helper function to handle errors
  function handleError(error: Error | APIErrorResponse, operation: string) {
    console.error(`[${operation} Error]`, error);
    
    const errorMessage = error instanceof Error ? error.message : error.message;
    const errorType = error instanceof Error ? 'UnknownError' : (error.error_type || 'UnknownError');
    const errorData = error instanceof Error ? undefined : error.data;
    
    state.error.value = {
      message: errorMessage,
      error_type: errorType,
      data: errorData,
      operation
    };
    
    return { 
      success: false,
      error: state.error.value
    };
  }

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

  async function loadTeams(forceReload = false) {
    return await state.withLoading('loadTeams', async () => {
      try {
        // Skip loading if we already have teams and not forcing reload
        if (!forceReload && state.data.value.teams.length > 0) {
          return { success: true, data: state.data.value.teams };
        }
        
        const response = await teamsApi.listUserTeams();
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
        return handleError(error as Error, 'loadTeams');
      }
    });
  }

  // Set the current team
  function setCurrentTeam(teamId: number | string) {
    state.data.value.currentTeamId = typeof teamId === 'string' ? parseInt(teamId) : teamId;
  }

  async function createTeam(data: CreateTeamRequest) {
    return await state.withLoading('createTeam', async () => {
      try {
        const response = await teamsApi.createTeam(data);
        
        // Update local state
        if (response) {
          // Add the new team to the local state
          const newTeam = {
            ...response,
            memberCount: 0
          };
          state.data.value.teams.push(newTeam);
          
          // If we have teams and no current team is selected, select the newly created one
          if (
            state.data.value.teams.length > 0 &&
            !state.data.value.currentTeamId &&
            response.id
          ) {
            setCurrentTeam(response.id);
          }
        }
        
        return { 
          success: true, 
          data: response,
          message: "Team created successfully"
        };
      } catch (error) {
        return handleError(error as Error, 'createTeam');
      }
    });
  }

  async function getTeam(teamId: number) {
    return await state.withLoading(`getTeam-${teamId}`, async () => {
      try {
        const response = await teamsApi.getTeam(teamId);
        
        // Update in local state if needed
        if (response) {
          const index = state.data.value.teams.findIndex(t => t.id === teamId);
          if (index >= 0) {
            state.data.value.teams[index] = {
              ...response,
              memberCount: state.data.value.teams[index].memberCount
            };
          }
        }
        
        return { success: true, data: response };
      } catch (error) {
        return handleError(error as Error, `getTeam-${teamId}`);
      }
    });
  }

  async function updateTeam(teamId: number, data: UpdateTeamRequest) {
    return await state.withLoading(`updateTeam-${teamId}`, async () => {
      try {
        const response = await teamsApi.updateTeam(teamId, data);
        
        // Update in local state
        if (response) {
          const index = state.data.value.teams.findIndex(t => t.id === teamId);
          if (index >= 0) {
            state.data.value.teams[index] = {
              ...state.data.value.teams[index],
              ...response,
            };
          }
        }
        
        return { 
          success: true, 
          data: response,
          message: "Team updated successfully"
        };
      } catch (error) {
        return handleError(error as Error, `updateTeam-${teamId}`);
      }
    });
  }

  async function deleteTeam(teamId: number) {
    return await state.withLoading(`deleteTeam-${teamId}`, async () => {
      try {
        await teamsApi.deleteTeam(teamId);
        
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
        
        return { 
          success: true,
          message: "Team deleted successfully"
        };
      } catch (error) {
        return handleError(error as Error, `deleteTeam-${teamId}`);
      }
    });
  }

  async function listTeamMembers(teamId: number) {
    return await state.withLoading(`listTeamMembers-${teamId}`, async () => {
      try {
        const response = await teamsApi.listTeamMembers(teamId);
        return { success: true, data: response };
      } catch (error) {
        return handleError(error as Error, `listTeamMembers-${teamId}`);
      }
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
      try {
        const response = await teamsApi.addTeamMember(teamId, data);
        
        // Update member count if successful
        const team = getTeamById.value(teamId);
        if (team) {
          team.memberCount = (team.memberCount || 0) + 1;
        }
        
        return { 
          success: true, 
          data: response,
          message: "Member added successfully"
        };
      } catch (error) {
        return handleError(error as Error, `addTeamMember-${teamId}`);
      }
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
      try {
        const response = await teamsApi.removeTeamMember(teamId, userId);
        
        // Update member count if successful
        const team = getTeamById.value(teamId);
        if (team && team.memberCount > 0) {
          team.memberCount--;
        }
        
        return { 
          success: true, 
          data: response,
          message: "Member removed successfully"
        };
      } catch (error) {
        return handleError(error as Error, `removeTeamMember-${teamId}-${userId}`);
      }
    });
  }

  async function listTeamSources(teamId: number) {
    return await state.withLoading(`listTeamSources-${teamId}`, async () => {
      try {
        const response = await teamsApi.listTeamSources(teamId);
        
        // Cache the sources in the teamSourcesMap
        if (response) {
          state.data.value.teamSourcesMap = {
            ...state.data.value.teamSourcesMap,
            [teamId]: response
          };
        }
        
        return { success: true, data: response };
      } catch (error) {
        return handleError(error as Error, `listTeamSources-${teamId}`);
      }
    });
  }

  async function addTeamSource(teamId: number, sourceId: number) {
    return await state.withLoading(`addTeamSource-${teamId}-${sourceId}`, async () => {
      try {
        const response = await teamsApi.addTeamSource(teamId, sourceId);
        return { 
          success: true, 
          data: response,
          message: "Source added successfully"
        };
      } catch (error) {
        return handleError(error as Error, `addTeamSource-${teamId}-${sourceId}`);
      }
    });
  }

  async function removeTeamSource(teamId: number, sourceId: number) {
    return await state.withLoading(`removeTeamSource-${teamId}-${sourceId}`, async () => {
      try {
        const response = await teamsApi.removeTeamSource(teamId, sourceId);
        return { 
          success: true, 
          data: response,
          message: "Source removed successfully"
        };
      } catch (error) {
        return handleError(error as Error, `removeTeamSource-${teamId}-${sourceId}`);
      }
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
        if (response) {
          state.data.value.teamSourcesMap = {
            ...state.data.value.teamSourcesMap,
            [teamId]: response
          };
        }
        
        return { success: true, data: response };
      } catch (error) {
        return handleError(error as Error, `getTeamSourceIds-${teamId}`);
      }
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
