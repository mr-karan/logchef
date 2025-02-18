import { defineStore } from "pinia";
import { ref } from "vue";
import { sourcesApi, type Source } from "@/api/sources";
import { isErrorResponse } from "@/api/types";

export const useSourcesStore = defineStore("sources", () => {
  const sources = ref<Source[]>([]);
  const isLoading = ref(false);
  const error = ref<string | null>(null);

  async function loadSources() {
    if (isLoading.value) return;

    try {
      isLoading.value = true;
      error.value = null;

      const response = await sourcesApi.listSources();

      if (isErrorResponse(response)) {
        error.value = response.data.error;
        return;
      }

      sources.value = response.data.sources;
    } catch (err) {
      error.value = "Failed to load sources";
      console.error("Error loading sources:", err);
    } finally {
      isLoading.value = false;
    }
  }

  // Get sources not in a specific team
  function getSourcesNotInTeam(teamSourceIds: string[]) {
    return sources.value.filter((source) => !teamSourceIds.includes(source.id));
  }

  return {
    sources,
    isLoading,
    error,
    loadSources,
    getSourcesNotInTeam,
  };
});
