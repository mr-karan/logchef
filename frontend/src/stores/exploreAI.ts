import { defineStore } from "pinia";
import { computed, ref } from "vue";
import { exploreApi } from "@/api/explore";
import type { AIGenerateSQLRequest, AIGenerateSQLResponse } from "@/api/explore";
import { useTeamsStore } from "@/stores/teams";
import { useSourcesStore } from "@/stores/sources";
import { useContextStore } from "@/stores/context";

interface AIState {
  isGenerating: boolean;
  error: string | null;
  generatedSql: string | null;
}

export const useExploreAIStore = defineStore("exploreAI", () => {
  const contextStore = useContextStore();

  const state = ref<AIState>({
    isGenerating: false,
    error: null,
    generatedSql: null,
  });

  const isGeneratingAISQL = computed(() => state.value.isGenerating);
  const aiSqlError = computed(() => state.value.error);
  const generatedAiSql = computed(() => state.value.generatedSql);

  function clearState() {
    state.value.isGenerating = false;
    state.value.error = null;
    state.value.generatedSql = null;
  }

  async function generateAiSql(naturalLanguageQuery: string, currentQuery?: string): Promise<{
    success: boolean;
    data?: AIGenerateSQLResponse;
    error?: { message: string; status?: string; error_type?: string };
  }> {
    state.value.isGenerating = true;
    state.value.error = null;
    state.value.generatedSql = null;

    try {
      const teamsStore = useTeamsStore();
      const currentTeamId = teamsStore.currentTeamId;
      if (!currentTeamId) {
        throw new Error("No team selected");
      }

      const sourcesStore = useSourcesStore();
      const sourceDetails = sourcesStore.currentSourceDetails;
      if (!sourceDetails) {
        throw new Error("Source details not available");
      }

      const sourceId = contextStore.sourceId;
      if (!sourceId) {
        throw new Error("No source selected");
      }

      const request: AIGenerateSQLRequest = {
        natural_language_query: naturalLanguageQuery,
        current_query: currentQuery
      };

      const response = await exploreApi.generateAISQL(sourceId, request, currentTeamId);

      if (response.data) {
        state.value.generatedSql = response.data.sql_query || '';
        return { success: true, data: response.data };
      } else {
        const errorMsg = 'Failed to generate SQL';
        state.value.error = errorMsg;
        return { success: false, error: { message: errorMsg } };
      }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error occurred';
      state.value.error = errorMessage;
      return {
        success: false,
        error: { message: errorMessage, status: 'error', error_type: 'AIGenerationError' }
      };
    } finally {
      state.value.isGenerating = false;
    }
  }

  return {
    isGeneratingAISQL,
    aiSqlError,
    generatedAiSql,

    clearState,
    generateAiSql,
  };
});
