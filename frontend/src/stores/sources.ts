import { defineStore } from "pinia";
import { ref, computed } from "vue";
import { useTeamsStore } from "./teams";
import { sourcesApi } from "@/api/sources";
import type {
  Source,
  SourceWithTeams,
  TeamGroupedQuery,
  CreateSourcePayload,
  SourceStats,
  CreateTeamQueryRequest,
} from "@/api/sources";
import type { SavedTeamQuery } from "@/api/types";
import { useBaseStore } from "./base";
import { showErrorToast, showSuccessToast } from "@/api/error-handler";

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

  async function loadTeamSources(teamId: number, forceReload = false) {
    // Skip if we already have team sources and no force reload
    if (teamSources.value.length > 0 && !forceReload) {
      return { success: true, data: teamSources.value };
    }

    return await state.callApi<Source[]>({
      apiCall: () => sourcesApi.listTeamSources(teamId),
      onSuccess: (data) => {
        state.data.value.teamSources = data;
      },
      showToast: false,
    });
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

    return await state.callApi<TeamGroupedQuery[] | SavedTeamQuery[]>({
      apiCall: () => sourcesApi.listSourceQueries(id),
      onSuccess: (data) => {
        state.data.value.sourceQueries = {
          ...state.data.value.sourceQueries,
          [key]: data,
        };
      },
      showToast: false,
    });
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

  async function getSource(sourceId: number) {
    const currentTeamId = teamsStore.currentTeamId;

    if (!currentTeamId) {
      return { success: false, error: "No team selected" };
    }

    return await state.callApi<Source>({
      apiCall: () => sourcesApi.getTeamSource(currentTeamId, sourceId),
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
  };
});
