import { defineStore } from "pinia";
import { silencesApi, type AlertSilence, type AlertSilenceRequest } from "@/api/silences";
import { createLatestRequests, useBaseStore } from "./base";
import { useMetaStore } from "./meta";
import { alertsDisabledResult } from "@/lib/alerts";
import { i18n } from "@/i18n";

interface SilencesState {
  /** Silences by source id; the list and the alert detail show different sources. */
  bySource: Record<number, AlertSilence[]>;
}

export const useSilencesStore = defineStore("silences", () => {
  const state = useBaseStore<SilencesState>({ bySource: {} });

  const listLoads = createLatestRequests({
    request: (sourceId: number) => String(sourceId),
    load: fetchSilenceList,
  });

  function silencesOf(sourceId: number | null | undefined): AlertSilence[] {
    return sourceId ? state.data.value.bySource[sourceId] ?? [] : [];
  }

  async function fetchSilences(sourceId: number) {
    if (!useMetaStore().alertsEnabled) return alertsDisabledResult();
    return await listLoads.run([sourceId]);
  }

  async function fetchSilenceList(signal: AbortSignal, sourceId: number) {
    return await state.callApi<AlertSilence[]>({
      apiCall: () => silencesApi.list(sourceId, { signal }),
      operationKey: `fetchSilences-${sourceId}`,
      showToast: false,
      onSuccess: (data) => {
        state.data.value.bySource[sourceId] = data ?? [];
      },
    });
  }

  function replace(silence: AlertSilence | null) {
    if (!silence) return;
    const list = state.data.value.bySource[silence.source_id] ?? [];
    const i = list.findIndex((s) => s.id === silence.id);
    if (i >= 0) list.splice(i, 1, silence);
    else list.push(silence);
    state.data.value.bySource[silence.source_id] = list;
  }

  async function createSilence(payload: AlertSilenceRequest) {
    return await state.callApi<AlertSilence>({
      apiCall: () => silencesApi.create(payload),
      operationKey: "saveSilence",
      successMessage: i18n.global.t("silences.created"),
      onSuccess: replace,
    });
  }

  async function updateSilence(id: number, payload: AlertSilenceRequest) {
    return await state.callApi<AlertSilence>({
      apiCall: () => silencesApi.update(id, payload),
      operationKey: "saveSilence",
      successMessage: i18n.global.t("silences.saved"),
      onSuccess: replace,
    });
  }

  async function deleteSilence(silence: AlertSilence) {
    return await state.callApi<{ message: string }>({
      apiCall: () => silencesApi.delete(silence.id),
      operationKey: `deleteSilence-${silence.id}`,
      successMessage: i18n.global.t("silences.deleted"),
      onSuccess: () => {
        const list = state.data.value.bySource[silence.source_id];
        if (list) state.data.value.bySource[silence.source_id] = list.filter((s) => s.id !== silence.id);
      },
    });
  }

  return {
    error: state.error,
    isLoadingOperation: state.isLoadingOperation,
    silencesOf,
    fetchSilences,
    createSilence,
    updateSilence,
    deleteSilence,
  };
});
