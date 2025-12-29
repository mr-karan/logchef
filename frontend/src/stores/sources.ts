import { defineStore } from "pinia";
import { computed, ref, reactive, watch } from "vue";
import { useTeamsStore } from "./teams";
import { useContextStore } from "./context";
import { sourcesApi } from "@/api/sources";
import type {
  Source,
  CreateSourcePayload,
  SourceStats,
  CreateTeamQueryRequest,
} from "@/api/sources";
import type { APIErrorResponse } from "@/api/types";
import { useBaseStore } from "./base";

interface SourcesState {
  sources: Source[];
  teamSources: Source[];
  sourceQueries: Record<string, any>;
  sourceStats: Record<string, SourceStats>;
  currentSourceDetails: Source | null;
  // Loading states
  isLoadingTeamSources: boolean;
  isLoadingSourceDetails: boolean;
  // Error states
  teamSourcesError: string | null;
  sourceDetailsError: string | null;
}

export const useSourcesStore = defineStore("sources", () => {
  const teamsStore = useTeamsStore();
  const contextStore = useContextStore();

  const state = useBaseStore<SourcesState>({
    sources: [],
    teamSources: [],
    sourceQueries: {},
    sourceStats: {},
    currentSourceDetails: null,
    isLoadingTeamSources: false,
    isLoadingSourceDetails: false,
    teamSourcesError: null,
    sourceDetailsError: null
  });

  // Computed properties
  const sources = computed(() => state.data.value.sources);
  const teamSources = computed(() => state.data.value.teamSources);
  const sourceQueries = computed(() => state.data.value.sourceQueries);
  const sourceStats = computed(() => state.data.value.sourceStats);
  const currentSourceDetails = computed(() => state.data.value.currentSourceDetails);
  
  // Loading state computed properties
  const isLoadingTeamSources = computed(() => state.data.value.isLoadingTeamSources);
  const isLoadingSourceDetails = computed(() => state.data.value.isLoadingSourceDetails);
  const teamSourcesError = computed(() => state.data.value.teamSourcesError);
  const sourceDetailsError = computed(() => state.data.value.sourceDetailsError);

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

  // Reactive watchers for centralized team/source loading
  // Track last processed IDs to prevent duplicate loading
  let lastProcessedTeamId: number | null = null;
  let lastProcessedSourceId: number | null = null;

  // Watch team changes and auto-load sources
  watch(
    () => contextStore.teamId,
    async (newTeamId, oldTeamId) => {
      // Prevent duplicate processing
      if (newTeamId === lastProcessedTeamId) {
        return;
      }
      
      console.log(`SourcesStore: Team changed from ${oldTeamId} to ${newTeamId}`);
      lastProcessedTeamId = newTeamId;
      
      // Clear previous state
      state.data.value.teamSourcesError = null;
      state.data.value.teamSources = [];
      state.data.value.currentSourceDetails = null;

      if (!newTeamId) {
        console.log('SourcesStore: No team selected, skipping source loading');
        return;
      }

      try {
        console.log(`SourcesStore: Loading sources for team ${newTeamId}`);
        state.data.value.isLoadingTeamSources = true;
        await loadTeamSources(newTeamId);
        
        if (contextStore.teamId !== newTeamId) {
          console.log(`SourcesStore: Team changed during load (now ${contextStore.teamId}), discarding results for ${newTeamId}`);
          return;
        }
        
        console.log(`SourcesStore: Successfully loaded ${teamSources.value.length} sources for team ${newTeamId}`);
        
        if (teamSources.value.length > 0) {
          const currentSourceId = contextStore.sourceId;
          const currentSourceValid = currentSourceId && teamSources.value.some(s => s.id === currentSourceId);
          
          if (!currentSourceValid) {
            const cachedSourceId = contextStore.getStoredSourceForTeam(newTeamId);
            const validCachedSource = cachedSourceId && teamSources.value.some(s => s.id === cachedSourceId);
            const targetSourceId = validCachedSource ? cachedSourceId : teamSources.value[0].id;
            
            console.log(`SourcesStore: Selecting source ${targetSourceId} for team ${newTeamId}`);
            contextStore.selectSource(targetSourceId);
          }
        }
      } catch (error) {
        const errorMessage = error instanceof Error ? error.message : 'Failed to load sources';
        state.data.value.teamSourcesError = errorMessage;
        console.error(`SourcesStore: Error loading sources for team ${newTeamId}:`, error);
      } finally {
        state.data.value.isLoadingTeamSources = false;
      }
    },
    { immediate: true }
  );

  watch(
    () => [contextStore.sourceId, state.data.value.isLoadingTeamSources] as const,
    async ([newSourceId, isLoadingTeamSources], oldValue) => {
      const [oldSourceId, wasLoadingTeamSources] = oldValue ?? [null, false];
      
      const teamSourcesJustFinishedLoading = wasLoadingTeamSources && !isLoadingTeamSources;
      if (teamSourcesJustFinishedLoading && newSourceId) {
        console.log(`SourcesStore: Team sources loaded, processing source ${newSourceId}`);
        lastProcessedSourceId = null;
      }
      
      if (newSourceId === lastProcessedSourceId) {
        return;
      }
      
      console.log(`SourcesStore: Source changed from ${oldSourceId} to ${newSourceId}`);
      
      state.data.value.sourceDetailsError = null;
      state.data.value.currentSourceDetails = null;

      if (!newSourceId) {
        lastProcessedSourceId = null;
        console.log('SourcesStore: No source selected, skipping details loading');
        return;
      }

      const currentTeamId = contextStore.teamId;
      if (!currentTeamId) {
        console.log(`SourcesStore: No team selected, deferring source ${newSourceId} details loading`);
        return;
      }

      if (isLoadingTeamSources) {
        console.log(`SourcesStore: Team sources still loading, will process source ${newSourceId} when ready`);
        return;
      }

      lastProcessedSourceId = newSourceId;

      const sourceExistsInTeam = teamSources.value.some(s => s.id === newSourceId);
      
      if (teamSources.value.length === 0) {
        console.log(`SourcesStore: Team ${currentTeamId} has no sources, cannot load source ${newSourceId}`);
        state.data.value.sourceDetailsError = 'Team has no sources configured';
        return;
      }
      
      if (!sourceExistsInTeam) {
        console.log(`SourcesStore: Source ${newSourceId} not in team ${currentTeamId}'s sources, selecting first available`);
        contextStore.selectSource(teamSources.value[0].id);
        return;
      }

      try {
        console.log(`SourcesStore: Loading details for source ${newSourceId}`);
        state.data.value.isLoadingSourceDetails = true;
        await loadSourceDetails(newSourceId);
        
        if (contextStore.sourceId !== newSourceId || contextStore.teamId !== currentTeamId) {
          console.log(`SourcesStore: Context changed during load, discarding details for source ${newSourceId}`);
          return;
        }
        
        console.log(`SourcesStore: Successfully loaded details for source ${newSourceId}`);
      } catch (error) {
        const errorMessage = error instanceof Error ? error.message : 'Failed to load source details';
        state.data.value.sourceDetailsError = errorMessage;
        console.error(`SourcesStore: Error loading details for source ${newSourceId}:`, error);
      } finally {
        state.data.value.isLoadingSourceDetails = false;
      }
    },
    { immediate: true }
  );

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

  async function loadTeamSources(teamId: number, _version?: number) {
    return await state.withLoading(`loadTeamSources-${teamId}`, async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.listTeamSources(teamId),
        operationKey: `loadTeamSources-${teamId}`,
        onSuccess: async (data) => {
          state.data.value.teamSources = data ?? [];
          console.log(`loadTeamSources: Loaded ${state.data.value.teamSources.length} sources for team ${teamId}`);
          if (state.data.value.teamSources.length === 0) {
            console.log(`loadTeamSources: Team ${teamId} genuinely has no sources configured`);
          } else {
            console.log(`loadTeamSources: Team ${teamId} sources:`, state.data.value.teamSources.map(s => `${s.id}:${s.name}`));
          }
        },
        defaultData: [],
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

  async function loadSourceDetails(sourceId: number, _version?: number) {
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
        onSuccess: async (data: any) => {
          if (!data) return;

          state.data.value.currentSourceDetails = data as Source;
          console.log(`Loaded source details for source ${sourceId}`);
          
          // Update the source in teamSources as well (like the existing getSource does)
          const index = state.data.value.teamSources.findIndex((s) => s.id === sourceId);
          if (index >= 0) {
            const newTeamSources = [...state.data.value.teamSources];
            newTeamSources[index] = data as Source;
            state.data.value.teamSources = newTeamSources;
          }
        },
        onError: (_error) => {
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

  function clearTeamSources() {
    state.data.value.teamSources = [];
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
    
    // Centralized loading states
    isLoadingTeamSources,
    isLoadingSourceDetails,
    teamSourcesError,
    sourceDetailsError,

    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
    isLoadingSource: (sourceId: number) =>
      state.isLoadingOperation(`getSource-${sourceId}`),
    isLoadingTeamSourcesForTeam: (teamId: number) =>
      state.isLoadingOperation(`loadTeamSources-${teamId}`),
    isLoadingSourceDetailsForId: (id: number) => {
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
    clearTeamSources,
    hydrate,
    loadAllSourcesForAdmin,
    getSourceSchema,
  };
});
