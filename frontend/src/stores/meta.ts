import { defineStore } from "pinia";
import { computed } from "vue";
import { metaApi, type MetaResponse, type DashboardCachePolicy } from "@/api/meta";
import { useBaseStore } from "./base";
import type { APIErrorResponse } from "@/api/types";

interface MetaState {
  version: string | null;
  httpServerTimeout: string | null;
  maxQueryLimit: number;
  maxQueryTimeoutSeconds: number;
  defaultPreviewLimit: number;
  maxPreviewLimit: number;
  maxExportRows: number;
  alertsEnabled: boolean;
  localAuthEnabled: boolean;
  oidcEnabled: boolean;
  // Server dashboard-cache policy. null = absent (old server) OR malformed →
  // "cache unavailable / fail closed" (resolveEffectiveCacheTtl returns 0).
  dashboardCachePolicy: DashboardCachePolicy | null;
  isInitialized: boolean;
}

// Validate the server's dashboard_cache blob. Anything absent or the wrong
// shape collapses to null so callers fail closed rather than caching against a
// guessed policy (which would change query-window semantics + risk stale data).
function parseDashboardCachePolicy(
  raw: MetaResponse["dashboard_cache"]
): DashboardCachePolicy | null {
  if (!raw || typeof raw !== "object") return null;
  if (typeof raw.enabled !== "boolean") return null;
  if (typeof raw.default_ttl_seconds !== "number" || typeof raw.max_ttl_seconds !== "number") {
    return null;
  }
  return {
    enabled: raw.enabled,
    default_ttl_seconds: raw.default_ttl_seconds,
    max_ttl_seconds: raw.max_ttl_seconds,
  };
}

export const useMetaStore = defineStore("meta", () => {
  const state = useBaseStore<MetaState>({
    version: null,
    httpServerTimeout: null,
    maxQueryLimit: 100000,
    maxQueryTimeoutSeconds: 120,
    defaultPreviewLimit: 1000,
    maxPreviewLimit: 100000,
    maxExportRows: 1000000,
    // Default true so an older server that doesn't advertise the field
    // keeps working; disabling is an opt-in signalled by the server.
    alertsEnabled: true,
    localAuthEnabled: false,
    oidcEnabled: true,
    dashboardCachePolicy: null,
    isInitialized: false,
  });

  // Computed properties
  const version = computed(() => state.data.value.version);
  const httpServerTimeout = computed(() => state.data.value.httpServerTimeout);
  const maxQueryLimit = computed(() => state.data.value.maxQueryLimit);
  const maxQueryTimeoutSeconds = computed(() => state.data.value.maxQueryTimeoutSeconds);
  const defaultPreviewLimit = computed(() => state.data.value.defaultPreviewLimit);
  const maxPreviewLimit = computed(() => state.data.value.maxPreviewLimit);
  const maxExportRows = computed(() => state.data.value.maxExportRows);
  const alertsEnabled = computed(() => state.data.value.alertsEnabled);
  const localAuthEnabled = computed(() => state.data.value.localAuthEnabled);
  const oidcEnabled = computed(() => state.data.value.oidcEnabled);
  const dashboardCachePolicy = computed(() => state.data.value.dashboardCachePolicy);
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
              state.data.value.maxQueryTimeoutSeconds = response.max_query_timeout_seconds ?? 120;
              state.data.value.defaultPreviewLimit = response.default_preview_limit ?? response.max_query_limit;
              state.data.value.maxPreviewLimit = response.max_preview_limit ?? response.max_query_limit;
              state.data.value.maxExportRows = response.max_export_rows ?? 1000000;
              state.data.value.alertsEnabled = response.alerts_enabled ?? true;
              state.data.value.localAuthEnabled = response.local_auth_enabled ?? false;
              state.data.value.oidcEnabled = response.oidc_enabled ?? true;
              state.data.value.dashboardCachePolicy = parseDashboardCachePolicy(response.dashboard_cache);
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
    state.data.value.maxQueryTimeoutSeconds = 120;
    state.data.value.defaultPreviewLimit = 1000;
    state.data.value.maxPreviewLimit = 100000;
    state.data.value.maxExportRows = 1000000;
    state.data.value.alertsEnabled = true;
    state.data.value.localAuthEnabled = false;
    state.data.value.oidcEnabled = true;
    state.data.value.dashboardCachePolicy = null;
    state.data.value.isInitialized = false;
  }

  return {
    version,
    httpServerTimeout,
    maxQueryLimit,
    maxQueryTimeoutSeconds,
    defaultPreviewLimit,
    maxPreviewLimit,
    maxExportRows,
    alertsEnabled,
    localAuthEnabled,
    oidcEnabled,
    dashboardCachePolicy,
    isInitialized,
    error,
    loadMeta,
    clearState,
    // Loading state helpers
    isLoading: computed(() => state.isLoading.value),
    isLoadingOperation: state.isLoadingOperation,
  };
});
