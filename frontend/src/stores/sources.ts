import { defineStore } from "pinia";
import { computed } from "vue";
import { useTeamsStore } from "./teams";
import { sourcesApi } from "@/api/sources";
import type {
  Source,
  TeamGroupedQuery,
  CreateSourcePayload,
  SourceStats,
  CreateTeamQueryRequest,
} from "@/api/sources";
import type { SavedTeamQuery } from "@/api/types";
import { useBaseStore } from "./base";
import { getErrorMessage } from "@/api/types";
import { useSavedQueriesStore } from "./savedQueries";

interface SourcesState {
  sources: Source[];
  teamSources: Source[];
  sourceQueries: Record<string, any>;
  sourceStats: Record<string, SourceStats>;
  lastTeamId: number | null;
  loadingTeamId: number | null;
  pendingSourceRequests: Record<string, Promise<any>>;
}

export const useSourcesStore = defineStore("sources", () => {
  const teamsStore = useTeamsStore();

  const state = useBaseStore<SourcesState>({
    sources: [],
    teamSources: [],
    sourceQueries: {},
    sourceStats: {},
    lastTeamId: null,
    loadingTeamId: null,
    pendingSourceRequests: {},
  });
  
  // Initialize global cache if needed
  if (typeof window !== 'undefined' && !window.__sourceCache) {
    window.__sourceCache = {};
  }

  // Computed properties
  const sources = computed(() => state.data.value.sources);
  const teamSources = computed(() => state.data.value.teamSources);
  const sourceQueries = computed(() => state.data.value.sourceQueries);
  const sourceStats = computed(() => state.data.value.sourceStats);

  // Get sources for a specific team
  const teamSourcesMap = computed(() => {
    const map: Record<number, Source> = {};
    if (teamSources.value) {
      teamSources.value.forEach((source) => {
        map[source.id] = source;
      });
    }
    return map;
  });

  async function loadSources(forceReload = false) {
    // Skip if we already have sources and no force reload
    if (sources.value.length > 0 && !forceReload) {
      return { success: true, data: sources.value };
    }

    return await state.callApi<Source[]>({
      apiCall: () => sourcesApi.listSources(),
      onSuccess: (data) => {
        state.data.value.sources = data;
      },
      showToast: false,
    });
  }

  async function loadTeamSources(teamId: number, useCache = false) {
    // Skip loading if we already have sources for this team and useCache is true
    if (
      useCache &&
      teamSources.value.length > 0 &&
      state.data.value.lastTeamId === teamId
    ) {
      console.log(`Using cached sources for team ${teamId}`);
      return { success: true, data: teamSources.value };
    }

    // If we're already loading sources for this team, return the pending promise
    // to prevent duplicate API calls
    const requestKey = `team-${teamId}`;
    if (state.data.value.pendingSourceRequests[requestKey]) {
      console.log(`Already loading sources for team ${teamId}, reusing request`);
      return state.data.value.pendingSourceRequests[requestKey];
    }

    // Create a new request and store it
    const request = state.callApi<Source[]>({
      apiCall: () => sourcesApi.listTeamSources(teamId),
      onSuccess: (data) => {
        state.data.value.teamSources = data;
        state.data.value.lastTeamId = teamId;
        // Remove from pending requests
        delete state.data.value.pendingSourceRequests[requestKey];
      },
      onError: () => {
        // Remove from pending requests on error too
        delete state.data.value.pendingSourceRequests[requestKey];
      },
      showToast: false,
    });

    // Store the request
    state.data.value.pendingSourceRequests[requestKey] = request;
    
    return request;
  }

  async function loadSourceQueries(
    sourceId: string | number,
    refresh: boolean = false
  ) {
    const id = typeof sourceId === "string" ? parseInt(sourceId) : sourceId;
    const key = id.toString();

    // Return cached data if available and not refreshing
    if (state.data.value.sourceQueries[key] && !refresh) {
      return { success: true, data: state.data.value.sourceQueries[key] };
    }

    try {
      state.isLoading.value = true;
      
      // Use the savedQueriesStore to fetch queries
      const savedQueriesStore = useSavedQueriesStore();
      const result = await savedQueriesStore.fetchSourceQueries(
        id,
        teamsStore.currentTeamId
      );
      
      if (result.success && result.data) {
        state.data.value.sourceQueries = {
          ...state.data.value.sourceQueries,
          [key]: result.data
        };
        
        return {
          success: true,
          data: result.data
        };
      }
      
      return {
        success: false,
        error: result.error || "Failed to load source queries"
      };
    } catch (error) {
      console.error("Error loading source queries:", error);
      return {
        success: false,
        error: getErrorMessage(error)
      };
    } finally {
      state.isLoading.value = false;
    }
  }

  async function loadTeamSourceQueries(
    teamId: number,
    sourceId: number,
    refresh: boolean = false
  ) {
    const key = `${teamId}-${sourceId}`;

    // Return cached data if available and not refreshing
    if (state.data.value.sourceQueries[key] && !refresh) {
      return { success: true, data: state.data.value.sourceQueries[key] };
    }

    return await state.callApi<SavedTeamQuery[]>({
      apiCall: () => sourcesApi.listTeamSourceQueries(teamId, sourceId),
      onSuccess: (data) => {
        state.data.value.sourceQueries = {
          ...state.data.value.sourceQueries,
          [key]: data,
        };
      },
      showToast: false,
    });
  }

  async function createSourceQuery(
    sourceId: string | number,
    query: CreateTeamQueryRequest
  ) {
    const id = typeof sourceId === "string" ? parseInt(sourceId) : sourceId;

    const result = await state.callApi<SavedTeamQuery>({
      apiCall: () => sourcesApi.createSourceQuery(id, query),
      successMessage: "Query saved successfully",
    });

    if (result.success) {
      // Refresh queries for this source
      await loadSourceQueries(id, true);
    }

    return result;
  }

  async function createTeamSourceQuery(
    teamId: number,
    sourceId: number,
    query: Omit<CreateTeamQueryRequest, "team_id">
  ) {
    const result = await state.callApi<SavedTeamQuery>({
      apiCall: () => sourcesApi.createTeamSourceQuery(teamId, sourceId, query),
      successMessage: "Query saved successfully",
    });

    if (result.success) {
      // Refresh queries for this team source
      await loadTeamSourceQueries(teamId, sourceId, true);
    }

    return result;
  }

  async function createSource(payload: CreateSourcePayload) {
    const result = await state.callApi<Source>({
      apiCall: () => sourcesApi.createSource(payload),
      successMessage: "Source created successfully",
    });

    if (result.success) {
      // Refresh sources
      await loadSources(true);
    }

    return result;
  }

  async function deleteSource(id: number) {
    const result = await state.callApi<{ message: string }>({
      apiCall: () => sourcesApi.deleteSource(id),
      successMessage: "Source deleted successfully",
    });

    if (result.success) {
      // Refresh sources
      await loadSources(true);

      // Remove from local state
      state.data.value.sources = state.data.value.sources.filter(
        (source) => source.id !== id
      );
    }

    return result;
  }

  // Get sources not in a specific team
  function getSourcesNotInTeam(teamSourceIds: (string | number)[]) {
    const ids = new Set(
      teamSourceIds.map((id) => (typeof id === "string" ? parseInt(id) : id))
    );

    return sources.value.filter((source) => !ids.has(source.id));
  }

  async function getSourceStats(sourceId: number, forceReload = false) {
    const key = sourceId.toString();

    // Return cached data if available and not forcing reload
    if (state.data.value.sourceStats[key] && !forceReload) {
      return { success: true, data: state.data.value.sourceStats[key] };
    }

    return await state.callApi<SourceStats>({
      apiCall: () => sourcesApi.getSourceStats(sourceId),
      onSuccess: (data) => {
        state.data.value.sourceStats = {
          ...state.data.value.sourceStats,
          [key]: data,
        };
      },
      showToast: false,
    });
  }

  async function getTeamSourceStats(
    teamId: number,
    sourceId: number,
    forceReload = false
  ) {
    const key = `${teamId}-${sourceId}`;

    // Return cached data if available and not forcing reload
    if (state.data.value.sourceStats[key] && !forceReload) {
      return { success: true, data: state.data.value.sourceStats[key] };
    }

    return await state.callApi<SourceStats>({
      apiCall: () => sourcesApi.getTeamSourceStats(teamId, sourceId),
      onSuccess: (data) => {
        state.data.value.sourceStats = {
          ...state.data.value.sourceStats,
          [key]: data,
        };
      },
      showToast: false,
    });
  }

  async function getSource(sourceId: number, useCache = false) {
    const currentTeamId = teamsStore.currentTeamId;

    if (!currentTeamId) {
      return { success: false, error: "No team selected" };
    }

    // Check if we already have this source in teamSources with complete data
    if (useCache) {
      const cachedSource = teamSources.value.find(
        (s) => s.id === sourceId && s.columns && s.columns.length > 0
      );
      
      if (cachedSource) {
        console.log(`Using cached source from store for ID ${sourceId}`);
        return {
          success: true,
          data: cachedSource
        };
      }
    }
    
    // Check if we already have a pending request for this source
    const requestKey = `source-${currentTeamId}-${sourceId}`;
    if (state.data.value.pendingSourceRequests[requestKey]) {
      console.log(`Already fetching source ${sourceId}, reusing request`);
      return state.data.value.pendingSourceRequests[requestKey];
    }
    
    // Create a new request and store it
    const request = state.callApi<Source>({
      apiCall: () => sourcesApi.getTeamSource(currentTeamId, sourceId, useCache),
      onSuccess: (data) => {
        // Update the source in teamSources if it exists
        const index = teamSources.value.findIndex(s => s.id === sourceId);
        if (index >= 0) {
          // Create a new array to trigger reactivity
          const newTeamSources = [...teamSources.value];
          newTeamSources[index] = data;
          state.data.value.teamSources = newTeamSources;
        }
        // Remove from pending requests
        delete state.data.value.pendingSourceRequests[requestKey];
      },
      onError: () => {
        // Remove from pending requests on error too
        delete state.data.value.pendingSourceRequests[requestKey];
      },
      showToast: false,
    });
    
    // Store the request
    state.data.value.pendingSourceRequests[requestKey] = request;
    
    return request;
  }

  async function validateSourceConnection(connectionInfo: {
    host: string;
    username: string;
    password: string;
    database: string;
    table_name: string;
    timestamp_field?: string;
    severity_field?: string;
  }) {
    return await state.callApi<{ success: boolean; message: string }>({
      apiCall: () => sourcesApi.validateSourceConnection(connectionInfo),
      showToast: false,
    });
  }

  return {
    // State
    sources,
    teamSources,
    sourceQueries,
    isLoading: state.isLoading,
    error: state.error,
    teamSourcesMap,
    sourceStats,

    // Actions
    loadSources,
    loadTeamSources,
    loadSourceQueries,
    loadTeamSourceQueries,
    createSourceQuery,
    createTeamSourceQuery,
    createSource,
    deleteSource,
    getSourcesNotInTeam,
    getSourceStats,
    getTeamSourceStats,
    getSource,
    validateSourceConnection,
  };
});
