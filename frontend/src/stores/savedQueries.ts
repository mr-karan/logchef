import { defineStore } from "pinia";
import { computed, watch } from "vue";
import {
  savedQueriesApi,
  type SavedTeamQuery,
  type Team,
  type SavedQueryContent,
} from "@/api/savedQueries";
import { useBaseStore } from "./base";
import { useTeamsStore } from "./teams";
import { useContextStore } from "./context";

export interface SavedQueriesState {
  queries: SavedTeamQuery[];
  selectedQuery: SavedTeamQuery | null;
  teams: Team[];
}

export const useSavedQueriesStore = defineStore("savedQueries", () => {
  const state = useBaseStore<SavedQueriesState>({
    queries: [],
    selectedQuery: null,
    teams: [],
  });

  const contextStore = useContextStore();

  watch(
    [() => contextStore.teamId, () => contextStore.sourceId],
    () => {
      state.data.value.queries = [];
      state.data.value.selectedQuery = null;
    }
  );

  // Getters
  const parseQueryContent = (query: SavedTeamQuery): SavedQueryContent => {
    try {
      const content = JSON.parse(query.query_content) as Partial<SavedQueryContent> & { activeTab?: string; queryType?: string }; // Allow extra props during parsing

      // Type guard for absolute time range
      const isAbsoluteTimeRange = (tr: any): tr is { absolute: { start: number; end: number } } => {
        return tr && typeof tr === 'object' && tr.absolute && typeof tr.absolute.start === 'number' && typeof tr.absolute.end === 'number';
      };

      // Check if timeRange is explicitly null
      const isNullTimeRange = content.timeRange === null;

      // Provide defaults for required fields
      const defaults: SavedQueryContent = {
        version: content.version ?? 1,
        sourceId: content.sourceId ?? query.source_id, // Use query.source_id as fallback
        timeRange: isNullTimeRange
          ? null
          : (isAbsoluteTimeRange(content.timeRange)
              ? content.timeRange
              : { absolute: { start: Date.now() - 3600000, end: Date.now() } }),
        limit: typeof content.limit === 'number' ? content.limit : 100,
        content: typeof content.content === 'string' ? content.content : '',
        variables: Array.isArray(content.variables) ? content.variables : []
      };

      // Remove temporary fields before returning
      // delete defaults.activeTab; // Property 'activeTab' does not exist on type 'SavedQueryContent'
      // delete defaults.queryType; // Property 'queryType' does not exist on type 'SavedQueryContent'

      return defaults;
    } catch (e) {
      console.error("Error parsing query content:", e);
      // Return a default structure on error
      return {
        version: 1,
        sourceId: query.source_id,
        timeRange: {
          absolute: { start: Date.now() - 3600000, end: Date.now() },
        },
        limit: 100,
        content: '',
        variables: []
      };
    }
  };

  // Public helper method to use outside the store
  function parseQueryContentHelper(query: SavedTeamQuery): SavedQueryContent {
    return parseQueryContent(query);
  }

  const queries = computed(() => state.data.value.queries);
  const selectedQuery = computed(() => state.data.value.selectedQuery);
  const teams = computed(() => state.data.value.teams);
  const selectedTeamId = computed(() => contextStore.teamId);

  const hasTeams = computed(() => (teams.value?.length || 0) > 0);
  const hasQueries = computed(() => (queries.value?.length || 0) > 0);
  const selectedTeam = computed(() => teams.value?.find((t) => t.id === selectedTeamId.value) || null);

  // State was already initialized above

  async function fetchUserTeams() {
    return await state.withLoading('fetchUserTeams', async () => {
      return await state.callApi<Team[]>({
        apiCall: () => savedQueriesApi.getUserTeams(),
        operationKey: 'fetchUserTeams',
        onSuccess: (response) => {
          state.data.value.teams = response ?? [];
          if (response && response.length > 0 && !contextStore.teamId) {
            contextStore.selectTeam(response[0].id);
          }
        },
        defaultData: []
      });
    });
  }

  function setSelectedTeam(teamId: number) {
    contextStore.selectTeam(teamId);
  }

  async function fetchTeamCollections(teamId: number) {
    return await state.withLoading(`fetchTeamCollections-${teamId}`, async () => {
      return await state.callApi<SavedTeamQuery[]>({
        apiCall: () => savedQueriesApi.listTeamCollections(teamId),
        operationKey: `fetchTeamCollections-${teamId}`,
        onSuccess: (responseData) => {
          state.data.value.queries = responseData ?? [];
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function fetchTeamQueries(_teamId: number) {
    console.warn('fetchTeamQueries: This function is deprecated, use fetchTeamCollections instead');
    state.data.value.queries = [];
    return { success: true, data: [] };
  }

  async function fetchSourceQueries(sourceId: number, teamId: number) {
    // Delegate to the proper API method
    return await fetchTeamSourceQueries(teamId, sourceId);
  }

  async function fetchTeamSourceQueries(teamId: number, sourceId: number) {
    // Ensure sourceId is valid, otherwise return empty results gracefully
    if (!sourceId || sourceId <= 0) {
       console.warn(`fetchTeamSourceQueries: Invalid sourceId ${sourceId}, returning empty.`);
       state.data.value.queries = [];
       return { success: true, data: [] }; // Mimic successful empty response
    }

    // Validate that source belongs to the team (only if team sources are loaded)
    const teamsStore = useTeamsStore();
    const teamSources = teamsStore.getTeamSources(teamId);
    if (teamSources.length > 0 && !teamSources.some(source => source.id === sourceId)) {
       console.warn(`fetchTeamSourceQueries: Source ${sourceId} does not belong to team ${teamId}, returning empty.`);
       console.log(`Available sources for team ${teamId}:`, teamSources.map(s => s.id));
       state.data.value.queries = [];
       return { success: true, data: [] }; // Mimic successful empty response
    }
    return await state.withLoading(`fetchTeamSourceQueries-${teamId}-${sourceId}`, async () => {
      return await state.callApi<SavedTeamQuery[]>({ // Specify expected type
        apiCall: () => savedQueriesApi.listTeamSourceQueries(teamId, sourceId),
        operationKey: `fetchTeamSourceQueries-${teamId}-${sourceId}`,
        onSuccess: (responseData) => {
          // responseData is now SavedTeamQuery[] | null
          state.data.value.queries = responseData ?? []; // Use nullish coalescing
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  // *** Renamed and updated action ***
  async function fetchTeamSourceQueryDetails(teamId: number, sourceId: number, queryId: string) {
    return await state.withLoading(`fetchTeamSourceQueryDetails-${teamId}-${sourceId}-${queryId}`, async () => {
      return await state.callApi<SavedTeamQuery>({ // Specify expected type (single query)
        apiCall: () => savedQueriesApi.getTeamSourceQuery(teamId, sourceId, queryId), // Use correct API function
        operationKey: `fetchTeamSourceQueryDetails-${teamId}-${sourceId}-${queryId}`,
        onSuccess: (response) => {
          // response is now SavedTeamQuery | null
          state.data.value.selectedQuery = response; // Assign directly (can be null)
        },
        // No defaultData needed for single object fetch? Or provide null?
        // defaultData: null // Explicitly set default if needed
      });
    });
  }

  async function createQuery(
    _teamId: number,
    _query: Omit<SavedTeamQuery, "id" | "created_at" | "updated_at">
  ) {
    // Deprecated: use createSourceQuery instead which includes sourceId
    console.warn('createQuery: This function is deprecated, use createSourceQuery instead');
    return { success: false, error: { message: 'This function is deprecated, use createSourceQuery instead' } };
  }

  async function createSourceQuery(
    teamId: number,
    sourceId: number,
    name: string,
    description: string,
    queryContent: SavedQueryContent,
    queryType: string
  ) {
    return await state.withLoading(`createSourceQuery-${teamId}-${sourceId}`, async () => {
      // Make a clean copy of the queryContent without any query_type field
      const apiQueryContent = { ...queryContent };

      // Ensure we use the explicit queryType parameter
      const query = {
        name,
        description,
        query_type: queryType, // Use the explicitly provided queryType parameter
        query_content: JSON.stringify(apiQueryContent),
      };

      return await state.callApi<SavedTeamQuery>({
        apiCall: () => savedQueriesApi.createTeamSourceQuery(teamId, sourceId, query),
        operationKey: `createSourceQuery-${teamId}-${sourceId}`,
        successMessage: "Query created successfully",
        onSuccess: (response) => {
          if (response) {
            if (!state.data.value.queries) {
              state.data.value.queries = [];
            }
            state.data.value.queries.unshift(response);
            state.data.value.selectedQuery = response;
          }
        }
      });
    });
  }

  async function updateQuery(
    _teamId: number,
    _queryId: string,
    _query: Partial<SavedTeamQuery>
  ) {
    // Deprecated: use updateTeamSourceQuery instead which includes sourceId
    console.warn('updateQuery: This function is deprecated, use updateTeamSourceQuery instead');
    return { success: false, error: { message: 'This function is deprecated, use updateTeamSourceQuery instead' } };
  }

  async function updateTeamSourceQuery(
    teamId: number,
    sourceId: number,
    queryId: string,
    query: Partial<Omit<SavedTeamQuery, "id" | "team_id" | "source_id" | "created_at" | "updated_at">>
  ) {
    return await state.withLoading(`updateTeamSourceQuery-${teamId}-${sourceId}-${queryId}`, async () => {
      return await state.callApi<SavedTeamQuery>({ // Specify expected type
        apiCall: () => savedQueriesApi.updateTeamSourceQuery(teamId, sourceId, queryId, query),
        operationKey: `updateTeamSourceQuery-${teamId}-${sourceId}-${queryId}`,
        successMessage: "Query updated successfully",
        onSuccess: (response) => {
          // response is now SavedTeamQuery | null
          if (response) {
            const index = state.data.value.queries.findIndex(
              (q) => String(q.id) === queryId
            );
            if (index >= 0) {
              // Merge updates carefully, ensuring types match
              state.data.value.queries[index] = {
                ...state.data.value.queries[index], // Keep existing fields
                ...response // Overwrite with fields from response
              };
            }
            if (state.data.value.selectedQuery?.id === Number(queryId)) {
              // Merge updates for selectedQuery as well
              state.data.value.selectedQuery = {
                ...state.data.value.selectedQuery, // Keep existing fields (like id, team_id etc)
                ...response // Overwrite with fields from response
              };
            }
          }
        }
      });
    });
  }

  async function deleteQuery(teamId: number, sourceId: number, queryId: string) {
    return await state.withLoading(`deleteQuery-${teamId}-${sourceId}-${queryId}`, async () => {
      // Delete API might return different structure, adjust type if needed
      return await state.callApi<{ success: boolean }>({ // Specify expected type
        apiCall: () => savedQueriesApi.deleteTeamSourceQuery(teamId, sourceId, queryId),
        operationKey: `deleteQuery-${teamId}-${sourceId}-${queryId}`,
        successMessage: "Query deleted successfully",
        onSuccess: (response) => {
          // response is { success: boolean } | null
          if (response?.success) {
            state.data.value.queries = state.data.value.queries.filter(
              (q) => String(q.id) !== queryId
            );
            if (state.data.value.selectedQuery?.id === Number(queryId)) {
              state.data.value.selectedQuery = null;
            }
          }
        }
      });
    });
  }

  async function toggleBookmark(teamId: number, sourceId: number, queryId: number) {
    return await state.withLoading(`toggleBookmark-${teamId}-${sourceId}-${queryId}`, async () => {
      return await state.callApi<{ is_bookmarked: boolean; message: string }>({
        apiCall: () => savedQueriesApi.toggleBookmark(teamId, sourceId, queryId),
        operationKey: `toggleBookmark-${teamId}-${sourceId}-${queryId}`,
        onSuccess: (response) => {
          if (response) {
            // Update the bookmark status in the queries list
            const index = state.data.value.queries.findIndex(
              (q) => q.id === queryId
            );
            if (index >= 0) {
              state.data.value.queries[index].is_bookmarked = response.is_bookmarked;
              // Update updated_at locally so sorting reflects the change
              state.data.value.queries[index].updated_at = new Date().toISOString();
              // Re-sort to match backend order: bookmarked first, then by updated_at desc
              state.data.value.queries.sort((a, b) => {
                // Bookmarked queries come first
                if (a.is_bookmarked !== b.is_bookmarked) {
                  return a.is_bookmarked ? -1 : 1;
                }
                // Then sort by updated_at descending
                return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime();
              });
            }
            // Update selectedQuery if it matches
            if (state.data.value.selectedQuery?.id === queryId) {
              state.data.value.selectedQuery.is_bookmarked = response.is_bookmarked;
            }
          }
        },
        showToast: false, // No toast for bookmark toggle - visual feedback via star icon
      });
    });
  }

  function resetState() {
    state.data.value = {
      queries: [],
      selectedQuery: null,
      teams: [],
    };
    state.error.value = null;
  }

  return {
    // State
    isLoading: state.isLoading,
    error: state.error,
    data: state.data.value, // Directly expose data for backward compatibility

    // Computed properties
    queries,
    selectedQuery,
    teams,
    selectedTeamId,
    parseQueryContent: parseQueryContentHelper, // Expose the helper function
    hasTeams,
    hasQueries,
    selectedTeam,

    // Actions
    fetchUserTeams,
    setSelectedTeam,
    fetchTeamCollections,
    fetchTeamQueries,
    fetchSourceQueries,
    fetchTeamSourceQueries,
    fetchTeamSourceQueryDetails,
    createQuery,
    createSourceQuery,
    updateQuery,
    updateTeamSourceQuery,
    deleteQuery,
    toggleBookmark,
    resetState,

    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
  };
});
