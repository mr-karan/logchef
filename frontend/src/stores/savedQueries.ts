import { defineStore } from "pinia";
import { ref, computed } from "vue";
import {
  savedQueriesApi,
  type SavedTeamQuery,
  type Team,
  type SavedQueryContent,
} from "@/api/savedQueries";
import { useApiQuery } from "@/composables/useApiQuery";
import { useLoadingState } from "@/composables/useLoadingState";
import { useBaseStore } from "./base";

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

  // Use our composables
  const { execute, isLoading } = useApiQuery();
  
  // Local state for data binding
  const data = ref<SavedQueriesState>({
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
  const hasTeams = computed(() => (data.value.teams?.length || 0) > 0);
  const hasQueries = computed(() => (data.value.queries?.length || 0) > 0);
  const selectedTeam = computed(() => {
    return (
      data.value.teams?.find((t) => t.id === data.value.selectedTeamId) || null
    );
  });

  // State was already initialized above

  async function fetchUserTeams() {
    return await state.withLoading('fetchUserTeams', async () => {
      return await state.callApi({
        apiCall: () => savedQueriesApi.getUserTeams(),
        operationKey: 'fetchUserTeams',
        onSuccess: (response) => {
          data.value.teams = response;
          if (response && response.length > 0 && !data.value.selectedTeamId) {
            data.value.selectedTeamId = response[0].id;
          }
        }
      });
    });
  }

  function setSelectedTeam(teamId: number) {
    data.value.selectedTeamId = teamId;
  }

  async function fetchTeamQueries(teamId: number) {
    return await state.withLoading(`fetchTeamQueries-${teamId}`, async () => {
      return await state.callApi({
        apiCall: () => savedQueriesApi.listQueries(teamId),
        operationKey: `fetchTeamQueries-${teamId}`,
        onSuccess: (responseData) => {
          data.value.queries = responseData;
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
          data.value.queries = responseData;
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
          data.value.queries = responseData;
        },
        defaultData: [], // Ensure empty array fallback
        showToast: false,
      });
    });
  }

  async function fetchQuery(teamId: number, queryId: string) {
    return await withLoading(`fetchQuery-${teamId}-${queryId}`, async () => {
      return await execute(() => savedQueriesApi.getQuery(teamId, queryId), {
        onSuccess: (response) => {
          data.value.selectedQuery = response;
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
          if (!data.value.queries) {
            data.value.queries = [];
          }
          data.value.queries.unshift(response);
          data.value.selectedQuery = response;
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
          if (!data.value.queries) {
            data.value.queries = [];
          }
          data.value.queries.unshift(response);
          data.value.selectedQuery = response;
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
          const index = data.value.queries.findIndex(
            (q) => String(q.id) === queryId
          );
          if (index >= 0) {
            data.value.queries[index] = response;
          }
          if (data.value.selectedQuery?.id === Number(queryId)) {
            data.value.selectedQuery = response;
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
          data.value.queries = data.value.queries.filter(
            (q) => String(q.id) !== queryId
          );
          if (data.value.selectedQuery?.id === Number(queryId)) {
            data.value.selectedQuery = null;
          }
        }
      });
    });
  }

  // Reset state function
  function resetState() {
    data.value = {
      queries: [], // Ensure this is always initialized as an empty array
      selectedQuery: null,
      teams: [],
      selectedTeamId: null,
    };
  }

  return {
    // State
    isLoading: state.isLoading,
    error: state.error,
    data,
    
    // Computed properties
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
