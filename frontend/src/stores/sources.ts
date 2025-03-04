import { defineStore } from "pinia";
import {
  sourcesApi,
  type Source,
  type CreateSourcePayload,
  type SourceWithTeams,
  type TeamGroupedQuery,
  type CreateTeamQueryRequest,
} from "@/api/sources";
import { useBaseStore, handleApiCall } from "./base";
import { ref, computed } from "vue";
import type { SavedTeamQuery } from "@/api/types";
import { useTeamsStore } from "./teams";

export const useSourcesStore = defineStore("sources", () => {
  const {
    data: sources,
    isLoading,
    error,
    withLoading,
  } = useBaseStore<Source[]>([]);

  // New state for sources with team info
  const sourcesWithTeams = ref<SourceWithTeams[]>([]);
  const sourceQueriesMap = ref<Record<string, TeamGroupedQuery[]>>({});
  const sourceQueriesLoading = ref<Record<string, boolean>>({});

  // Get the team store
  const teamsStore = useTeamsStore();

  // Computed property for sources with teams info
  const deduplicatedSources = computed(() => {
    return sourcesWithTeams.value;
  });

  // Get sources for a specific team
  const teamSources = computed(() => {
    if (!teamsStore.currentTeamId) return [];

    return sourcesWithTeams.value.filter((source) =>
      source.teams.some((team) => team.id === teamsStore.currentTeamId)
    );
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

  async function loadUserSources() {
    return withLoading(async () => {
      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.listUserSources(),
        successMessage: undefined, // Don't show toast for loading
      });

      if (success && data) {
        // Transform the response to match the expected structure
        sourcesWithTeams.value =
          data.map((item) => ({
            ...item.source,
            teams: item.teams,
          })) || [];
      }
    });
  }

  async function loadSourceQueries(sourceId: string, refresh: boolean = false) {
    if (sourceQueriesLoading.value[sourceId]) {
      return;
    }

    // Return cached queries if available and not refreshing
    if (sourceQueriesMap.value[sourceId] && !refresh) {
      return sourceQueriesMap.value[sourceId];
    }

    // Set loading state for this source
    sourceQueriesLoading.value = {
      ...sourceQueriesLoading.value,
      [sourceId]: true,
    };

    try {
      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.listSourceQueries(sourceId, true),
        successMessage: undefined, // Don't show toast for loading
      });

      if (success && data) {
        // Update queries map
        sourceQueriesMap.value = {
          ...sourceQueriesMap.value,
          [sourceId]: data,
        };
        return data;
      }
      return [];
    } catch (error) {
      console.error("Error loading source queries:", error);
      return [];
    } finally {
      // Clear loading state
      sourceQueriesLoading.value = {
        ...sourceQueriesLoading.value,
        [sourceId]: false,
      };
    }
  }

  async function createSourceQuery(
    sourceId: string,
    query: CreateTeamQueryRequest
  ) {
    const { success, data } = await handleApiCall({
      apiCall: () => sourcesApi.createSourceQuery(sourceId, query),
      successMessage: "Query saved successfully",
    });

    // If successful, update the cache
    if (success && data) {
      const teamId = query.team_id;
      const existingQueries = sourceQueriesMap.value[sourceId] || [];
      const teamGroup = existingQueries.find((g) => g.team_id === teamId);

      if (teamGroup) {
        // Update existing team group
        teamGroup.queries = [data, ...teamGroup.queries];
      } else {
        // Create new team group
        const team = teamsStore.teams.find((t) => t.id === teamId);
        existingQueries.push({
          team_id: teamId,
          team_name: team?.name || "Unknown Team",
          queries: [data],
        });
      }

      // Update the cache
      sourceQueriesMap.value = {
        ...sourceQueriesMap.value,
        [sourceId]: existingQueries,
      };
    }

    return { success, data };
  }

  async function createSource(payload: CreateSourcePayload) {
    const { success, data } = await handleApiCall({
      apiCall: () => sourcesApi.createSource(payload),
      successMessage: "Source created successfully",
      onSuccess: (sourceData) => {
        // Add to sources list if we have it loaded
        if (sources.value && sourceData) {
          sources.value.push(sourceData);
        }
      },
    });

    return success;
  }

  async function deleteSource(id: number) {
    const { success } = await handleApiCall({
      apiCall: () => sourcesApi.deleteSource(id),
      successMessage: "Source deleted successfully",
      onSuccess: () => {
        // Remove from sources list if we have it loaded
        if (sources.value) {
          sources.value = sources.value.filter((s) => s.id !== id);
        }

        // Also remove from deduplicated sources
        if (sourcesWithTeams.value) {
          sourcesWithTeams.value = sourcesWithTeams.value.filter(
            (s) => s.id !== id
          );
        }

        // Clean up queries cache
        if (sourceQueriesMap.value[id]) {
          const newMap = { ...sourceQueriesMap.value };
          delete newMap[id];
          sourceQueriesMap.value = newMap;
        }
      },
    });

    return success;
  }

  // Get sources not in a specific team
  function getSourcesNotInTeam(teamSourceIds: number[]) {
    return sources.value
      ? sources.value.filter((source) => !teamSourceIds.includes(source.id))
      : [];
  }

  // Get teams for a specific source
  function getTeamsForSource(sourceId: number) {
    const source = sourcesWithTeams.value.find((s) => s.id === sourceId);
    return source ? source.teams : [];
  }

  // Source stats
  const sourceStatsLoading = ref(false);
  const sourceStatsError = ref<string | null>(null);

  async function getSourceStats(sourceId: number) {
    sourceStatsLoading.value = true;
    sourceStatsError.value = null;
    
    try {
      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.getSourceStats(sourceId),
        successMessage: undefined, // Don't show toast for loading
      });

      sourceStatsLoading.value = false;
      return success ? data : null;
    } catch (error: any) {
      sourceStatsLoading.value = false;
      sourceStatsError.value = error.message || "Failed to fetch source statistics";
      return null;
    }
  }

  return {
    sources,
    isLoading,
    error,
    sourcesWithTeams,
    sourceQueriesMap,
    sourceQueriesLoading,
    sourceStatsLoading,
    sourceStatsError,
    deduplicatedSources,
    teamSources,
    loadSources,
    loadUserSources,
    loadSourceQueries,
    createSource,
    deleteSource,
    createSourceQuery,
    getSourceStats,
    getSourcesNotInTeam,
    getTeamsForSource,
  };
});
