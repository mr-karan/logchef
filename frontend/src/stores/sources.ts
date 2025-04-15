import { defineStore } from "pinia";
import { computed, ref, reactive } from "vue";
import { useTeamsStore } from "./teams";
import { sourcesApi } from "@/api/sources";
import type {
  Source,
  CreateSourcePayload,
  SourceStats,
  CreateTeamQueryRequest,
} from "@/api/sources";
import type { APIErrorResponse } from "@/api/types";
import { useBaseStore } from "./base";
import { useSavedQueriesStore } from "./savedQueries";

interface SourcesState {
  sources: Source[];
  teamSources: Source[];
  sourceQueries: Record<string, any>;
  sourceStats: Record<string, SourceStats>;
  currentSourceDetails: Source | null;
}

export const useSourcesStore = defineStore("sources", () => {
  const teamsStore = useTeamsStore();

  const state = useBaseStore<SourcesState>({
    sources: [],
    teamSources: [],
    sourceQueries: {},
    sourceStats: {},
    currentSourceDetails: null
  });

  // Computed properties
  const sources = computed(() => state.data.value.sources);
  const teamSources = computed(() => state.data.value.teamSources);
  const sourceQueries = computed(() => state.data.value.sourceQueries);
  const sourceStats = computed(() => state.data.value.sourceStats);
  const currentSourceDetails = computed(() => state.data.value.currentSourceDetails);

  // Filtered sources
  const visibleSources = computed(() =>
    sources.value.filter(source => {
      // Safe check for archived property, which might not exist on the Source type
      return !(source as any).archived;
    })
  );

  // Source getters
  const getSourceById = computed(() => (id: number) =>
    sources.value.find(source => source.id === id)
  );

  const getTeamSourceById = computed(() => (id: number) =>
    teamSources.value.find(source => source.id === id)
  );

  // Source stats getter
  const getSourceStatsById = computed(() => (id: number) =>
    sourceStats.value[id.toString()]
  );

  // Track validated connections
  const validatedConnections = reactive(new Set<string>());

  // Hydration state
  const isHydrated = ref(false);

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

  // Check if current source is valid for querying (connected according to the backend check)
  const hasValidCurrentSource = computed(() => {
    const details = state.data.value.currentSourceDetails;
    // A source is considered valid for querying if the backend check marked it as connected.
    // We still need the details object to exist.
    return !!(details && details.is_connected === true);
  });

  // Get formatted table name from current source details
  const getCurrentSourceTableName = computed(() => {
    const details = state.data.value.currentSourceDetails;
    if (details?.connection?.database && details?.connection?.table_name) {
      return `${details.connection.database}.${details.connection.table_name}`;
    }
    return null; // Or a default/placeholder
  });

  async function loadSources() {
    // Use admin endpoint for listing sources
    return await loadAllSourcesForAdmin();
  }

  // Function specifically for Admin views to load ALL sources
  async function loadAllSourcesForAdmin() {
    return await state.withLoading("loadAllSourcesForAdmin", async () => {
      state.error.value = null; // Reset error before loading
      return await state.callApi({
        apiCall: () => sourcesApi.listAllSourcesForAdmin(),
        operationKey: "loadAllSourcesForAdmin",
        onSuccess: (response) => {
          const sourcesData = Array.isArray(response) ? response : (response as any)?.data || [];
          state.data.value.sources = sourcesData as Source[]; // Ensure type
        }
      });
    });
  }

  // Hydrate the store
  async function hydrate() {
    if (!isHydrated.value && sources.value.length === 0) {
      await loadSources();
    }
    return isHydrated.value;
  }

  async function loadTeamSources(teamId: number) {
    return await state.withLoading(`loadTeamSources-${teamId}`, async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.listTeamSources(teamId),
        operationKey: `loadTeamSources-${teamId}`,
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
        return state.handleError(
          {
            status: "error",
            message: "No team selected",
            error_type: "ValidationError"
          } as APIErrorResponse,
          `loadSourceQueries-${id}`
        );
      }

      return await state.callApi({
        apiCall: () => savedQueriesStore.fetchSourceQueries(id, currentTeamId),
        operationKey: `loadSourceQueries-${id}`,
        onSuccess: (result) => {
          if (result) {
            state.data.value.sourceQueries = {
              ...state.data.value.sourceQueries,
              [id.toString()]: result,
            };
          }
        },
        showToast: false,
      });
    });
  }

  // Function to load source queries
  async function loadTeamSourceQueries(teamId: number, sourceId: number) {
    return await state.withLoading(`loadTeamSourceQueries-${sourceId}`, async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.listTeamSourceQueries(teamId, sourceId),
        operationKey: `loadTeamSourceQueries-${sourceId}`,
        onSuccess: (data) => {
          state.data.value.sourceQueries = {
            ...state.data.value.sourceQueries,
            [sourceId]: data
          };
        },
        showToast: false,
      });
    });
  }

  // Create a new query for a team's source
  async function createTeamSourceQuery(teamId: number, sourceId: number, queryData: Omit<CreateTeamQueryRequest, "team_id">) {
    return await state.withLoading(`createTeamSourceQuery-${sourceId}`, async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.createTeamSourceQuery(teamId, sourceId, queryData),
        operationKey: `createTeamSourceQuery-${sourceId}`,
        successMessage: "Query saved successfully",
        onSuccess: (data) => {
          // Add to existing queries if we have them loaded
          if (state.data.value.sourceQueries[sourceId]) {
            state.data.value.sourceQueries = {
              ...state.data.value.sourceQueries,
              [sourceId]: [...state.data.value.sourceQueries[sourceId], data]
            };
          }
        }
      });
    });
  }

  async function createSource(payload: CreateSourcePayload) {
    const result = await state.withLoading("createSource", async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.createSource(payload),
        successMessage: "Source created successfully",
        operationKey: "createSource"
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
      return await state.callApi({
        apiCall: () => sourcesApi.deleteSource(id),
        successMessage: "Source deleted successfully",
        operationKey: `deleteSource-${id}`,
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

  // Get stats for a source (admin-only version)
  async function getSourceStats(sourceId: number) {
    return await state.withLoading(`getSourceStats-${sourceId}`, async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.getAdminSourceStats(sourceId),
        operationKey: `getSourceStats-${sourceId}`,
        onSuccess: (data: any) => {
          state.data.value.sourceStats = {
            ...state.data.value.sourceStats,
            [sourceId.toString()]: data as SourceStats,
          };
        },
        showToast: false,
      });
    });
  }

  // Get team-scoped stats for a source
  async function getTeamSourceStats(teamId: number, sourceId: number) {
    return await state.withLoading(`getTeamSourceStats-${teamId}-${sourceId}`, async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.getTeamSourceStats(teamId, sourceId),
        operationKey: `getTeamSourceStats-${teamId}-${sourceId}`,
        onSuccess: (data: any) => {
          state.data.value.sourceStats = {
            ...state.data.value.sourceStats,
            [sourceId.toString()]: data as SourceStats,
          };
        },
        showToast: false,
      });
    });
  }

  async function getSource(sourceId: number) {
    const currentTeamId = teamsStore.currentTeamId;

    if (!currentTeamId) {
      return state.handleError(
        {
          status: "error",
          message: "No team selected",
          error_type: "ValidationError"
        } as APIErrorResponse,
        `getSource-${sourceId}`
      );
    }

    return await state.withLoading(`getSource-${sourceId}`, async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.getTeamSource(currentTeamId, sourceId),
        operationKey: `getSource-${sourceId}`,
        onSuccess: (data: any) => {
          if (!data) return;

          // Update the source in teamSources if it exists
          const index = teamSources.value.findIndex((s) => s.id === sourceId);
          if (index >= 0) {
            // Create a new array to trigger reactivity
            const newTeamSources = [...teamSources.value];
            newTeamSources[index] = data as Source;
            state.data.value.teamSources = newTeamSources;
          }
        },
        showToast: true,
      });
    });
  }

  async function loadSourceDetails(sourceId: number) {
    // Use a unique loading key
    const loadingKey = `loadSourceDetails-${sourceId}`;

    // Check cache first (optional but recommended)
    const cachedSource = state.data.value.teamSources.find(s => s.id === sourceId);
    if (cachedSource && cachedSource.columns && cachedSource.columns.length > 0) {
       // Check if current details are already set to this source to avoid redundant updates
       if (state.data.value.currentSourceDetails?.id !== sourceId) {
          state.data.value.currentSourceDetails = cachedSource;
       }
       // Return success immediately if cached
       return { success: true, data: cachedSource };
    }

    return await state.withLoading(loadingKey, async () => {
      // Reset current details before fetching (only if not already loading this specific source)
      if (!state.isLoadingOperation(loadingKey)) {
         state.data.value.currentSourceDetails = null;
      }

      // Use the existing getSource function which seems to handle team context
      const currentTeamId = teamsStore.currentTeamId;
      if (!currentTeamId) {
        return state.handleError(
          { status: "error", message: "No team selected", error_type: "ValidationError" } as APIErrorResponse,
          loadingKey
        );
      }

      return await state.callApi<Source>({
        // Use getTeamSource API call as it seems to be the one implemented
        apiCall: () => sourcesApi.getTeamSource(currentTeamId, sourceId),
        onSuccess: (data: any) => {
          if (!data) return;

          state.data.value.currentSourceDetails = data as Source;
          // Update the source in teamSources as well (like the existing getSource does)
          const index = state.data.value.teamSources.findIndex((s) => s.id === sourceId);
          if (index >= 0) {
            const newTeamSources = [...state.data.value.teamSources];
            newTeamSources[index] = data as Source;
            state.data.value.teamSources = newTeamSources;
          }
        },
        onError: (error) => {
          state.data.value.currentSourceDetails = null; // Clear on error
        },
        operationKey: loadingKey,
        showToast: false, // Don't show toast for background loading
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
      return await state.callApi({
        apiCall: () => sourcesApi.validateSourceConnection(connectionInfo),
        operationKey: "validateSourceConnection",
        showToast: true,
        onSuccess: () => {
          validatedConnections.add(connectionKey);
        }
      });
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

    // Clear current source details if it matches this source
    if (state.data.value.currentSourceDetails?.id === sourceId) {
      state.data.value.currentSourceDetails = null;
    }
  }

  function clearCurrentSourceDetails() {
    state.data.value.currentSourceDetails = null;
  }

  // Use the centralized error handler from base store

  // Update source
  async function updateSource(id: number, payload: Partial<Source>) {
    return await state.withLoading(`updateSource-${id}`, async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.updateSource(id, payload),
        successMessage: "Source updated successfully",
        operationKey: `updateSource-${id}`,
        onSuccess: (data) => {
          // Ensure data is treated as a Source object
          const updatedSourceData = data as Source | null;
          if (!updatedSourceData) return;

          // Update in local state
          const index = sources.value.findIndex(s => s.id === id);
          if (index >= 0) {
            const updatedSource = { ...sources.value[index], ...updatedSourceData };
            state.data.value.sources = [
              ...sources.value.slice(0, index),
              updatedSource,
              ...sources.value.slice(index + 1)
            ];
          }

          // Also update in team sources if present
          const teamIndex = teamSources.value.findIndex(s => s.id === id);
          if (teamIndex >= 0) {
            const updatedTeamSource = { ...teamSources.value[teamIndex], ...updatedSourceData };
            state.data.value.teamSources = [
              ...teamSources.value.slice(0, teamIndex),
              updatedTeamSource,
              ...teamSources.value.slice(teamIndex + 1)
            ];
          }

          // Invalidate cache for this source
          invalidateSourceCache(id);
        }
      });
    });
  }

  // Load the schema for a source
  async function getSourceSchema(teamId: number, sourceId: number) {
    return await state.withLoading(`getSourceSchema-${sourceId}`, async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.getTeamSourceSchema(teamId, sourceId),
        operationKey: `getSourceSchema-${sourceId}`,
        onSuccess: (data: string | null) => {
          // Handle null case
          if (data === null) return;

          const schema = data;

          // Store schema in the source
          // Use the existing getter function - we need to ignore the teamId parameter
          // since the actual function has a different signature
          const source = getSourceById.value(sourceId);
          if (source) {
            const updatedSource = { ...source, schema };

            // Update in sources collection
            const index = state.data.value.sources.findIndex(s => s.id === sourceId);
            if (index >= 0) {
              const newSources = [...state.data.value.sources];
              newSources[index] = updatedSource as Source;
              state.data.value.sources = newSources;
            }

            // Also update current source details if loaded
            if (state.data.value.currentSourceDetails?.id === sourceId) {
              state.data.value.currentSourceDetails = updatedSource as Source;
            }
          }
        },
        showToast: false,
      });
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
    loadingStates: state.loadingStates,
    validatedConnections,
    currentSourceDetails,
    getCurrentSourceTableName,
    hasValidCurrentSource,
    visibleSources,
    isHydrated,

    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
    isLoadingSource: (sourceId: number) =>
      state.isLoadingOperation(`getSource-${sourceId}`),
    isLoadingTeamSources: (teamId: number) =>
      state.isLoadingOperation(`loadTeamSources-${teamId}`),
    isLoadingSourceDetails: (id: number) => {
      if (!id) return false;
      return state.isLoadingOperation(`loadSourceDetails-${id}`);
    },

    // Getters
    getSourceById,
    getTeamSourceById,
    getSourceStatsById: (id: number) => getSourceStatsById.value(id),
    isConnectionValidated,

    // Actions
    loadSources,
    loadTeamSources,
    loadTeamSourceQueries,
    createTeamSourceQuery,
    createSource,
    updateSource,
    deleteSource,
    getSourcesNotInTeam,
    getSourceStats,
    getTeamSourceStats,
    getSource,
    validateSourceConnection,
    invalidateSourceCache,
    loadSourceDetails,
    clearCurrentSourceDetails,
    hydrate,
    loadAllSourcesForAdmin,
    getSourceSchema,
  };
});
