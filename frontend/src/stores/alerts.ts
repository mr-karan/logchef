import { computed, watch } from "vue";
import { defineStore } from "pinia";
import { alertsApi, type Alert, type CreateAlertRequest, type UpdateAlertRequest } from "@/api/alerts";
import { useBaseStore } from "./base";
import { useContextStore } from "./context";
import { useMetaStore } from "./meta";

interface AlertsState {
  alerts: Alert[];
  selectedAlertId: number | null;
}

function sortAlerts(a: Alert, b: Alert) {
  const aTime = a.updated_at || a.created_at;
  const bTime = b.updated_at || b.created_at;
  return new Date(bTime).getTime() - new Date(aTime).getTime();
}

export const useAlertsStore = defineStore("alerts", () => {
  const state = useBaseStore<AlertsState>({
    alerts: [],
    selectedAlertId: null,
  });

  const contextStore = useContextStore();

  watch(
    [() => contextStore.teamId, () => contextStore.sourceId],
    () => {
      state.data.value.alerts = [];
      state.data.value.selectedAlertId = null;
    }
  );

  const alerts = computed(() => state.data.value.alerts);
  const selectedAlertId = computed(() => state.data.value.selectedAlertId);
  const selectedAlert = computed(() =>
    alerts.value.find((alert) => alert.id === state.data.value.selectedAlertId) || null
  );
  const hasAlerts = computed(() => alerts.value.length > 0);

  function setSelectedAlert(alertId: number | null) {
    state.data.value.selectedAlertId = alertId;
  }

  function clearAlerts() {
    state.data.value.alerts = [];
    state.data.value.selectedAlertId = null;
  }

  // Belt-and-braces: even if the router guard is bypassed (bookmarked URL
  // hitting the view before meta has loaded, or a devtools call), stop
  // firing alert HTTP when the server advertises alerts as disabled.
  function alertsDisabledResult() {
    return { success: false as const, data: null };
  }

  async function fetchAlerts(_teamId?: number | undefined, sourceId?: number) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    const key = sourceId ? `fetchAlerts-${sourceId}` : 'fetchAlerts-all';
    return await state.withLoading(key, async () => {
      return await state.callApi<Alert[]>({
        apiCall: () => alertsApi.list(sourceId),
        operationKey: key,
        onSuccess: (response) => {
          state.data.value.alerts = (response ?? []).slice().sort(sortAlerts);
          // Clear selected alert if it no longer exists in the new list.
          if (
            state.data.value.selectedAlertId &&
            !state.data.value.alerts.some((alert) => alert.id === state.data.value.selectedAlertId)
          ) {
            state.data.value.selectedAlertId = null;
          }
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  function upsertAlert(alert: Alert) {
    const alertsRef = state.data.value.alerts;
    const index = alertsRef.findIndex((existing) => existing.id === alert.id);
    if (index === -1) {
      alertsRef.unshift(alert);
    } else {
      alertsRef.splice(index, 1, alert);
    }
    state.data.value.alerts = alertsRef.slice().sort(sortAlerts);
  }

  function removeAlert(alertId: number) {
    state.data.value.alerts = state.data.value.alerts.filter((alert) => alert.id !== alertId);
    if (state.data.value.selectedAlertId === alertId) {
      state.data.value.selectedAlertId = null;
    }
  }

  async function createAlert(_teamId: number | undefined, sourceId: number, payload: Omit<CreateAlertRequest, "source_id">) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    return await state.withLoading("createAlert", async () => {
      return await state.callApi<Alert>({
        apiCall: () => alertsApi.create({ ...payload, source_id: sourceId }),
        operationKey: "createAlert",
        successMessage: "Alert created successfully",
        onSuccess: (response) => {
          if (response) {
            upsertAlert(response);
            state.data.value.selectedAlertId = response.id;
          }
        },
      });
    });
  }

  async function updateAlert(_teamId: number | undefined, _sourceId: number | undefined, alertId: number, payload: UpdateAlertRequest) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    return await state.withLoading(`updateAlert-${alertId}`, async () => {
      return await state.callApi<Alert>({
        apiCall: () => alertsApi.update(alertId, payload),
        operationKey: `updateAlert-${alertId}`,
        successMessage: "Alert updated successfully",
        onSuccess: (response) => {
          if (response) {
            upsertAlert(response);
          }
        },
      });
    });
  }

  async function deleteAlert(_teamId: number | undefined, _sourceId: number | undefined, alertId: number) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    return await state.withLoading(`deleteAlert-${alertId}`, async () => {
      return await state.callApi<{ message: string }>({
        apiCall: () => alertsApi.delete(alertId),
        operationKey: `deleteAlert-${alertId}`,
        successMessage: "Alert deleted",
        onSuccess: () => {
          removeAlert(alertId);
        },
      });
    });
  }

  async function refreshAlert(_teamId: number | undefined, _sourceId: number | undefined, alertId: number) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    return await state.withLoading(`refreshAlert-${alertId}`, async () => {
      return await state.callApi<Alert>({
        apiCall: () => alertsApi.get(alertId),
        operationKey: `refreshAlert-${alertId}`,
        onSuccess: (response) => {
          if (response) {
            upsertAlert(response);
          }
        },
        showToast: false,
      });
    });
  }

  async function toggleAlertActivity(
    teamId: number | undefined,
    sourceId: number | undefined,
    alertId: number,
    isActive: boolean
  ) {
    return await updateAlert(teamId, sourceId, alertId, { is_active: isActive });
  }

  return {
    // state
    data: state.data,
    error: state.error,
    isLoading: state.isLoading,
    loadingStates: state.loadingStates,
    alerts,
    selectedAlert,
    selectedAlertId,
    hasAlerts,

    // utils
    isLoadingOperation: state.isLoadingOperation,
    setSelectedAlert,
    clearAlerts,
    fetchAlerts,
    createAlert,
    updateAlert,
    deleteAlert,
    refreshAlert,
    toggleAlertActivity,
  };
});
