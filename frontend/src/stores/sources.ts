import { defineStore } from "pinia";
import {
  sourcesApi,
  type Source,
  type CreateSourcePayload,
} from "@/api/sources";
import { useBaseStore, handleApiCall } from "./base";

export const useSourcesStore = defineStore("sources", () => {
  const {
    data: sources,
    isLoading,
    error,
    withLoading,
  } = useBaseStore<Source[]>([]);

  async function loadSources() {
    return withLoading(async () => {
      const { success, data } = await handleApiCall({
        apiCall: () => sourcesApi.listSources(),
        successMessage: undefined, // Don't show toast for loading
      });

      if (success && data) {
        // Initialize with empty array if sources is null
        sources.value = data.sources || [];
      }
    });
  }

  async function createSource(payload: CreateSourcePayload) {
    const { success } = await handleApiCall({
      apiCall: () => sourcesApi.createSource(payload),
      successMessage: "Source created successfully",
      onSuccess: (data) => {
        // Add to sources list if we have it loaded
        if (sources.value) {
          sources.value.push(data.source);
        }
      },
    });

    return success;
  }

  async function deleteSource(id: string) {
    const { success } = await handleApiCall({
      apiCall: () => sourcesApi.deleteSource(id),
      successMessage: "Source deleted successfully",
      onSuccess: () => {
        // Remove from sources list if we have it loaded
        if (sources.value) {
          sources.value = sources.value.filter((s) => s.id !== id);
        }
      },
    });

    return success;
  }

  // Get sources not in a specific team
  function getSourcesNotInTeam(teamSourceIds: string[]) {
    return sources.value
      ? sources.value.filter((source) => !teamSourceIds.includes(source.id))
      : [];
  }

  return {
    sources,
    isLoading,
    error,
    loadSources,
    createSource,
    deleteSource,
    getSourcesNotInTeam,
  };
});
