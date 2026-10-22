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

  function setCurrentContext(alertId: number | null) {
    state.data.value.currentAlertId = alertId;
    if (!alertId) {
      state.data.value.entries = [];
    }
  }

  function setLimit(limit: number) {
    state.data.value.limit = limit > 0 ? limit : state.data.value.limit;
  }

  // Belt-and-braces: match the guard in useAlertsStore so that a stale
  // bookmarked URL bypassing the router guard cannot fire alert HTTP.
  function alertsDisabledResult() {
    return { success: false as const, data: null };
  }

  async function loadHistory(alertId: number, _teamId?: number | null, _sourceId?: number | null, limit?: number) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    setCurrentContext(alertId);
    const effectiveLimit = limit ?? state.data.value.limit;
    return await state.callApi<AlertHistoryEntry[]>({
      apiCall: () => alertsApi.history(alertId, effectiveLimit),
      operationKey: `loadHistory-${alertId}`,
      onSuccess: (response) => {
        state.data.value.entries = response ?? [];
      },
      defaultData: [],
      showToast: false,
    });
  }

  async function resolveAlert(_teamId: number | undefined, _sourceId: number | undefined, alertId: number, payload: ResolveAlertRequest) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    return await state.callApi<{ message: string }>({
      apiCall: () => alertsApi.resolve(alertId, payload),
      operationKey: `resolveAlert-${alertId}`,
      successMessage: i18n.global.t("alerts.alertResolved"),
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
    setLimit,
  };
});
