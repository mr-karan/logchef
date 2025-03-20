import { defineStore } from "pinia";
import { ref, computed } from "vue";
import {
  savedQueriesApi,
  type SavedTeamQuery,
  type Team,
  type SavedQueryContent,
} from "@/api/savedQueries";
import { getErrorMessage } from "@/api/types";

export interface SavedQueriesState {
  queries: SavedTeamQuery[];
  selectedQuery: SavedTeamQuery | null;
  teams: Team[];
  selectedTeamId: number | null;
}

export const useSavedQueriesStore = defineStore("savedQueries", () => {
  // State
  const isLoading = ref(false);
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

  // Actions
  async function fetchUserTeams() {
    try {
      isLoading.value = true;
      const response = await savedQueriesApi.getUserTeams();
      if (response.status === "success") {
        data.value.teams = response.data;
        if (response.data.length > 0 && !data.value.selectedTeamId) {
          data.value.selectedTeamId = response.data[0].id;
        }
      }
      return response;
    } catch (error) {
      console.error("Error fetching user teams:", getErrorMessage(error));
      throw error;
    } finally {
      isLoading.value = false;
    }
  }

  function setSelectedTeam(teamId: number) {
    data.value.selectedTeamId = teamId;
  }

  async function fetchTeamQueries(teamId: number) {
    try {
      isLoading.value = true;
      const response = await savedQueriesApi.listQueries(teamId);
      if (response.status === "success") {
        // Handle null data (no queries available)
        data.value.queries = response.data || [];
      }
      return response;
    } catch (error) {
      console.error("Error fetching team queries:", getErrorMessage(error));
      throw error;
    } finally {
      isLoading.value = false;
    }
  }

  async function fetchSourceQueries(sourceId: number, teamId: number) {
    try {
      isLoading.value = true;
      const response = await savedQueriesApi.listSourceQueries(
        sourceId,
        teamId
      );
      if (response.status === "success") {
        // Handle null data (no queries available)
        data.value.queries = response.data || [];
      }
      return response;
    } catch (error) {
      console.error("Error fetching source queries:", getErrorMessage(error));
      throw error;
    } finally {
      isLoading.value = false;
    }
  }

  async function fetchTeamSourceQueries(teamId: number, sourceId: number) {
    try {
      isLoading.value = true;
      const response = await savedQueriesApi.listTeamSourceQueries(
        teamId,
        sourceId
      );
      if (response.status === "success") {
        // Update the store's state with the returned queries
        data.value.queries = response.data || [];
      }
      return response;
    } catch (error) {
      console.error("Error fetching team source queries:", getErrorMessage(error));
      throw error;
    } finally {
      isLoading.value = false;
    }
  }

  async function fetchQuery(teamId: number, queryId: string) {
    try {
      isLoading.value = true;
      const response = await savedQueriesApi.getQuery(teamId, queryId);
      if (response.status === "success") {
        data.value.selectedQuery = response.data;
      }
      return response;
    } catch (error) {
      console.error("Error fetching query:", getErrorMessage(error));
      throw error;
    } finally {
      isLoading.value = false;
    }
  }

  async function createQuery(
    teamId: number,
    query: Omit<SavedTeamQuery, "id" | "created_at" | "updated_at">
  ) {
    try {
      isLoading.value = true;
      const response = await savedQueriesApi.createQuery(teamId, query);
      if (response.status === "success") {
        // Ensure queries array exists before modifying it
        if (!data.value.queries) {
          data.value.queries = [];
        }
        data.value.queries.unshift(response.data);
        data.value.selectedQuery = response.data;
      }
      return response;
    } catch (error) {
      console.error("Error creating query:", getErrorMessage(error));
      throw error;
    } finally {
      isLoading.value = false;
    }
  }

  async function createSourceQuery(
    teamId: number,
    sourceId: number,
    name: string,
    description: string,
    queryContent: SavedQueryContent
  ) {
    try {
      isLoading.value = true;
      const query = {
        name,
        description,
        query_type: queryContent.queryType || "sql",
        query_content: JSON.stringify(queryContent),
      };
      const response = await savedQueriesApi.createSourceQuery(
        teamId,
        sourceId,
        query
      );
      if (response.status === "success") {
        // Ensure queries array exists before modifying it
        if (!data.value.queries) {
          data.value.queries = [];
        }
        data.value.queries.unshift(response.data);
        data.value.selectedQuery = response.data;
      }
      return response;
    } catch (error) {
      console.error("Error creating source query:", getErrorMessage(error));
      throw error;
    } finally {
      isLoading.value = false;
    }
  }

  async function updateQuery(
    teamId: number,
    queryId: string,
    query: Partial<SavedTeamQuery>
  ) {
    try {
      isLoading.value = true;
      const response = await savedQueriesApi.updateQuery(
        teamId,
        queryId,
        query
      );
      if (response.status === "success") {
        const index = data.value.queries.findIndex(
          (q) => String(q.id) === queryId
        );
        if (index >= 0) {
          data.value.queries[index] = response.data;
        }
        if (data.value.selectedQuery?.id === Number(queryId)) {
          data.value.selectedQuery = response.data;
        }
      }
      return response;
    } catch (error) {
      console.error("Error updating query:", getErrorMessage(error));
      throw error;
    } finally {
      isLoading.value = false;
    }
  }

  async function deleteQuery(teamId: number, queryId: string) {
    try {
      isLoading.value = true;
      const response = await savedQueriesApi.deleteQuery(teamId, queryId);
      if (response.status === "success") {
        data.value.queries = data.value.queries.filter(
          (q) => String(q.id) !== queryId
        );
        if (data.value.selectedQuery?.id === Number(queryId)) {
          data.value.selectedQuery = null;
        }
      }
      return response;
    } catch (error) {
      console.error("Error deleting query:", getErrorMessage(error));
      throw error;
    } finally {
      isLoading.value = false;
    }
  }

  // Reset state function (similar to explore.ts)
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
