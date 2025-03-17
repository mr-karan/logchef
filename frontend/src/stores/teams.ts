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

export interface TeamWithMemberCount extends Team {
  memberCount: number;
}

interface TeamsState {
  teams: TeamWithMemberCount[];
  currentTeamId: number | null;
}

export const useTeamsStore = defineStore("teams", () => {
  const state = useBaseStore<TeamsState>({
    teams: [],
    currentTeamId: null,
  });

  // Computed properties
  const teams = computed(() => state.data.value.teams);
  const currentTeamId = computed(() => state.data.value.currentTeamId);
  const currentTeam = computed(() =>
    currentTeamId.value
      ? teams.value.find((t) => t.id === currentTeamId.value)
      : null
  );

  async function loadTeams() {
    return await state.callApi<TeamWithMemberCount[]>({
      apiCall: () => teamsApi.listUserTeams(),
      operationKey: 'loadTeams',
      onSuccess: (response) => {
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
      },
      showToast: true,
    });
  }

  // Set the current team
  function setCurrentTeam(teamId: number) {
    state.data.value.currentTeamId = teamId;
  }

  async function createTeam(data: CreateTeamRequest) {
    const result = await state.callApi<Team>({
      apiCall: () => teamsApi.createTeam(data),
      successMessage: "Team created successfully",
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
    return await state.callApi<Team>({
      apiCall: () => teamsApi.getTeam(teamId),
      showToast: true,
    });
  }

  async function updateTeam(teamId: number, data: UpdateTeamRequest) {
    return await state.callApi<Team>({
      apiCall: () => teamsApi.updateTeam(teamId, data),
      successMessage: "Team updated successfully",
      onSuccess: async () => {
        // Reload teams to get fresh data
        await loadTeams();
      },
    });
  }

  async function deleteTeam(teamId: number) {
    return await state.callApi<{ message: string }>({
      apiCall: () => teamsApi.deleteTeam(teamId),
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
      },
    });
  }

  async function listTeamMembers(teamId: number) {
    return await state.callApi<TeamMember[]>({
      apiCall: () => teamsApi.listTeamMembers(teamId),
      showToast: true,
    });
  }

  async function addTeamMember(
    teamId: number,
    data: { user_id: number; role: "admin" | "member" }
  ) {
    return await state.callApi<TeamMember>({
      apiCall: () => teamsApi.addTeamMember(teamId, data),
      successMessage: "Member added successfully",
    });
  }

  async function removeTeamMember(teamId: number, userId: number) {
    return await state.callApi<{ message: string }>({
      apiCall: () => teamsApi.removeTeamMember(teamId, userId),
      successMessage: "Member removed successfully",
    });
  }

  async function listTeamSources(teamId: number) {
    return await state.callApi<Source[]>({
      apiCall: () => teamsApi.listTeamSources(teamId),
      showToast: true,
    });
  }

  async function addTeamSource(teamId: number, sourceId: number) {
    return await state.callApi<Source>({
      apiCall: () => teamsApi.addTeamSource(teamId, sourceId),
      successMessage: "Source added successfully",
    });
  }

  async function removeTeamSource(teamId: number, sourceId: number) {
    return await state.callApi<{ message: string }>({
      apiCall: () => teamsApi.removeTeamSource(teamId, sourceId),
      successMessage: "Source removed successfully",
    });
  }

  // Get team source IDs - helper method for filtering
  async function getTeamSourceIds(teamId: number) {
    const result = await state.callApi<Source[]>({
      apiCall: () => teamsApi.listTeamSources(teamId),
      showToast: false,
    });

    if (result.success && result.data) {
      return result.data.map((source) => source.id);
    }

    return [];
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
  };
});
