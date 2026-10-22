import { defineStore } from "pinia";
import { computed, ref, reactive, watch } from "vue";
import { pickSource, useContextStore } from "./context";
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
import { useBaseStore } from "./base";
import { i18n } from "@/i18n";
import { isAbortError } from "@/api/error-handler";

type SourceActivityRequestResult = {
  success: boolean;
  data?: SourceActivity | null;
  error?: APIErrorResponse;
};

interface SourcesState {
  sources: Source[];
  sourceQueries: Record<string, any>;
  sourceInspections: Record<string, SourceInspection>;
  sourceActivities: Record<string, SourceActivity>;
  sourceActivityErrors: Record<string, string>;
  // The context's team sources and source details only. Keyed, so a value
  // never shows under another team or source while the context switches.
  sourcesByTeam: Record<number, Source[]>;
  sourceDetails: Record<number, Record<number, Source>>;
}

export const useSourcesStore = defineStore("sources", () => {
  const contextStore = useContextStore();

  const state = useBaseStore<SourcesState>({
    sources: [],
    sourceQueries: {},
    sourceInspections: {},
    sourceActivities: {},
    sourceActivityErrors: {},
    sourcesByTeam: {},
    sourceDetails: {},
  });

  // Computed properties
  const sources = computed(() => state.data.value.sources);
  const sourceInspections = computed(() => state.data.value.sourceInspections);
  const sourceActivities = computed(() => state.data.value.sourceActivities);
  const sourceActivityErrors = computed(() => state.data.value.sourceActivityErrors);

  const detailsKey = (teamId: number, sourceId: number) => `${teamId}-${sourceId}`;
  const teamSources = computed(() =>
    contextStore.teamId ? state.data.value.sourcesByTeam[contextStore.teamId] ?? [] : []
  );
  const currentSourceDetails = computed(() => {
    const { teamId, sourceId } = contextStore;
    return teamId && sourceId ? state.data.value.sourceDetails[teamId]?.[sourceId] ?? null : null;
  });

  const isLoadingTeamSources = computed(() =>
    !!contextStore.teamId && state.isLoadingOperation(`loadTeamSources-${contextStore.teamId}`)
  );
  const isLoadingSourceDetailsForId = (sourceId: number) =>
    !!contextStore.teamId && !!sourceId &&
    state.isLoadingOperation(`loadSourceDetails-${detailsKey(contextStore.teamId, sourceId)}`);
  const isLoadingSourceDetails = computed(() =>
    !!contextStore.sourceId && isLoadingSourceDetailsForId(contextStore.sourceId)
  );

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
  // Bumped whenever the context changes and on invalidation. A request from
  // an older generation neither writes the cache nor is joined.
  let generation = 0;

  function invalidateRequests() {
    generation++;
  }
  const teamSourcesRequests = new Map<string, { teamId: number; controller: AbortController; promise: Promise<Source[]> }>();
  const sourceDetailsRequests = new Map<string, Promise<Source>>();

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
    const details = currentSourceDetails.value;
    // A source is considered valid for querying if the backend check marked it as connected.
    // We still need the details object to exist.
    return !!(details && details.is_connected === true);
  });

  // Get formatted table name from current source details
  const getCurrentSourceTableName = computed(() => {
    const details = currentSourceDetails.value;
    const chConn = asClickHouseConnection(details?.connection);
    if (chConn?.database && chConn?.table_name) {
      return `${chConn.database}.${chConn.table_name}`;
    }
    return null; // Or a default/placeholder
  });

  // Pages other than Explorer select a team and source through the context
  // only. Keep a valid source selected and its details loaded for them.
  async function loadContext(refresh = false) {
    const { teamId } = contextStore;
    if (!teamId) return;
    const requestedIn = generation;
    const list = await loadTeamSources(teamId, refresh);
    if (requestedIn !== generation || contextStore.teamId !== teamId) return;
    const sourceId = pickSource(teamId, list, contextStore.sourceId);
    if (sourceId !== contextStore.sourceId) {
      if (sourceId) contextStore.selectSource(sourceId);
      else contextStore.clearSource();
      return;
    }
    if (sourceId) {
      contextStore.rememberCurrentSource();
      await loadSourceDetails(teamId, sourceId, refresh);
    }
  }

  // Synchronous, so a request made right after a context change, before the
  // watcher below runs, already belongs to the new generation.
  watch(() => [contextStore.teamId, contextStore.sourceId], ([teamId], [leftTeamId]) => {
    invalidateRequests();
    if (teamId !== leftTeamId) {
      for (const request of teamSourcesRequests.values()) {
        if (request.teamId === leftTeamId) request.controller.abort();
      }
    }
  }, { flush: "sync" });

  // A left team or source drops its cache, so a return fetches it again.
  watch(
    () => [contextStore.teamId, contextStore.sourceId] as const,
    ([teamId, sourceId], left) => {
      const [leftTeamId, leftSourceId] = left ?? [null, null];
      const cache = state.data.value;
      if (leftTeamId && leftTeamId !== teamId) {
        delete cache.sourcesByTeam[leftTeamId];
        delete cache.sourceDetails[leftTeamId];
      } else if (leftTeamId && leftSourceId && leftSourceId !== sourceId) {
        delete cache.sourceDetails[leftTeamId]?.[leftSourceId];
      }
      loadContext().catch((error) => {
        if (!isAbortError(error)) console.error("error loading the context's sources", error);
      });
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

  /**
   * The team's sources: cached for the current team, else fetched, joining a
   * request in flight. Only the current team's are cached. Rejects when the
   * request fails.
   */
  function loadTeamSources(teamId: number, refresh = false): Promise<Source[]> {
    const current = teamId === contextStore.teamId;
    const key = `${generation}-${teamId}`;
    const pending = teamSourcesRequests.get(key);
    if (pending) return pending.promise;
    const cached = current && !refresh ? state.data.value.sourcesByTeam[teamId] : undefined;
    if (cached) return Promise.resolve(cached);

    const requestedIn = generation;
    const controller = new AbortController();
    const request = state
      .callApi<Source[]>({
        apiCall: () => sourcesApi.listTeamSources(teamId, { signal: controller.signal }),
        operationKey: `loadTeamSources-${teamId}`,
        showToast: false,
      })
      .then((result) => {
        if (!result.success) throw result.error;
        const list = result.data ?? [];
        if (current && requestedIn === generation) state.data.value.sourcesByTeam[teamId] = list;
        return list;
      })
      .finally(() => teamSourcesRequests.delete(key));
    teamSourcesRequests.set(key, { teamId, controller, promise: request });
    return request;
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
      invalidateTeamSources();
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
      invalidateTeamSources();
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

  /**
   * A source's details: cached for the current source, else fetched, joining
   * a request in flight. Only the current source's are
   * cached. Rejects when the request fails.
   */
  function loadSourceDetails(teamId: number, sourceId: number, refresh = false): Promise<Source> {
    const current = teamId === contextStore.teamId && sourceId === contextStore.sourceId;
    const key = `${generation}-${detailsKey(teamId, sourceId)}`;
    const pending = sourceDetailsRequests.get(key);
    if (pending) return pending;
    const cached = current && !refresh ? state.data.value.sourceDetails[teamId]?.[sourceId] : undefined;
    if (cached) return Promise.resolve(cached);

    const requestedIn = generation;
    const request = state
      .callApi<Source>({
        apiCall: () => sourcesApi.getTeamSource(teamId, sourceId),
        operationKey: `loadSourceDetails-${detailsKey(teamId, sourceId)}`,
        showToast: false,
      })
      .then((result) => {
        if (!result.success || !result.data) throw result.error ?? new Error("Source not found.");
        if (current && requestedIn === generation) (state.data.value.sourceDetails[teamId] ??= {})[sourceId] = result.data;
        return result.data;
      })
      .finally(() => sourceDetailsRequests.delete(key));
    sourceDetailsRequests.set(key, request);
    return request;
  }

  /** Caches what an Explorer navigation resolved, just before it opens that team and source. */
  function holdResolved(teamId: number, list: Source[], details: Source | null, resolvedIn: number): boolean {
    if (resolvedIn !== generation) return false;
    state.data.value.sourcesByTeam[teamId] = list;
    if (details) (state.data.value.sourceDetails[teamId] ??= {})[details.id] = details;
    return true;
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

  /** Refetches the context's sources and details after a source or team change, keeping the old ones until then. */
  function invalidateTeamSources() {
    invalidateRequests();
    loadContext(true).catch((error) => {
      if (!isAbortError(error)) console.error("error reloading the context's sources", error);
    });
  }

  function invalidateSourceCache(sourceId: number) {
    state.data.value.sources = state.data.value.sources.filter(
      s => s.id !== sourceId
    );
    invalidateTeamSources();

    // Also clear any stats for this source
    delete state.data.value.sourceInspections[sourceId.toString()];
    delete state.data.value.sourceActivities[sourceId.toString()];
    delete state.data.value.sourceActivityErrors[sourceId.toString()];
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

          for (const list of Object.values(state.data.value.sourcesByTeam)) {
            const index = list.findIndex(source => source.id === id);
            if (index >= 0) list[index] = updatedSourceData;
          }
          for (const details of Object.values(state.data.value.sourceDetails)) {
            if (details[id]) details[id] = updatedSourceData;
          }
          delete state.data.value.sourceInspections[id.toString()];
          delete state.data.value.sourceActivities[id.toString()];
          delete state.data.value.sourceActivityErrors[id.toString()];
        }
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

    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
    isLoadingSourceDetailsForId,

    // Getters
    getSourceById,
    getTeamSourceById,
    getSourceInspectionById: (id: number) => getSourceInspectionById.value(id),
    getSourceActivityById: (id: number) => getSourceActivityById.value(id),
    getSourceActivityErrorById: (id: number) => getSourceActivityErrorById.value(id),
    isConnectionValidated,

    // Actions
    loadSources,
    loadTeamSources,
    createSource,
    updateSource,
    deleteSource,
    getSourcesNotInTeam,
    getSourceInspection,
    getSourceActivity,
    getTeamSourceInspection,
    validateSourceConnection,
    invalidateSourceCache,
    invalidateTeamSources,
    loadSourceDetails,
    holdResolved,
    getMetadataGeneration: () => generation,
    hydrate,
    loadAllSourcesForAdmin,
  };
});
