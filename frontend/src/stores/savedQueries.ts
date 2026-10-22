import { defineStore } from "pinia";
import { computed, watch } from "vue";
import {
  savedQueriesApi,
  type SavedQuery,
  type SavedQueryContent,
} from "@/api/savedQueries";
import type { APIErrorResponse } from "@/api/types";
import type { TeamSummary } from "@/api/teams";
import type { QueryLanguage, SavedQueryEditorMode } from "@/lib/queryMetadata";
import { useApiQuery } from "@/composables/useApiQuery";
import { useBaseStore } from "./base";
import { useContextStore } from "./context";
import { i18n } from "@/i18n";

interface SavedQueriesState {
  queries: SavedQuery[];
  // allQueries backs the admin "All queries" browse surface. Kept separate from
  // `queries` so it doesn't clobber the explorer dropdown's per-source list.
  allQueries: SavedQuery[];
  selectedQuery: SavedQuery | null;
  teams: TeamSummary[];
}

export const useSavedQueriesStore = defineStore("savedQueries", () => {
  const state = useBaseStore<SavedQueriesState>({
    queries: [],
    allQueries: [],
    selectedQuery: null,
    teams: [],
  });

  const contextStore = useContextStore();
  let listVersion = 0;
  let inFlightList: {
    sourceId: number | undefined;
    promise: Promise<{ success: boolean; data?: SavedQuery[] | null; error?: APIErrorResponse | null }>;
  } | null = null;

  watch(
    [() => contextStore.teamId, () => contextStore.sourceId],
    () => {
      listVersion++;
      inFlightList = null;
      state.data.value.queries = [];
      state.data.value.selectedQuery = null;
    },
    { flush: "sync" },
  );

  const queries = computed(() => state.data.value.queries);
  const selectedQuery = computed(() => state.data.value.selectedQuery);
  const teams = computed(() => state.data.value.teams);
  const selectedTeamId = computed(() => contextStore.teamId);

  const hasQueries = computed(() => (queries.value?.length || 0) > 0);

  async function fetchUserTeams() {
    return await state.callApi<TeamSummary[]>({
      apiCall: () => savedQueriesApi.getUserTeams(),
      operationKey: 'fetchUserTeams',
      onSuccess: (response) => {
        state.data.value.teams = response ?? [];
        if (response && response.length > 0 && !contextStore.teamId) {
          contextStore.selectTeam(response[0].id);
        }
      },
      defaultData: []
    });
  }

  // list fetches saved queries the user can see. Optional sourceId narrows to one source.
  function list(sourceId?: number) {
    if (inFlightList && inFlightList.sourceId === sourceId) return inFlightList.promise;
    const version = ++listVersion;
    const key = sourceId ? `listSavedQueries-${sourceId}` : 'listSavedQueries';
    const { execute } = useApiQuery<SavedQuery[]>();
    const promise = state.withLoading(key, async () => {
      state.error.value = null;
      const result = await execute(() => savedQueriesApi.list(sourceId), { defaultData: [], showToast: false });
      if (version !== listVersion) return { success: false, data: null, error: null };
      if (result.success) state.data.value.queries = result.data ?? [];
      else state.error.value = result.error ?? null;
      return result;
    }).finally(() => {
      if (inFlightList?.promise === promise) inFlightList = null;
    });
    inFlightList = { sourceId, promise };
    return promise;
  }

  const allQueries = computed(() => state.data.value.allQueries);

  // listAll fetches every saved query (global-admin only) for the Library
  // "All queries" browse surface. Rows the caller can't run come back with
  // runnable=false so the UI can lock them.
  async function listAll() {
    return await state.callApi<SavedQuery[]>({
      apiCall: () => savedQueriesApi.listAll(),
      operationKey: 'listAllSavedQueries',
      onSuccess: (responseData) => {
        state.data.value.allQueries = responseData ?? [];
      },
      defaultData: [],
      showToast: false,
    });
  }

  async function fetchById(queryId: number | string) {
    const key = `fetchSavedQuery-${queryId}`;
    return await state.callApi<SavedQuery>({
      apiCall: () => savedQueriesApi.resolve(queryId, contextStore.teamId),
      operationKey: key,
      onSuccess: (response) => {
        state.data.value.selectedQuery = response;
      },
    });
  }

  async function create(
    sourceId: number,
    createdFromTeamId: number | null | undefined,
    name: string,
    description: string,
    queryContent: SavedQueryContent,
    queryLanguage: QueryLanguage,
    editorMode: SavedQueryEditorMode,
  ) {
    const key = `createSavedQuery-${sourceId}`;
    const apiQueryContent = { ...queryContent };
    const payload = {
      source_id: sourceId,
      created_from_team_id: createdFromTeamId ?? null,
      name,
      description,
      query_language: queryLanguage,
      editor_mode: editorMode,
      query_content: JSON.stringify(apiQueryContent),
    };

    return await state.callApi<SavedQuery>({
      apiCall: () => savedQueriesApi.create(payload),
      operationKey: key,
      successMessage: i18n.global.t("library.queryCreated"),
      onSuccess: (response) => {
        if (response) {
          if (!state.data.value.queries) {
            state.data.value.queries = [];
          }
          state.data.value.queries.unshift(response);
          state.data.value.selectedQuery = response;
        }
      }
    });
  }

  async function update(
    queryId: number | string,
    payload: Partial<Omit<SavedQuery, "id" | "source_id" | "created_by" | "created_at" | "updated_at">>
  ) {
    const key = `updateSavedQuery-${queryId}`;
    return await state.callApi<SavedQuery>({
      apiCall: () => savedQueriesApi.update(queryId, payload),
      operationKey: key,
      successMessage: i18n.global.t("library.queryUpdated"),
      onSuccess: (response) => {
        if (response) {
          const index = state.data.value.queries.findIndex(
            (q) => String(q.id) === String(queryId)
          );
          if (index >= 0) {
            state.data.value.queries[index] = {
              ...state.data.value.queries[index],
              ...response
            };
          }
          if (state.data.value.selectedQuery?.id === Number(queryId)) {
            state.data.value.selectedQuery = {
              ...state.data.value.selectedQuery,
              ...response
            };
          }
        }
      }
    });
  }

  async function remove(queryId: number | string) {
    const key = `deleteSavedQuery-${queryId}`;
    return await state.callApi<{ message: string }>({
      apiCall: () => savedQueriesApi.delete(queryId),
      operationKey: key,
      successMessage: i18n.global.t("library.queryDeleted"),
      onSuccess: () => {
        state.data.value.queries = state.data.value.queries.filter(
          (q) => String(q.id) !== String(queryId)
        );
        if (state.data.value.selectedQuery?.id === Number(queryId)) {
          state.data.value.selectedQuery = null;
        }
      }
    });
  }

  return {
    isLoading: state.isLoading,
    error: state.error,
    data: state.data,

    queries,
    allQueries,
    selectedQuery,
    teams,
    selectedTeamId,
    hasQueries,

    fetchUserTeams,
    list,
    listAll,
    fetchById,
    create,
    update,
    remove,

    isLoadingOperation: state.isLoadingOperation,
  };
});
