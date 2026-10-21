import { alertsDisabledResult } from "@/lib/alerts";
import { computed } from "vue";
import { defineStore } from "pinia";
import { alertsApi, type AlertHistoryEntry, type ResolveAlertRequest } from "@/api/alerts";
import { useBaseStore } from "./base";
import { useMetaStore } from "./meta";
import { i18n } from "@/i18n";

const legacyDeliveryAttemptLimit = 3;

export interface AlertDeliveryFailure {
  message: string;
  retriesExhausted: boolean;
}

export function alertDeliveryFailure(entry: AlertHistoryEntry): AlertDeliveryFailure | null {
  const payload = entry.payload;
  const message = typeof payload?.delivery_error === "string" ? payload.delivery_error : "";
  if (payload?.delivery_failed !== true) return null;
  const retriesExhausted = typeof payload?.delivery_retries_exhausted === "boolean"
    ? payload.delivery_retries_exhausted
    : typeof payload?.delivery_attempts === "number" && payload.delivery_attempts >= legacyDeliveryAttemptLimit;
  return { message, retriesExhausted };
}

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

  const entries = computed(() => state.data.value.entries);
  const currentAlertId = computed(() => state.data.value.currentAlertId);

  let historyRequestId = 0;

  function setCurrentContext(alertId: number | null) {
    if (alertId !== state.data.value.currentAlertId || alertId === null) {
      historyRequestId++;
      state.data.value.entries = [];
    }
    state.data.value.currentAlertId = alertId;
  }

  function setLimit(limit: number) {
    state.data.value.limit = limit > 0 ? limit : state.data.value.limit;
  }

  async function loadHistory(alertId: number, limit?: number) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    setCurrentContext(alertId);
    const requestId = ++historyRequestId;
    const effectiveLimit = limit ?? state.data.value.limit;
    return await state.callApi<AlertHistoryEntry[]>({
      apiCall: async () => {
        try {
          const response = await alertsApi.history(alertId, effectiveLimit);
          if (requestId !== historyRequestId) throw { status: "error", message: "Request superseded", error_type: "CanceledError" };
          return response;
        } catch (error) {
          if (requestId !== historyRequestId) throw { status: "error", message: "Request superseded", error_type: "CanceledError" };
          throw error;
        }
      },
      operationKey: `loadHistory-${alertId}`,
      onSuccess: (response) => {
        if (requestId !== historyRequestId) return;
        state.data.value.entries = response ?? [];
      },
      defaultData: [],
      showToast: false,
    });
  }

  async function resolveAlert(alertId: number, payload: ResolveAlertRequest) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    const requestId = historyRequestId;
    return await state.callApi<{ message: string }>({
      apiCall: () => alertsApi.resolve(alertId, payload),
      operationKey: `resolveAlert-${alertId}`,
      successMessage: i18n.global.t("alerts.alertResolved"),
      onSuccess: () => {
        if (requestId !== historyRequestId || alertId !== currentAlertId.value) return;
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
  }

  return {
    data: state.data,
    error: state.error,
    isLoading: state.isLoading,
    loadingStates: state.loadingStates,
    entries,
    currentAlertId,
    isLoadingOperation: state.isLoadingOperation,
    loadHistory,
    resolveAlert,
    setCurrentContext,
    setLimit,
  };
});
