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
    return await state.withLoading('loadTokens', async () => {
      if (!forceReload && state.data.value.tokens && state.data.value.tokens.length > 0) {
        return { success: true, data: state.data.value.tokens };
      }
      
      return await state.callApi({
        apiCall: () => apiTokensApi.listTokens(),
        operationKey: 'loadTokens',
        onSuccess: (response) => {
          state.data.value.tokens = response || [];
        },
        showToast: false,
      });
    });
  }

  async function createToken(data: CreateAPITokenRequest) {
    return await state.withLoading('createToken', async () => {
      const result = await state.callApi({
        apiCall: () => apiTokensApi.createToken(data),
        successMessage: "API token created successfully",
        operationKey: 'createToken',
      });
      
      if (result && result.success) {
        // Reload tokens to ensure frontend state is in sync
        await loadTokens(true);
      }
      
      return result;
    });
  }

  async function deleteToken(tokenId: number) {
    return await state.withLoading(`deleteToken-${tokenId}`, async () => {
      const result = await state.callApi({
        apiCall: () => apiTokensApi.deleteToken(tokenId),
        successMessage: "API token deleted successfully",
        operationKey: `deleteToken-${tokenId}`,
      });
      
      if (result && result.success) {
        // Reload tokens to ensure frontend state is in sync
        await loadTokens(true);
      }
      
      return result;
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