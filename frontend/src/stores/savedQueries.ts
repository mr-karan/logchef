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

export interface SavedQueriesState {
  queries: SavedTeamQuery[];
  selectedQuery: SavedTeamQuery | null;
  teams: Team[];
  selectedTeamId: number | null;
}

export const useSavedQueriesStore = defineStore("savedQueries", () => {
  // State
  const data = ref<SavedQueriesState>({
    queries: [],
    selectedQuery: null,
    teams: [],
    selectedTeamId: null,
  });

  // Use our composables
  const { execute, isLoading } = useApiQuery();
  const { withLoading } = useLoadingState();

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

  // Actions
  async function fetchUserTeams() {
    return await withLoading('fetchUserTeams', async () => {
      return await execute(() => savedQueriesApi.getUserTeams(), {
        onSuccess: (response) => {
          data.value.teams = response;
          if (response.length > 0 && !data.value.selectedTeamId) {
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
    return await withLoading(`fetchTeamQueries-${teamId}`, async () => {
      return await execute(
        () => savedQueriesApi.listQueries(teamId),
        {
          onSuccess: (responseData) => {
            data.value.queries = responseData;
          },
          defaultData: [],
          showToast: false,
        }
      );
    });
  }

  async function fetchSourceQueries(sourceId: number, teamId: number) {
    return await withLoading(`fetchSourceQueries-${sourceId}-${teamId}`, async () => {
      return await execute(
        () => savedQueriesApi.listSourceQueries(sourceId, teamId),
        {
          onSuccess: (responseData) => {
            data.value.queries = responseData;
          },
          defaultData: [],
          showToast: false,
        }
      );
    });
  }

  async function fetchTeamSourceQueries(teamId: number, sourceId: number) {
    return await withLoading(`fetchTeamSourceQueries-${teamId}-${sourceId}`, async () => {
      return await execute(
        () => savedQueriesApi.listTeamSourceQueries(teamId, sourceId),
        {
          onSuccess: (responseData) => {
            // ResponseData is already null-safe due to defaultData
            data.value.queries = responseData;
          },
          defaultData: [], // Ensure empty array fallback
          showToast: false,
        }
      );
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
    return await withLoading(`createQuery-${teamId}`, async () => {
      return await execute(() => savedQueriesApi.createQuery(teamId, query), {
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
    return await withLoading(`createSourceQuery-${teamId}-${sourceId}`, async () => {
      const query = {
        name,
        description,
        query_type: queryContent.queryType || "sql",
        query_content: JSON.stringify(queryContent),
      };
      
      return await execute(() => savedQueriesApi.createSourceQuery(teamId, sourceId, query), {
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
    return await withLoading(`updateQuery-${teamId}-${queryId}`, async () => {
      return await execute(() => savedQueriesApi.updateQuery(teamId, queryId, query), {
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
    return await withLoading(`deleteQuery-${teamId}-${queryId}`, async () => {
      return await execute(() => savedQueriesApi.deleteQuery(teamId, queryId), {
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
    isLoading,
    data,
    parseQueryContent,
    hasTeams,
    hasQueries,
    selectedTeam,
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
  };
});
