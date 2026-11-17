import { computed } from "vue";
import { defineStore } from "pinia";
import { alertsApi, type AlertHistoryEntry, type AlertInstance, type ResolveAlertRequest } from "@/api/alerts";
import { createLatestRequests, useBaseStore } from "./base";
import { useMetaStore } from "./meta";
import { alertsDisabledResult } from "@/lib/alerts";
import { i18n } from "@/i18n";

interface AlertHistoryState {
  entries: AlertHistoryEntry[];
  instances: AlertInstance[];
  currentAlertId: number | null;
  limit: number;
}

export const useAlertHistoryStore = defineStore("alertHistory", () => {
  const state = useBaseStore<AlertHistoryState>({
    entries: [],
    instances: [],
    currentAlertId: null,
    limit: 100,
  });

  const entries = computed(() => state.data.value.entries);
  const instances = computed(() => state.data.value.instances);
  const currentAlertId = computed(() => state.data.value.currentAlertId);

  let historyRequestId = 0;
  const historyLoads = createLatestRequests({
    request: (alertId: number, limit: number) => `${alertId}:${limit}`,
    slot: () => "history",
    load: fetchHistory,
  });
  const instanceLoads = createLatestRequests({
    request: (alertId: number) => String(alertId),
    slot: () => "instances",
    load: fetchInstances,
  });

  function setCurrentContext(alertId: number | null) {
    if (alertId !== state.data.value.currentAlertId || alertId === null) {
      historyRequestId++;
      historyLoads.cancelAll();
      instanceLoads.cancelAll();
      state.data.value.entries = [];
      state.data.value.instances = [];
    }
    state.data.value.currentAlertId = alertId;
  }

  function setLimit(limit: number) {
    state.data.value.limit = limit > 0 ? limit : state.data.value.limit;
  }

  async function loadHistory(alertId: number, limit?: number) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    setCurrentContext(alertId);
    historyRequestId++;
    return await historyLoads.run([alertId, limit ?? state.data.value.limit]);
  }

  async function fetchHistory(signal: AbortSignal, alertId: number, limit: number) {
    return await state.callApi<AlertHistoryEntry[]>({
      apiCall: () => alertsApi.history(alertId, limit, { signal }),
      operationKey: `loadHistory-${alertId}`,
      onSuccess: (response) => {
        state.data.value.entries = response ?? [];
      },
      defaultData: [],
      showToast: false,
    });
  }

  async function loadInstances(alertId: number) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    setCurrentContext(alertId);
    return await instanceLoads.run([alertId]);
  }

  async function fetchInstances(signal: AbortSignal, alertId: number) {
    return await state.callApi<AlertInstance[]>({
      apiCall: () => alertsApi.instances(alertId, { signal }),
      operationKey: `loadInstances-${alertId}`,
      onSuccess: (response) => {
        state.data.value.instances = response ?? [];
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
        // Optimistically mark the resolved group's open entries, or every
        // open entry when no group was named.
        const resolvedAt = new Date().toISOString();
        state.data.value.entries = state.data.value.entries.map((entry) =>
          entry.status === "triggered" && (payload.group_key === undefined || entry.group_key === payload.group_key)
            ? { ...entry, status: "resolved", resolved_at: resolvedAt, message: payload.message ?? entry.message ?? undefined }
            : entry,
        );
      },
    });
  }

  return {
    data: state.data,
    error: state.error,
    isLoading: state.isLoading,
    loadingStates: state.loadingStates,
    entries,
    instances,
    currentAlertId,
    isLoadingOperation: state.isLoadingOperation,
    loadHistory,
    loadInstances,
    resolveAlert,
    setCurrentContext,
    setLimit,
  };
});
