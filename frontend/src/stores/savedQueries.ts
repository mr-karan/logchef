import { defineStore } from "pinia";
import { useBaseStore, handleApiCall } from "./base";
import { savedQueriesApi } from "@/api/savedQueries";
import type { SavedTeamQuery, Team, APIResponse } from "@/api/types";
import { isErrorResponse } from "@/api/types";
import { computed } from "vue";

export interface SavedQueriesState {
  queries: SavedTeamQuery[];
  selectedQuery: SavedTeamQuery | null;
  teams: Team[];
  selectedTeamId: number | null;
}

export const useSavedQueriesStore = defineStore("savedQueries", () => {
  // Initialize base store with default state
  const state = useBaseStore<SavedQueriesState>({
    queries: [],
    selectedQuery: null,
    teams: [],
    selectedTeamId: null,
  });

  // Computed properties
  const hasTeams = computed(() => (state.data.value.teams?.length || 0) > 0);
  const hasQueries = computed(
    () => (state.data.value.queries?.length || 0) > 0
  );
  const selectedTeam = computed(() => {
    return (
      state.data.value.teams?.find(
        (t) => t.id === state.data.value.selectedTeamId
      ) || null
    );
  });

  // Actions
  async function fetchUserTeams() {
    return await state.withLoading(async () => {
      const result = await handleApiCall<Team[]>({
        apiCall: () => savedQueriesApi.getUserTeams(),
        onSuccess: (response) => {
          state.data.value.teams = response || [];
          if (
            response &&
            response.length > 0 &&
            !state.data.value.selectedTeamId
          ) {
            state.data.value.selectedTeamId = response[0].id;
          }
        },
      });
      return result;
    });
  }

  function setSelectedTeam(teamId: number) {
    state.data.value.selectedTeamId = teamId;
  }

  async function fetchTeamQueries(teamId: number) {
    return await state.withLoading(async () => {
      const result = await handleApiCall<SavedTeamQuery[]>({
        apiCall: () => savedQueriesApi.listQueries(teamId),
        onSuccess: (response) => {
          state.data.value.queries = response || [];
        },
      });
      return result;
    });
  }

  async function fetchSourceQueries(sourceId: number, teamId: number) {
    return await state.withLoading(async () => {
      const result = await handleApiCall<SavedTeamQuery[]>({
        apiCall: () => savedQueriesApi.listSourceQueries(sourceId, teamId),
        onSuccess: (response) => {
          state.data.value.queries = response || [];
        },
      });
      return result;
    });
  }

  async function fetchQuery(teamId: number, queryId: string) {
    return await state.withLoading(async () => {
      const result = await handleApiCall<SavedTeamQuery>({
        apiCall: () => savedQueriesApi.getQuery(teamId, queryId),
        onSuccess: (response) => {
          state.data.value.selectedQuery = response || null;
        },
      });
      return result;
    });
  }

  async function createQuery(
    teamId: number,
    query: Omit<SavedTeamQuery, "id" | "created_at" | "updated_at">
  ) {
    return await state.withLoading(async () => {
      const result = await handleApiCall<SavedTeamQuery>({
        apiCall: () => savedQueriesApi.createQuery(teamId, query),
        onSuccess: (response) => {
          state.data.value.queries.unshift(response);
          state.data.value.selectedQuery = response;
        },
      });
      return result;
    });
  }

  async function updateQuery(
    teamId: number,
    queryId: string,
    query: Partial<SavedTeamQuery>
  ) {
    return await state.withLoading(async () => {
      const result = await handleApiCall<SavedTeamQuery>({
        apiCall: () => savedQueriesApi.updateQuery(teamId, queryId, query),
        onSuccess: (response) => {
          const index = state.data.value.queries.findIndex(
            (q) => q.id === queryId
          );
          if (index >= 0) {
            state.data.value.queries[index] = response;
          }
          if (state.data.value.selectedQuery?.id === queryId) {
            state.data.value.selectedQuery = response;
          }
        },
      });
      return result;
    });
  }

  async function deleteQuery(teamId: number, queryId: string) {
    return await state.withLoading(async () => {
      const result = await handleApiCall<{ success: boolean }>({
        apiCall: () => savedQueriesApi.deleteQuery(teamId, queryId),
        onSuccess: () => {
          state.data.value.queries = state.data.value.queries.filter(
            (q) => q.id !== queryId
          );
          if (state.data.value.selectedQuery?.id === queryId) {
            state.data.value.selectedQuery = null;
          }
        },
      });
      return result;
    });
  }

  // Reset state function (similar to explore.ts)
  function resetState() {
    state.data.value = {
      queries: [],
      selectedQuery: null,
      teams: state.data.value.teams, // Preserve teams
      selectedTeamId: state.data.value.selectedTeamId, // Preserve selected team
    };
  }

  return {
    ...state,
    // Computed properties
    hasTeams,
    hasQueries,
    selectedTeam,
    // Actions
    fetchUserTeams,
    setSelectedTeam,
    fetchTeamQueries,
    fetchSourceQueries,
    fetchQuery,
    createQuery,
    updateQuery,
    deleteQuery,
    resetState,
  };
});
