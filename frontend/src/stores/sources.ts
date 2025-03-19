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
}

export const useSourcesStore = defineStore("sources", () => {
  const teamsStore = useTeamsStore();

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
    return await state.callApi<Source[]>({
      apiCall: () => sourcesApi.listSources(),
      operationKey: "loadSources",
      onSuccess: (data) => {
        state.data.value.sources = data;
      },
      showToast: false,
    });
  }

  async function loadTeamSources(teamId: number) {
    return await state.callApi<Source[]>({
      apiCall: () => sourcesApi.listTeamSources(teamId),
      operationKey: `loadTeamSources-${teamId}`,
      onSuccess: (data) => {
        state.data.value.teamSources = data;
      },
      showToast: false,
    });
  }

  async function loadSourceQueries(sourceId: string | number) {
    const id = typeof sourceId === "string" ? parseInt(sourceId) : sourceId;

    try {
      state.isLoading.value = true;

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

      if (result && "data" in result && result.data) {
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
        error:
          "error" in result ? result.error : "Failed to load source queries",
      };
    } catch (error) {
      console.error("Error loading source queries:", error);
      return {
        success: false,
        error: getErrorMessage(error),
      };
    } finally {
      state.isLoading.value = false;
    }
  }

  async function loadTeamSourceQueries(teamId: number, sourceId: number) {
    const key = `${teamId}-${sourceId}`;

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
      await loadSourceQueries(id);
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
      await loadTeamSourceQueries(teamId, sourceId);
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
      await loadSources();
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
      await loadSources();

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

  async function getSourceStats(sourceId: number) {
    return await state.callApi<SourceStats>({
      apiCall: () => sourcesApi.getSourceStats(sourceId),
      onSuccess: (data) => {
        state.data.value.sourceStats = {
          ...state.data.value.sourceStats,
          [sourceId.toString()]: data,
        };
      },
      showToast: false,
    });
  }

  async function getTeamSourceStats(teamId: number, sourceId: number) {
    const key = `${teamId}-${sourceId}`;

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

  async function getSource(sourceId: number) {
    const currentTeamId = teamsStore.currentTeamId;

    if (!currentTeamId) {
      return { success: false, error: "No team selected" };
    }

    return await state.callApi<Source>({
      apiCall: () => sourcesApi.getTeamSource(currentTeamId, sourceId),
      operationKey: `getSource-${sourceId}`,
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
    return await state.callApi<{ message: string }>({
      apiCall: () => sourcesApi.validateSourceConnection(connectionInfo),
      showToast: true,
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

    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
    isLoadingSource: (sourceId: number) =>
      state.isLoadingOperation(`getSource-${sourceId}`),
    isLoadingTeamSources: (teamId: number) =>
      state.isLoadingOperation(`loadTeamSources-${teamId}`),

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
