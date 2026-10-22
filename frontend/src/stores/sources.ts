import { defineStore } from "pinia";
import { computed, ref, reactive, watch } from "vue";
import { useTeamsStore } from "./teams";
import { useContextStore } from "./context";
import { sourcesApi, asClickHouseConnection } from "@/api/sources";
import type {
  Source,
  CreateSourcePayload,
  UpdateSourcePayload,
  SourceInspection,
  SourceActivity,
  ValidateConnectionRequestInfo,
} from "@/api/sources";
import type { APIErrorResponse } from "@/api/types";
import { useApiQuery } from "@/composables/useApiQuery";
import { createLatestRequests, useBaseStore } from "./base";
import { i18n } from "@/i18n";

type SourceActivityRequestResult = {
  success: boolean;
  data?: SourceActivity | null;
  error?: APIErrorResponse;
};

interface SourcesState {
  sources: Source[];
  teamSources: Source[];
  sourceQueries: Record<string, any>;
  sourceInspections: Record<string, SourceInspection>;
  sourceActivities: Record<string, SourceActivity>;
  sourceActivityErrors: Record<string, string>;
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
    sourceInspections: {},
    sourceActivities: {},
    sourceActivityErrors: {},
    currentSourceDetails: null,
    isLoadingTeamSources: false,
    isLoadingSourceDetails: false,
    teamSourcesError: null,
    sourceDetailsError: null
  });

  // Computed properties
  const sources = computed(() => state.data.value.sources);
  const teamSources = computed(() => state.data.value.teamSources);
  const sourceInspections = computed(() => state.data.value.sourceInspections);
  const sourceActivities = computed(() => state.data.value.sourceActivities);
  const sourceActivityErrors = computed(() => state.data.value.sourceActivityErrors);
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
  const getSourceInspectionById = computed(() => (id: number) => sourceInspections.value[id.toString()]);
  const getSourceActivityById = computed(() => (id: number) => sourceActivities.value[id.toString()]);
  const getSourceActivityErrorById = computed(() => (id: number) => sourceActivityErrors.value[id.toString()] ?? null);

  // Track validated connections
  const validatedConnections = reactive(new Set<string>());
  const sourceActivityRequests = new Map<number, Promise<SourceActivityRequestResult>>();
  const { execute } = useApiQuery<Source[]>();
  const teamSourceLoads = createLatestRequests({
    request: (teamId: number) => String(teamId),
    slot: () => "currentTeam",
    load: fetchTeamSources,
  });

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
    const chConn = asClickHouseConnection(details?.connection);
    if (chConn?.database && chConn?.table_name) {
      return `${chConn.database}.${chConn.table_name}`;
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
    async (newTeamId, _oldTeamId, onCleanup) => {
      // Prevent duplicate processing
      if (newTeamId === lastProcessedTeamId) {
        return;
      }

      lastProcessedTeamId = newTeamId;
      let superseded = false;
      onCleanup(() => { superseded = true; });
      const isCurrent = () => !superseded && contextStore.teamId === newTeamId;
      
      // Clear previous state
      state.data.value.teamSourcesError = null;
      state.data.value.teamSources = [];
      state.data.value.currentSourceDetails = null;

      if (!newTeamId) {
        teamSourceLoads.cancelAll();
        state.data.value.isLoadingTeamSources = false;
        return;
      }

      state.data.value.isLoadingTeamSources = true;
      try {
        await loadCurrentTeamSources();

        if (!isCurrent()) {
          return;
        }

        if (teamSources.value.length > 0) {
          const currentSourceId = contextStore.sourceId;
          const currentSourceValid = currentSourceId && teamSources.value.some(s => s.id === currentSourceId);
          
          if (!currentSourceValid) {
            const cachedSourceId = contextStore.getStoredSourceForTeam(newTeamId);
            const validCachedSource = cachedSourceId && teamSources.value.some(s => s.id === cachedSourceId);
            const targetSourceId = validCachedSource ? cachedSourceId : teamSources.value[0].id;

            contextStore.selectSource(targetSourceId);
          }
        }
      } catch (error) {
        if (!isCurrent()) return;
        const errorMessage = error instanceof Error ? error.message : 'Failed to load sources';
        state.data.value.teamSourcesError = errorMessage;
        console.error(`SourcesStore: Error loading sources for team ${newTeamId}:`, error);
      } finally {
        if (isCurrent()) {
          state.data.value.isLoadingTeamSources = false;
        }
      }
    },
    { immediate: true }
  );

  watch(
    () => [contextStore.sourceId, state.data.value.isLoadingTeamSources] as const,
    async ([newSourceId, isLoadingTeamSources], oldValue) => {
      const [_oldSourceId, wasLoadingTeamSources] = oldValue ?? [null, false];

      const teamSourcesJustFinishedLoading = wasLoadingTeamSources && !isLoadingTeamSources;
      if (teamSourcesJustFinishedLoading && newSourceId) {
        lastProcessedSourceId = null;
      }

      if (newSourceId === lastProcessedSourceId) {
        return;
      }

      state.data.value.sourceDetailsError = null;
      state.data.value.currentSourceDetails = null;

      if (!newSourceId) {
        lastProcessedSourceId = null;
        return;
      }

      const currentTeamId = contextStore.teamId;
      if (!currentTeamId) {
        return;
      }

      if (isLoadingTeamSources) {
        return;
      }

      lastProcessedSourceId = newSourceId;

      const sourceExistsInTeam = teamSources.value.some(s => s.id === newSourceId);

      if (teamSources.value.length === 0) {
        state.data.value.sourceDetailsError = 'Team has no sources configured';
        return;
      }

      if (!sourceExistsInTeam) {
        contextStore.selectSource(teamSources.value[0].id);
        return;
      }

      contextStore.rememberCurrentSource();

      try {
        state.data.value.isLoadingSourceDetails = true;
        await loadSourceDetails(newSourceId);

        if (contextStore.sourceId !== newSourceId || contextStore.teamId !== currentTeamId) {
          return;
        }
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

  // Loads the current team's sources into teamSources. A newer load for the same team hands its result to earlier callers.
  async function loadCurrentTeamSources() {
    const teamId = contextStore.teamId;
    if (!teamId) return { success: false, data: null, error: null };
    return await teamSourceLoads.run([teamId]);
  }

  async function fetchTeamSources(signal: AbortSignal, teamId: number) {
    return await state.withLoading(`loadTeamSources-${teamId}`, async () => {
      state.error.value = null;
      const result = await execute(() => sourcesApi.listTeamSources(teamId, { signal }), { defaultData: [], showToast: false });
      if (signal.aborted || contextStore.teamId !== teamId) return result;
      if (result.success) state.data.value.teamSources = result.data ?? [];
      else state.error.value = result.error ?? null;
      return result;
    });
  }

  async function createSource(payload: CreateSourcePayload) {
    const result = await state.withLoading("createSource", async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.createSource(payload),
        successMessage: i18n.global.t("sources.sourceCreated"),
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
        successMessage: i18n.global.t("sources.sourceDeleted"),
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
  async function getSourceInspection(sourceId: number, refresh = false) {
    return await state.withLoading(`getSourceInspection-${sourceId}`, async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.getAdminSourceInspection(sourceId, refresh),
        operationKey: `getSourceInspection-${sourceId}`,
        onSuccess: (data: any) => {
          state.data.value.sourceInspections = {
            ...state.data.value.sourceInspections,
            [sourceId.toString()]: data as SourceInspection,
          };
        },
        showToast: false,
      });
    });
  }


  async function getSourceActivity(sourceId: number, refresh = false) {
    const existingRequest = sourceActivityRequests.get(sourceId);
    if (existingRequest) {
      return await existingRequest;
    }

    const key = sourceId.toString();
    const { [key]: _removed, ...remaining } = state.data.value.sourceActivityErrors;
    state.data.value.sourceActivityErrors = remaining;

    const request = state.withLoading(`getSourceActivity-${sourceId}`, async () =>
      state.callApi<SourceActivity>({
        apiCall: () => sourcesApi.getAdminSourceActivity(sourceId, refresh),
        operationKey: `getSourceActivity-${sourceId}`,
        showToast: false,
        onSuccess: (data) => {
          if (data) {
            state.data.value.sourceActivities = { ...state.data.value.sourceActivities, [key]: data };
          }
          const { [key]: _removed, ...remaining } = state.data.value.sourceActivityErrors;
          state.data.value.sourceActivityErrors = remaining;
        },
        onError: (error) => {
          state.data.value.sourceActivityErrors = {
            ...state.data.value.sourceActivityErrors,
            [key]: error.message ?? "Recent activity unavailable",
          };
        },
      })
    );
    sourceActivityRequests.set(sourceId, request);

    try {
      return await request;
    } finally {
      if (sourceActivityRequests.get(sourceId) === request) {
        sourceActivityRequests.delete(sourceId);
      }
    }
  }

  // Get team-scoped inspection data for a source
  async function getTeamSourceInspection(teamId: number, sourceId: number) {
    return await state.withLoading(`getTeamSourceInspection-${teamId}-${sourceId}`, async () => {
      return await state.callApi({
        apiCall: () => sourcesApi.getTeamSourceInspection(teamId, sourceId),
        operationKey: `getTeamSourceInspection-${teamId}-${sourceId}`,
        onSuccess: (data: any) => {
          state.data.value.sourceInspections = {
            ...state.data.value.sourceInspections,
            [sourceId.toString()]: data as SourceInspection,
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

          // Stale-request guard: only commit if this source is still the
          // one the user has selected. A fast source switch can cause an
          // older request to resolve after a newer one.
          if (contextStore.sourceId !== sourceId) return;

          state.data.value.currentSourceDetails = data as Source;

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

  async function validateSourceConnection(connectionInfo: ValidateConnectionRequestInfo) {
    const connectionKey = JSON.stringify(connectionInfo);

    return await state.withLoading("validateSourceConnection", async () => {
      return await state.callApi<{ message: string }>({
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
    delete state.data.value.sourceInspections[sourceId.toString()];
    delete state.data.value.sourceActivities[sourceId.toString()];
    delete state.data.value.sourceActivityErrors[sourceId.toString()];

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
  async function updateSource(id: number, payload: UpdateSourcePayload) {
    return await state.withLoading(`updateSource-${id}`, async () => {
      return await state.callApi<Source>({
        apiCall: () => sourcesApi.updateSource(id, payload),
        successMessage: i18n.global.t("sources.sourceUpdated"),
        operationKey: `updateSource-${id}`,
        onSuccess: (updatedSourceData) => {
          if (!updatedSourceData) return;

          // Update in local state
          const index = sources.value.findIndex(s => s.id === id);
          if (index >= 0) {
            state.data.value.sources = [
              ...sources.value.slice(0, index),
              updatedSourceData,
              ...sources.value.slice(index + 1)
            ];
          }

          // Also update in team sources if present
          const teamIndex = teamSources.value.findIndex(s => s.id === id);
          if (teamIndex >= 0) {
            state.data.value.teamSources = [
              ...teamSources.value.slice(0, teamIndex),
              updatedSourceData,
              ...teamSources.value.slice(teamIndex + 1)
            ];
          }

          if (state.data.value.currentSourceDetails?.id === id) {
            state.data.value.currentSourceDetails = updatedSourceData;
          }
          delete state.data.value.sourceInspections[id.toString()];
          delete state.data.value.sourceActivities[id.toString()];
          delete state.data.value.sourceActivityErrors[id.toString()];
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
    isLoading: state.isLoading,
    error: state.error,
    teamSourcesMap,
    sourceInspections,
    sourceActivities,
    sourceActivityErrors,
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
    getSourceInspectionById: (id: number) => getSourceInspectionById.value(id),
    getSourceActivityById: (id: number) => getSourceActivityById.value(id),
    getSourceActivityErrorById: (id: number) => getSourceActivityErrorById.value(id),
    isConnectionValidated,

    // Actions
    loadSources,
    loadCurrentTeamSources,
    createSource,
    updateSource,
    deleteSource,
    getSourcesNotInTeam,
    getSourceInspection,
    getSourceActivity,
    getTeamSourceInspection,
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
