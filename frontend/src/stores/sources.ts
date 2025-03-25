import { defineStore } from "pinia";
import { computed, ref, reactive } from "vue";
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
import { useSavedQueriesStore } from "./savedQueries";
import { useApiQuery } from "@/composables/useApiQuery";
import { useLoadingState } from "@/composables/useLoadingState";

interface SourcesState {
  sources: Source[];
  teamSources: Source[];
  sourceQueries: Record<string, any>;
  sourceStats: Record<string, SourceStats>;
}

export const useSourcesStore = defineStore("sources", () => {
  const teamsStore = useTeamsStore();
  const { execute } = useApiQuery();
  const { withLoading, isLoadingOperation } = useLoadingState();

  const state = useBaseStore<SourcesState>({
    sources: [],
    teamSources: [],
    sourceQueries: {},
    sourceStats: {},
  });

  // Computed properties
  const sources = computed(() => state.data.value.sources);
  const teamSources = computed(() => state.data.value.teamSources);
  const sourceQueries = computed(() => state.data.value.sourceQueries);
  const sourceStats = computed(() => state.data.value.sourceStats);
  
  // Filtered sources
  const visibleSources = computed(() => 
    sources.value.filter(source => !source.archived)
  );
  
  // Source getters
  const getSourceById = computed(() => (id: number) => 
    sources.value.find(source => source.id === id)
  );
  
  const getTeamSourceById = computed(() => (id: number) =>
    teamSources.value.find(source => source.id === id)
  );
  
  // Track validated connections
  const validatedConnections = reactive(new Set<string>());

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

  async function loadSources() {
    return await state.withLoading("loadSources", async () => {
      state.error.value = null; // Reset error before loading
      return await execute(() => sourcesApi.listSources(), {
        onSuccess: (data) => {
          state.data.value.sources = data ?? [];
        },
        onError: (error) => {
          state.error.value = { message: error.message, operation: 'loadSources' };
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function loadTeamSources(teamId: number) {
    return await state.withLoading(`loadTeamSources-${teamId}`, async () => {
      return await execute(() => sourcesApi.listTeamSources(teamId), {
        onSuccess: (data) => {
          state.data.value.teamSources = data ?? [];
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function loadSourceQueries(sourceId: string | number) {
    const id = typeof sourceId === "string" ? parseInt(sourceId) : sourceId;

    return await state.withLoading(`loadSourceQueries-${id}`, async () => {
      // Use the savedQueriesStore to fetch queries
      const savedQueriesStore = useSavedQueriesStore();
      const currentTeamId = teamsStore.currentTeamId;

      if (!currentTeamId) {
        return {
          success: false,
          error: "No team selected",
        };
      }

      const result = await savedQueriesStore.fetchSourceQueries(
        id,
        currentTeamId
      );

      if (result && result.success && result.data) {
        state.data.value.sourceQueries = {
          ...state.data.value.sourceQueries,
          [id.toString()]: result.data,
        };

        return {
          success: true,
          data: result.data,
        };
      }

      return {
        success: false,
        error: result.error || "Failed to load source queries",
      };
    });
  }

  async function loadTeamSourceQueries(teamId: number, sourceId: number) {
    const key = `${teamId}-${sourceId}`;

    return await state.withLoading(`loadTeamSourceQueries-${key}`, async () => {
      return await execute(() => sourcesApi.listTeamSourceQueries(teamId, sourceId), {
        onSuccess: (data) => {
          state.data.value.sourceQueries = {
            ...state.data.value.sourceQueries,
            [key]: data,
          };
        },
        showToast: false,
      });
    });
  }

  async function createSourceQuery(
    sourceId: string | number,
    query: CreateTeamQueryRequest
  ) {
    const id = typeof sourceId === "string" ? parseInt(sourceId) : sourceId;

    const result = await state.withLoading(`createSourceQuery-${id}`, async () => {
      return await execute(() => sourcesApi.createSourceQuery(id, query), {
        successMessage: "Query saved successfully",
      });
    });

    if (result.success) {
      // Refresh queries for this source
      await loadSourceQueries(id);
    }

    return result;
  }

  async function createTeamSourceQuery(
    teamId: number,
    sourceId: number,
    query: Omit<CreateTeamQueryRequest, "team_id">
  ) {
    const key = `${teamId}-${sourceId}`;
    
    const result = await state.withLoading(`createTeamSourceQuery-${key}`, async () => {
      return await execute(() => sourcesApi.createTeamSourceQuery(teamId, sourceId, query), {
        successMessage: "Query saved successfully",
      });
    });

    if (result.success) {
      // Refresh queries for this team source
      await loadTeamSourceQueries(teamId, sourceId);
    }

    return result;
  }

  async function createSource(payload: CreateSourcePayload) {
    const result = await state.withLoading("createSource", async () => {
      return await execute(() => sourcesApi.createSource(payload), {
        successMessage: "Source created successfully",
      });
    });

    if (result.success) {
      // Refresh sources
      await loadSources();
    }

    return result;
  }

  async function deleteSource(id: number) {
    const result = await state.withLoading(`deleteSource-${id}`, async () => {
      return await execute(() => sourcesApi.deleteSource(id), {
        successMessage: "Source deleted successfully",
        onSuccess: () => {
          // Remove from local state
          state.data.value.sources = state.data.value.sources.filter(
            (source) => source.id !== id
          );
        }
      });
    });

    if (result.success) {
      // Refresh sources
      await loadSources();
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

  async function getSourceStats(sourceId: number) {
    return await state.withLoading(`getSourceStats-${sourceId}`, async () => {
      return await execute(() => sourcesApi.getSourceStats(sourceId), {
        onSuccess: (data) => {
          state.data.value.sourceStats = {
            ...state.data.value.sourceStats,
            [sourceId.toString()]: data,
          };
        },
        showToast: false,
      });
    });
  }

  async function getTeamSourceStats(teamId: number, sourceId: number) {
    const key = `${teamId}-${sourceId}`;

    return await state.withLoading(`getTeamSourceStats-${key}`, async () => {
      return await execute(() => sourcesApi.getTeamSourceStats(teamId, sourceId), {
        onSuccess: (data) => {
          state.data.value.sourceStats = {
            ...state.data.value.sourceStats,
            [key]: data,
          };
        },
        showToast: false,
      });
    });
  }

  async function getSource(sourceId: number) {
    const currentTeamId = teamsStore.currentTeamId;

    if (!currentTeamId) {
      return { success: false, error: "No team selected" };
    }

    return await state.withLoading(`getSource-${sourceId}`, async () => {
      return await execute(() => sourcesApi.getTeamSource(currentTeamId, sourceId), {
        onSuccess: (data) => {
          // Update the source in teamSources if it exists
          const index = teamSources.value.findIndex((s) => s.id === sourceId);
          if (index >= 0) {
            // Create a new array to trigger reactivity
            const newTeamSources = [...teamSources.value];
            newTeamSources[index] = data;
            state.data.value.teamSources = newTeamSources;
          }
        },
        showToast: true,
      });
    });
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
    const connectionKey = `${connectionInfo.host}-${connectionInfo.database}-${connectionInfo.table_name}`;
    
    return await state.withLoading("validateSourceConnection", async () => {
      const result = await execute(() => sourcesApi.validateSourceConnection(connectionInfo), {
        showToast: true,
      });
      
      if (result.success) {
        validatedConnections.add(connectionKey);
      }
      
      return result;
    });
  }
  
  function isConnectionValidated(host: string, database: string, tableName: string): boolean {
    const connectionKey = `${host}-${database}-${tableName}`;
    return validatedConnections.has(connectionKey);
  }
  
  function invalidateSourceCache(sourceId: number) {
    state.data.value.sources = state.data.value.sources.filter(
      s => s.id !== sourceId
    );
    state.data.value.teamSources = state.data.value.teamSources.filter(
      s => s.id !== sourceId
    );
    
    // Also clear any stats for this source
    delete state.data.value.sourceStats[sourceId.toString()];
  }
  
  function handleError(error: Error, operation: string) {
    console.error(`Error in sources store (${operation}):`, error);
    state.error.value = {
      message: error.message,
      operation
    };
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
    loadingStates: state.loadingStates,
    validatedConnections,
    visibleSources,

    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
    isLoadingSource: (sourceId: number) =>
      state.isLoadingOperation(`getSource-${sourceId}`),
    isLoadingTeamSources: (teamId: number) =>
      state.isLoadingOperation(`loadTeamSources-${teamId}`),

    // Getters
    getSourceById,
    getTeamSourceById,
    isConnectionValidated,

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
    invalidateSourceCache,
    handleError,
  };
});
