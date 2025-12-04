import { computed } from "vue";
import { defineStore } from "pinia";
import { alertsApi, type Alert, type CreateAlertRequest, type UpdateAlertRequest } from "@/api/alerts";
import { useBaseStore } from "./base";

interface AlertsState {
  alerts: Alert[];
  selectedAlertId: number | null;
  lastLoaded: {
    teamId: number | null;
    sourceId: number | null;
  };
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
    lastLoaded: {
      teamId: null,
      sourceId: null,
    },
  });

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

  async function fetchAlerts(teamId: number, sourceId: number) {
    state.data.value.lastLoaded = {
      teamId,
      sourceId,
    };
    return await state.withLoading(`fetchAlerts-${teamId}-${sourceId}`, async () => {
      return await state.callApi<Alert[]>({
        apiCall: () => alertsApi.listAlerts(teamId, sourceId),
        operationKey: `fetchAlerts-${teamId}-${sourceId}`,
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

  async function createAlert(teamId: number, sourceId: number, payload: CreateAlertRequest) {
    return await state.withLoading("createAlert", async () => {
      return await state.callApi<Alert>({
        apiCall: () => alertsApi.createAlert(teamId, sourceId, payload),
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

  async function updateAlert(teamId: number, sourceId: number, alertId: number, payload: UpdateAlertRequest) {
    return await state.withLoading(`updateAlert-${alertId}`, async () => {
      return await state.callApi<Alert>({
        apiCall: () => alertsApi.updateAlert(teamId, sourceId, alertId, payload),
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

  async function deleteAlert(teamId: number, sourceId: number, alertId: number) {
    return await state.withLoading(`deleteAlert-${alertId}`, async () => {
      return await state.callApi<{ message: string }>({
        apiCall: () => alertsApi.deleteAlert(teamId, sourceId, alertId),
        operationKey: `deleteAlert-${alertId}`,
        successMessage: "Alert deleted",
        onSuccess: () => {
          removeAlert(alertId);
        },
      });
    });
  }

  async function refreshAlert(teamId: number, sourceId: number, alertId: number) {
    return await state.withLoading(`refreshAlert-${alertId}`, async () => {
      return await state.callApi<Alert>({
        apiCall: () => alertsApi.getAlert(teamId, sourceId, alertId),
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
    teamId: number,
    sourceId: number,
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
