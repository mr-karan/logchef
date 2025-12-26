import { computed } from "vue";
import { defineStore } from "pinia";
import { alertsApi, type AlertHistoryEntry, type ResolveAlertRequest } from "@/api/alerts";
import { useBaseStore } from "./base";
import { useContextStore } from "./context";
import type { APIErrorResponse } from "@/api/types";

interface AlertHistoryState {
  entries: AlertHistoryEntry[];
  currentAlertId: number | null;
  limit: number;
}

export const useAlertHistoryStore = defineStore("alertHistory", () => {
  const state = useBaseStore<AlertHistoryState>({
    entries: [],
    currentAlertId: null,
    limit: 100,
  });

  const contextStore = useContextStore();

  const entries = computed(() => state.data.value.entries);
  const currentAlertId = computed(() => state.data.value.currentAlertId);
  const hasHistory = computed(() => entries.value.length > 0);

  function setCurrentContext(alertId: number | null) {
    state.data.value.currentAlertId = alertId;
    if (!alertId) {
      state.data.value.entries = [];
    }
  }

  function setLimit(limit: number) {
    state.data.value.limit = limit > 0 ? limit : state.data.value.limit;
  }

  async function loadHistory(alertId: number, teamId?: number | null, sourceId?: number | null, limit?: number) {
    const effectiveTeamId = teamId ?? contextStore.teamId;
    const effectiveSourceId = sourceId ?? contextStore.sourceId;
    if (!effectiveTeamId || !effectiveSourceId) {
      const error: APIErrorResponse = {
        status: "error",
        message: "Missing team or source context",
        error_type: "ValidationError",
      };
      return { success: false, error };
    }
    setCurrentContext(alertId);
    const effectiveLimit = limit ?? state.data.value.limit;
    return await state.withLoading(`loadHistory-${alertId}`, async () => {
      return await state.callApi<AlertHistoryEntry[]>({
        apiCall: () => alertsApi.listAlertHistory(effectiveTeamId, effectiveSourceId, alertId, effectiveLimit),
        operationKey: `loadHistory-${alertId}`,
        onSuccess: (response) => {
          state.data.value.entries = response ?? [];
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function resolveCurrentAlert(message?: string) {
    const teamId = contextStore.teamId;
    const sourceId = contextStore.sourceId;
    const alertId = state.data.value.currentAlertId;
    if (!teamId || !sourceId || !alertId) {
      const error: APIErrorResponse = {
        status: "error",
        message: "Missing alert context",
        error_type: "ValidationError",
      };
      return { success: false, error };
    }
    return resolveAlert(teamId, sourceId, alertId, { message });
  }

  async function resolveAlert(teamId: number, sourceId: number, alertId: number, payload: ResolveAlertRequest) {
    return await state.withLoading(`resolveAlert-${alertId}`, async () => {
      return await state.callApi<{ message: string }>({
        apiCall: () => alertsApi.resolveAlert(teamId, sourceId, alertId, payload),
        operationKey: `resolveAlert-${alertId}`,
        successMessage: "Alert resolved",
        onSuccess: () => {
          // Optimistically mark the latest triggered entry as resolved.
          const idx = state.data.value.entries.findIndex((entry) => entry.status === "triggered");
          if (idx !== -1) {
            const entry = state.data.value.entries[idx];
            state.data.value.entries.splice(idx, 1, {
              ...entry,
              status: "resolved",
              resolved_at: new Date().toISOString(),
              message: payload.message ?? entry.message ?? undefined,
            });
          }
        },
      });
    });
  }

  return {
    data: state.data,
    error: state.error,
    isLoading: state.isLoading,
    loadingStates: state.loadingStates,
    entries,
    currentAlertId,
    hasHistory,
    isLoadingOperation: state.isLoadingOperation,
    loadHistory,
    resolveAlert,
    resolveCurrentAlert,
    setCurrentContext,
    setLimit,
  };
});
