import { defineStore } from "pinia";
import {
  sourcesApi,
  type Source,
  type CreateSourcePayload,
  type SourceWithTeams,
  type TeamGroupedQuery,
  type CreateTeamQueryRequest,
  type SourceStats,
} from "@/api/sources";
import { useBaseStore, handleApiCall } from "./base";
import { ref, computed } from "vue";
import { useTeamsStore } from "./teams";
import type { SavedTeamQuery } from "@/api/types";

export const useSourcesStore = defineStore("sources", () => {
  const {
    data: sources,
    isLoading,
    error,
    withLoading,
  } = useBaseStore<Source[]>([]);

  const sourceQueries = ref<
    Record<string, TeamGroupedQuery[] | SavedTeamQuery[]>
  >({});
  const sourceQueriesLoading = ref<Record<string, boolean>>({});
  const teamSources = ref<Source[]>([]);
  const sourceStats = ref<Record<string, SourceStats>>({});

  // Get the team store
  const teamsStore = useTeamsStore();


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
    return withLoading(async () => {
      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.listSources(),
        successMessage: undefined, // Don't show toast for loading
      });

      if (success && data) {
        // Data is now directly an array of sources
        sources.value = data || [];
      }
    });
  }


  async function loadTeamSources(teamId: number) {
    return withLoading(async () => {
      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.listTeamSources(teamId),
        successMessage: undefined, // Don't show toast for loading
      });

      if (success && data) {
        teamSources.value = data || [];
      }
    });
  }

  async function loadSourceQueries(
    sourceId: string | number,
    refresh: boolean = false
  ) {
    const id = typeof sourceId === "string" ? parseInt(sourceId) : sourceId;
    const key = id.toString();

    // Return cached data if available and not refreshing
    if (sourceQueries.value[key] && !refresh) {
      return sourceQueries.value[key];
    }

    return withLoading(async () => {
      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.listSourceQueries(id),
        successMessage: undefined, // Don't show toast for loading
      });

      if (success && data) {
        sourceQueries.value[key] = data;
        return data;
      }

      return [];
    });
  }

  async function loadTeamSourceQueries(
    teamId: number,
    sourceId: number,
    refresh: boolean = false
  ) {
    const key = `${teamId}-${sourceId}`;

    // Return cached data if available and not refreshing
    if (sourceQueries.value[key] && !refresh) {
      return sourceQueries.value[key];
    }

    return withLoading(async () => {
      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.listTeamSourceQueries(teamId, sourceId),
        successMessage: undefined, // Don't show toast for loading
      });

      if (success && data) {
        sourceQueries.value[key] = data;
        return data;
      }

      return [];
    });
  }

  async function createSourceQuery(sourceId: string | number, query: any) {
    const id = typeof sourceId === "string" ? parseInt(sourceId) : sourceId;

    return withLoading(async () => {
      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.createSourceQuery(id, query),
        successMessage: "Query saved successfully",
      });

      if (success && data) {
        // Refresh queries for this source
        await loadSourceQueries(id, true);
        return data;
      }
    });
  }

  async function createTeamSourceQuery(
    teamId: number,
    sourceId: number,
    query: Omit<any, "team_id">
  ) {
    return withLoading(async () => {
      const { success, data } = await handleApiCall({
        apiCall: () =>
          sourcesApi.createTeamSourceQuery(teamId, sourceId, query),
        successMessage: "Query saved successfully",
      });

      if (success && data) {
        // Refresh queries for this team source
        await loadTeamSourceQueries(teamId, sourceId, true);
        return data;
      }
    });
  }

  async function createSource(payload: CreateSourcePayload) {
    return withLoading(async () => {
      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.createSource(payload),
        successMessage: "Source created successfully",
      });

      if (success && data) {
        // Refresh sources
        await loadSources();
        return data;
      }
    });
  }

  async function deleteSource(id: number) {
    return withLoading(async () => {
      const { success } = await handleApiCall({
        apiCall: () => sourcesApi.deleteSource(id),
        successMessage: "Source deleted successfully",
      });

      if (success) {
        // Refresh sources
        await loadSources();
        return true;
      }

      return false;
    });
  }

  // Get sources not in a specific team
  function getSourcesNotInTeam(teamSourceIds: (string | number)[]) {
    const ids = new Set(
      teamSourceIds.map((id) => (typeof id === "string" ? parseInt(id) : id))
    );

    return sources.value.filter((source) => !ids.has(source.id));
  }


  // Source stats
  const sourceStatsLoading = ref(false);
  const sourceStatsError = ref<string | null>(null);

  async function getSourceStats(sourceId: number) {
    const key = sourceId.toString();

    // Return cached data if available
    if (sourceStats.value[key]) {
      return sourceStats.value[key];
    }

    return withLoading(async () => {
      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.getSourceStats(sourceId),
        successMessage: undefined, // Don't show toast for loading
      });

      if (success && data) {
        sourceStats.value[key] = data;
        return data;
      }

      return null;
    });
  }

  // Get team source stats
  async function getTeamSourceStats(teamId: number, sourceId: number) {
    const key = `${teamId}-${sourceId}`;

    // Return cached data if available
    if (sourceStats.value[key]) {
      return sourceStats.value[key];
    }

    return withLoading(async () => {
      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.getTeamSourceStats(teamId, sourceId),
        successMessage: undefined, // Don't show toast for loading
      });

      if (success && data) {
        sourceStats.value[key] = data;
        return data;
      }

      return null;
    });
  }

  // Get a single source by ID
  async function getSource(sourceId: number) {
    try {
      const currentTeamId = teamsStore.currentTeamId;

      if (!currentTeamId) {
        console.error("No current team ID available");
        return null;
      }

      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.getTeamSource(currentTeamId, sourceId),
        successMessage: undefined, // Don't show toast for loading
      });

      if (success && data) {
        console.log(`Source data received from API for ID ${sourceId}:`, data);

        // Log columns and schema for debugging
        if (data.columns) {
          console.log(
            `Columns received from API for source ${sourceId}: ${data.columns.length}`,
            data.columns
          );
        } else {
          console.warn(`No columns received from API for source ${sourceId}`);
        }

        if (data.schema) {
          console.log(
            `Schema received from API for source ${sourceId} (length): ${data.schema.length}`
          );
        } else {
          console.warn(`No schema received from API for source ${sourceId}`);
        }

        return data;
      }
    } catch (error) {
      console.error(`Error fetching source ${sourceId}:`, error);
    }

    return null;
  }

  return {
    sources,
    isLoading,
    error,
    sourceQueries,
    sourceQueriesLoading,
    sourceStatsLoading,
    sourceStatsError,
    teamSources,
    teamSourcesMap,
    sourceStats,
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
