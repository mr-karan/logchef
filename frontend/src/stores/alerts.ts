import { computed } from "vue";
import { defineStore } from "pinia";
import { alertsApi, type Alert, type CreateAlertRequest, type UpdateAlertRequest } from "@/api/alerts";
import { useBaseStore } from "./base";
import { useMetaStore } from "./meta";
import { alertsDisabledResult } from "@/lib/alerts";
import { i18n } from "@/i18n";

interface AlertsState {
  alerts: Alert[];
}

function sortAlerts(a: Alert, b: Alert) {
  const aTime = a.updated_at || a.created_at;
  const bTime = b.updated_at || b.created_at;
  return new Date(bTime).getTime() - new Date(aTime).getTime();
}

export const useAlertsStore = defineStore("alerts", () => {
  const state = useBaseStore<AlertsState>({
    alerts: [],
  });

  const alerts = computed(() => state.data.value.alerts);

  let listRequestId = 0;

  function clearAlerts() {
    listRequestId++;
    state.data.value.alerts = [];
  }

  async function fetchAlerts(sourceId?: number) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    const requestId = ++listRequestId;
    const key = sourceId ? `fetchAlerts-${sourceId}` : 'fetchAlerts-all';
    return await state.callApi<Alert[]>({
      apiCall: async () => {
        try {
          const response = await alertsApi.list(sourceId);
          if (requestId !== listRequestId) throw { status: "error", message: "Request superseded", error_type: "CanceledError" };
          return response;
        } catch (error) {
          if (requestId !== listRequestId) throw { status: "error", message: "Request superseded", error_type: "CanceledError" };
          throw error;
        }
      },
      operationKey: key,
      onSuccess: (response) => {
        if (requestId !== listRequestId) return;
        state.data.value.alerts = (response ?? []).slice().sort(sortAlerts);
      },
      defaultData: [],
      showToast: false,
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
  }

  async function createAlert(sourceId: number, payload: Omit<CreateAlertRequest, "source_id">) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    return await state.callApi<Alert>({
      apiCall: () => alertsApi.create({ ...payload, source_id: sourceId }),
      operationKey: "createAlert",
      successMessage: i18n.global.t("alerts.alertCreated"),
      onSuccess: (response) => {
        if (response) {
          upsertAlert(response);
        }
      },
    });
  }

  async function updateAlert(alertId: number, payload: UpdateAlertRequest) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    return await state.callApi<Alert>({
      apiCall: () => alertsApi.update(alertId, payload),
      operationKey: `updateAlert-${alertId}`,
      successMessage: i18n.global.t("alerts.alertUpdated"),
      onSuccess: (response) => {
        if (response) {
          upsertAlert(response);
        }
      },
    });
  }

  async function deleteAlert(alertId: number) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    return await state.callApi<{ message: string }>({
      apiCall: () => alertsApi.delete(alertId),
      operationKey: `deleteAlert-${alertId}`,
      successMessage: i18n.global.t("alerts.alertDeleted"),
      onSuccess: () => {
        removeAlert(alertId);
      },
    });
  }

  async function refreshAlert(alertId: number) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
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
  }

  async function toggleAlertActivity(
    alertId: number,
    isActive: boolean
  ) {
    return await updateAlert(alertId, { is_active: isActive });
  }

  return {
    // state
    data: state.data,
    error: state.error,
    isLoading: state.isLoading,
    loadingStates: state.loadingStates,
    alerts,

    // utils
    isLoadingOperation: state.isLoadingOperation,
    clearAlerts,
    fetchAlerts,
    createAlert,
    updateAlert,
    deleteAlert,
    refreshAlert,
    toggleAlertActivity,
  };
});
