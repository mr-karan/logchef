import { defineStore } from "pinia";
import { useBaseStore } from "./base";
import { computed } from "vue";
import { apiTokensApi, type APIToken, type CreateAPITokenRequest } from "@/api/apiTokens";

interface APITokensState {
  tokens: APIToken[];
}

export const useAPITokensStore = defineStore("apiTokens", () => {
  const state = useBaseStore<APITokensState>({
    tokens: [],
  });
  
  const tokens = computed(() => state.data.value.tokens || []);

  async function loadTokens(forceReload = false) {
    if (!forceReload && state.data.value.tokens && state.data.value.tokens.length > 0) {
      return { success: true, data: state.data.value.tokens };
    }
    return await state.callApi({
      apiCall: () => apiTokensApi.listTokens(),
      operationKey: 'loadTokens',
      onSuccess: (response) => {
        state.data.value.tokens = (response as APIToken[]) || [];
      },
      showToast: false,
    });
  }

  async function createToken(data: CreateAPITokenRequest) {
    return await state.callApi({
      apiCall: () => apiTokensApi.createToken(data),
      successMessage: "API token created successfully",
      operationKey: 'createToken',
      onSuccess: async () => {
        await loadTokens(true);
      },
    });
  }

  async function deleteToken(tokenId: number) {
    return await state.callApi({
      apiCall: () => apiTokensApi.deleteToken(tokenId),
      successMessage: "API token deleted successfully",
      operationKey: `deleteToken-${tokenId}`,
      onSuccess: async () => {
        await loadTokens(true);
      },
    });
  }

  return {
    // State
    tokens,
    isLoading: state.isLoading,
    error: state.error,
    
    // Actions
    loadTokens,
    createToken,
    deleteToken,
    
    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
  };
});