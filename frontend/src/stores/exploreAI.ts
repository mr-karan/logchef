import { defineStore } from "pinia";
import { computed, ref } from "vue";
import { exploreApi } from "@/api/explore";
import type { AIGenerateSQLRequest, AIGenerateSQLResponse } from "@/api/explore";
import { useTeamsStore } from "@/stores/teams";
import { useSourcesStore } from "@/stores/sources";
import { useContextStore } from "@/stores/context";

export type AIQueryMode = "logchefql" | "native";

// A generated query and the editor mode it was written for.
export interface GeneratedAIQuery {
  query: string;
  mode: AIQueryMode;
}

interface AIState {
  isGenerating: boolean;
  error: string | null;
  generated: GeneratedAIQuery | null;
}

// The server names the language it wrote; older servers name none, so fall
// back to the mode that was asked for.
function generatedMode(language: string | undefined, requested: AIQueryMode | undefined): AIQueryMode {
  if (language === "logchefql") return "logchefql";
  if (language) return "native";
  return requested ?? "native";
}

export const useExploreAIStore = defineStore("exploreAI", () => {
  const contextStore = useContextStore();

  const state = ref<AIState>({
    isGenerating: false,
    error: null,
    generated: null,
  });

  const isGeneratingAISQL = computed(() => state.value.isGenerating);
  const aiSqlError = computed(() => state.value.error);
  const generatedAiQuery = computed(() => state.value.generated);

  function clearState() {
    state.value.isGenerating = false;
    state.value.error = null;
    state.value.generated = null;
  }

  async function generateAiSql(
    naturalLanguageQuery: string,
    currentQuery?: string,
    mode?: AIQueryMode
  ): Promise<{
    success: boolean;
    data?: AIGenerateSQLResponse;
    error?: { message: string; status?: string; error_type?: string };
  }> {
    state.value.isGenerating = true;
    state.value.error = null;
    state.value.generated = null;

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
        current_query: currentQuery,
        mode
      };

      const response = await exploreApi.generateAISQL(sourceId, request, currentTeamId);

      if (response.data) {
        state.value.generated = { query: response.data.sql_query || '', mode: generatedMode(response.data.language, mode) };
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
    generatedAiQuery,

    clearState,
    generateAiSql,
  };
});
