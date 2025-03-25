import { defineStore } from "pinia";
import { computed } from "vue";
import {
  savedQueriesApi,
  type SavedTeamQuery,
  type Team,
  type SavedQueryContent,
} from "@/api/savedQueries";
import { useBaseStore } from "./base";
import type { APIErrorResponse } from "@/api/types";

export interface SavedQueriesState {
  queries: SavedTeamQuery[];
  selectedQuery: SavedTeamQuery | null;
  teams: Team[];
  selectedTeamId: number | null;
}

export const useSavedQueriesStore = defineStore("savedQueries", () => {
  // Create base store for common functionality and error handling
  const state = useBaseStore<SavedQueriesState>({
    queries: [],
    selectedQuery: null,
    teams: [],
    selectedTeamId: null,
  });

  // Getters
  const parseQueryContent = (query: SavedTeamQuery): SavedQueryContent => {
    try {
      const content = JSON.parse(query.query_content);

      // Ensure timeRange is always present with valid timestamps
      if (!content.timeRange || !content.timeRange.absolute) {
        content.timeRange = {
          absolute: {
            start: Date.now() - 3600000, // 1 hour ago
            end: Date.now(),
          },
        };
      }

      // Ensure limit is always present
      if (content.limit === null || content.limit === undefined) {
        content.limit = 100;
      }

      return content;
    } catch (e) {
      console.error("Error parsing query content:", e);
      return {
        version: 1,
        activeTab: "filters",
        sourceId: query.source_id,
        timeRange: {
          absolute: {
            start: Date.now() - 3600000, // 1 hour ago
            end: Date.now(),
          },
        },
        limit: 100,
        queryType: "sql",
        rawSql: "",
      };
    }
  };

  // Computed properties
  const queries = computed(() => state.data.value.queries);
  const selectedQuery = computed(() => state.data.value.selectedQuery);
  const teams = computed(() => state.data.value.teams);
  const selectedTeamId = computed(() => state.data.value.selectedTeamId);
  
  const hasTeams = computed(() => (state.data.value.teams?.length || 0) > 0);
  const hasQueries = computed(() => (state.data.value.queries?.length || 0) > 0);
  const selectedTeam = computed(() => {
    return (
      state.data.value.teams?.find((t) => t.id === state.data.value.selectedTeamId) || null
    );
  });

  // State was already initialized above

  async function fetchUserTeams() {
    return await state.withLoading('fetchUserTeams', async () => {
      return await state.callApi({
        apiCall: () => savedQueriesApi.getUserTeams(),
        operationKey: 'fetchUserTeams',
        onSuccess: (response) => {
          state.data.value.teams = response;
          if (response && response.length > 0 && !state.data.value.selectedTeamId) {
            state.data.value.selectedTeamId = response[0].id;
          }
        }
      });
    });
  }

  function setSelectedTeam(teamId: number) {
    state.data.value.selectedTeamId = teamId;
  }

  async function fetchTeamQueries(teamId: number) {
    return await state.withLoading(`fetchTeamQueries-${teamId}`, async () => {
      return await state.callApi({
        apiCall: () => savedQueriesApi.listQueries(teamId),
        operationKey: `fetchTeamQueries-${teamId}`,
        onSuccess: (responseData) => {
          state.data.value.queries = responseData;
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function fetchSourceQueries(sourceId: number, teamId: number) {
    return await state.withLoading(`fetchSourceQueries-${sourceId}-${teamId}`, async () => {
      return await state.callApi({
        apiCall: () => savedQueriesApi.listSourceQueries(sourceId, teamId),
        operationKey: `fetchSourceQueries-${sourceId}-${teamId}`,
        onSuccess: (responseData) => {
          state.data.value.queries = responseData;
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function fetchTeamSourceQueries(teamId: number, sourceId: number) {
    return await state.withLoading(`fetchTeamSourceQueries-${teamId}-${sourceId}`, async () => {
      return await state.callApi({
        apiCall: () => savedQueriesApi.listTeamSourceQueries(teamId, sourceId),
        operationKey: `fetchTeamSourceQueries-${teamId}-${sourceId}`,
        onSuccess: (responseData) => {
          // ResponseData is already null-safe due to defaultData
          state.data.value.queries = responseData;
        },
        defaultData: [], // Ensure empty array fallback
        showToast: false,
      });
    });
  }

  async function fetchQuery(teamId: number, queryId: string) {
    return await state.withLoading(`fetchQuery-${teamId}-${queryId}`, async () => {
      return await state.callApi({
        apiCall: () => savedQueriesApi.getQuery(teamId, queryId),
        operationKey: `fetchQuery-${teamId}-${queryId}`,
        onSuccess: (response) => {
          state.data.value.selectedQuery = response;
        }
      });
    });
  }

  async function createQuery(
    teamId: number,
    query: Omit<SavedTeamQuery, "id" | "created_at" | "updated_at">
  ) {
    return await state.withLoading(`createQuery-${teamId}`, async () => {
      return await state.callApi({
        apiCall: () => savedQueriesApi.createQuery(teamId, query),
        operationKey: `createQuery-${teamId}`,
        successMessage: "Query created successfully",
        onSuccess: (response) => {
          // Ensure queries array exists before modifying it
          if (!state.data.value.queries) {
            state.data.value.queries = [];
          }
          state.data.value.queries.unshift(response);
          state.data.value.selectedQuery = response;
        }
      });
    });
  }

  async function createSourceQuery(
    teamId: number,
    sourceId: number,
    name: string,
    description: string,
    queryContent: SavedQueryContent
  ) {
    return await state.withLoading(`createSourceQuery-${teamId}-${sourceId}`, async () => {
      const query = {
        name,
        description,
        query_type: queryContent.queryType || "sql",
        query_content: JSON.stringify(queryContent),
      };
      
      return await state.callApi({
        apiCall: () => savedQueriesApi.createSourceQuery(teamId, sourceId, query),
        operationKey: `createSourceQuery-${teamId}-${sourceId}`,
        successMessage: "Query created successfully",
        onSuccess: (response) => {
          // Ensure queries array exists before modifying it
          if (!state.data.value.queries) {
            state.data.value.queries = [];
          }
          state.data.value.queries.unshift(response);
          state.data.value.selectedQuery = response;
        }
      });
    });
  }

  async function updateQuery(
    teamId: number,
    queryId: string,
    query: Partial<SavedTeamQuery>
  ) {
    return await state.withLoading(`updateQuery-${teamId}-${queryId}`, async () => {
      return await state.callApi({
        apiCall: () => savedQueriesApi.updateQuery(teamId, queryId, query),
        operationKey: `updateQuery-${teamId}-${queryId}`,
        successMessage: "Query updated successfully",
        onSuccess: (response) => {
          const index = state.data.value.queries.findIndex(
            (q) => String(q.id) === queryId
          );
          if (index >= 0) {
            state.data.value.queries[index] = response;
          }
          if (state.data.value.selectedQuery?.id === Number(queryId)) {
            state.data.value.selectedQuery = response;
          }
        }
      });
    });
  }

  async function deleteQuery(teamId: number, queryId: string) {
    return await state.withLoading(`deleteQuery-${teamId}-${queryId}`, async () => {
      return await state.callApi({
        apiCall: () => savedQueriesApi.deleteQuery(teamId, queryId),
        operationKey: `deleteQuery-${teamId}-${queryId}`,
        successMessage: "Query deleted successfully",
        onSuccess: () => {
          state.data.value.queries = state.data.value.queries.filter(
            (q) => String(q.id) !== queryId
          );
          if (state.data.value.selectedQuery?.id === Number(queryId)) {
            state.data.value.selectedQuery = null;
          }
        }
      });
    });
  }

  // Reset state function
  function resetState() {
    state.data.value = {
      queries: [],
      selectedQuery: null,
      teams: [],
      selectedTeamId: null,
    };
  }

  return {
    // State
    isLoading: state.isLoading,
    error: state.error,
    
    // Computed properties
    queries,
    selectedQuery,
    teams,
    selectedTeamId,
    parseQueryContent,
    hasTeams,
    hasQueries,
    selectedTeam,
    
    // Actions
    fetchUserTeams,
    setSelectedTeam,
    fetchTeamQueries,
    fetchSourceQueries,
    fetchTeamSourceQueries,
    fetchQuery,
    createQuery,
    createSourceQuery,
    updateQuery,
    deleteQuery,
    resetState,
    
    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
  };
});
