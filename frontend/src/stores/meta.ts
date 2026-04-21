import { defineStore } from "pinia";
import { computed } from "vue";
import { metaApi, type MetaResponse } from "@/api/meta";
import { useBaseStore } from "./base";
import type { APIErrorResponse } from "@/api/types";

interface MetaState {
  version: string | null;
  httpServerTimeout: string | null;
  maxQueryLimit: number;
  defaultPreviewLimit: number;
  maxPreviewLimit: number;
  maxExportRows: number;
  isInitialized: boolean;
}

export const useMetaStore = defineStore("meta", () => {
  const state = useBaseStore<MetaState>({
    version: null,
    httpServerTimeout: null,
    maxQueryLimit: 100000,
    defaultPreviewLimit: 1000,
    maxPreviewLimit: 100000,
    maxExportRows: 1000000,
    isInitialized: false,
  });

  // Computed properties
  const version = computed(() => state.data.value.version);
  const httpServerTimeout = computed(() => state.data.value.httpServerTimeout);
  const maxQueryLimit = computed(() => state.data.value.maxQueryLimit);
  const defaultPreviewLimit = computed(() => state.data.value.defaultPreviewLimit);
  const maxPreviewLimit = computed(() => state.data.value.maxPreviewLimit);
  const maxExportRows = computed(() => state.data.value.maxExportRows);
  const isInitialized = computed(() => state.data.value.isInitialized);
  const error = computed(() => state.error.value);

  // Load server metadata
  async function loadMeta() {
    if (isInitialized.value) {
      return { success: true, message: "Meta already loaded" };
    }

    return await state.withLoading('loadMeta', async () => {
      try {
        const result = await state.callApi({
          apiCall: () => metaApi.getMeta(),
          showToast: false, // Don't show toast for meta loading
          operationKey: 'loadMeta',
          onSuccess: (response: MetaResponse | null) => {
            if (response) {
              state.data.value.version = response.version;
              state.data.value.httpServerTimeout = response.http_server_timeout;
              state.data.value.maxQueryLimit = response.max_query_limit;
              state.data.value.defaultPreviewLimit = response.default_preview_limit ?? response.max_query_limit;
              state.data.value.maxPreviewLimit = response.max_preview_limit ?? response.max_query_limit;
              state.data.value.maxExportRows = response.max_export_rows ?? 1000000;
              state.data.value.isInitialized = true;
            }
          },
          onError: (error) => {
            console.error("Failed to load server metadata:", error);
          },
        });

        return result;
      } catch (error) {
        return state.handleError(error as Error | APIErrorResponse, 'loadMeta');
      }
    });
  }

  // Clear meta state
  function clearState() {
    state.data.value.version = null;
    state.data.value.httpServerTimeout = null;
    state.data.value.maxQueryLimit = 100000;
    state.data.value.defaultPreviewLimit = 1000;
    state.data.value.maxPreviewLimit = 100000;
    state.data.value.maxExportRows = 1000000;
    state.data.value.isInitialized = false;
  }

  return {
    version,
    httpServerTimeout,
    maxQueryLimit,
    defaultPreviewLimit,
    maxPreviewLimit,
    maxExportRows,
    isInitialized,
    error,
    loadMeta,
    clearState,
    // Loading state helpers
    isLoading: computed(() => state.isLoading.value),
    isLoadingOperation: state.isLoadingOperation,
  };
});
