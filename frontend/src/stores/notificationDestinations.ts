import { computed } from "vue";
import { defineStore } from "pinia";
import {
  notificationDestinationsApi,
  type DestinationRequest,
  type DestinationTestRequest,
  type DestinationTestResponse,
  type NotificationDestination,
} from "@/api/notificationDestinations";
import { useBaseStore } from "./base";
import { i18n } from "@/i18n";

interface DestinationsState {
  destinations: NotificationDestination[];
}

export const useNotificationDestinationsStore = defineStore("notificationDestinations", () => {
  const state = useBaseStore<DestinationsState>({ destinations: [] });

  const destinations = computed(() => state.data.value.destinations);

  async function fetchDestinations() {
    return await state.callApi<NotificationDestination[]>({
      apiCall: () => notificationDestinationsApi.list(),
      operationKey: "fetchDestinations",
      showToast: false,
      onSuccess: (data) => {
        state.data.value.destinations = data ?? [];
      },
    });
  }

  function replace(destination: NotificationDestination | null) {
    if (!destination) return;
    const list = state.data.value.destinations;
    const i = list.findIndex((d) => d.id === destination.id);
    if (i >= 0) list.splice(i, 1, destination);
    else list.push(destination);
  }

  // The form shows save and test errors inline, so these calls do not toast.
  async function createDestination(payload: DestinationRequest) {
    return await state.callApi<NotificationDestination>({
      apiCall: () => notificationDestinationsApi.create(payload),
      operationKey: "saveDestination",
      showToast: false,
      onSuccess: replace,
    });
  }

  async function updateDestination(id: number, payload: DestinationRequest) {
    return await state.callApi<NotificationDestination>({
      apiCall: () => notificationDestinationsApi.update(id, payload),
      operationKey: "saveDestination",
      showToast: false,
      onSuccess: replace,
    });
  }

  // A destination that rules use is refused with 409; the toast carries the server's count.
  async function deleteDestination(id: number) {
    return await state.callApi<{ message: string }>({
      apiCall: () => notificationDestinationsApi.delete(id),
      operationKey: `deleteDestination-${id}`,
      successMessage: i18n.global.t("destinations.deleted"),
      onSuccess: () => {
        state.data.value.destinations = state.data.value.destinations.filter((d) => d.id !== id);
      },
    });
  }

  async function testDestination(payload: DestinationTestRequest) {
    return await state.callApi<DestinationTestResponse>({
      apiCall: () => notificationDestinationsApi.test(payload),
      operationKey: "testDestination",
      showToast: false,
    });
  }

  return {
    destinations,
    error: state.error,
    isLoadingOperation: state.isLoadingOperation,
    fetchDestinations,
    createDestination,
    updateDestination,
    deleteDestination,
    testDestination,
  };
});
