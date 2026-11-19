import { defineStore } from "pinia";
import { ref } from "vue";

/**
 * The active saved view and the team and source it belongs to. The Explorer's
 * URL writer reads it, and it sits in the entry bundle through that writer, so
 * it stays free of the savedViews store's API client and storage schemas.
 */
export const useActiveSavedViewStore = defineStore("activeSavedView", () => {
  const scope = ref<{ teamId: number; sourceId: number } | null>(null);
  const viewId = ref<number | null>(null);

  /** The id to write to the URL for this team and source, if a view is active there. */
  function viewParameter(teamId: number | null, sourceId: number | null): number | null {
    return scope.value && scope.value.teamId === teamId && scope.value.sourceId === sourceId ? viewId.value : null;
  }

  return { scope, viewId, viewParameter };
});
