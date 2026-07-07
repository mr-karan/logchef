import { defineStore } from "pinia";
import { computed, watch } from "vue";
import {
  savedQueriesApi,
  type SavedQuery,
  type Team,
  type SavedQueryContent,
} from "@/api/savedQueries";
import type { QueryLanguage, SavedQueryEditorMode } from "@/lib/queryMetadata";
import { useBaseStore } from "./base";
import { useContextStore } from "./context";

export interface SavedQueriesState {
  queries: SavedQuery[];
  // allQueries backs the admin "All queries" browse surface. Kept separate from
  // `queries` so it doesn't clobber the explorer dropdown's per-source list.
  allQueries: SavedQuery[];
  selectedQuery: SavedQuery | null;
  teams: Team[];
}

export const useSavedQueriesStore = defineStore("savedQueries", () => {
  const state = useBaseStore<SavedQueriesState>({
    queries: [],
    allQueries: [],
    selectedQuery: null,
    teams: [],
  });

  const contextStore = useContextStore();

  watch(
    [() => contextStore.teamId, () => contextStore.sourceId],
    () => {
      state.data.value.queries = [];
      state.data.value.selectedQuery = null;
    }
  );

  const parseQueryContent = (query: SavedQuery): SavedQueryContent => {
    try {
      const content = JSON.parse(query.query_content) as Partial<SavedQueryContent>;

      const isAbsoluteTimeRange = (tr: any): tr is { absolute: { start: number; end: number } } => {
        return tr && typeof tr === 'object' && tr.absolute && typeof tr.absolute.start === 'number' && typeof tr.absolute.end === 'number';
      };

      const isNullTimeRange = content.timeRange === null;

      return {
        version: content.version ?? 1,
        sourceId: content.sourceId ?? query.source_id,
        timeRange: isNullTimeRange
          ? null
          : (isAbsoluteTimeRange(content.timeRange)
              ? content.timeRange
              : { absolute: { start: Date.now() - 3600000, end: Date.now() } }),
        limit: typeof content.limit === 'number' ? content.limit : 100,
        content: typeof content.content === 'string' ? content.content : '',
        variables: Array.isArray(content.variables) ? content.variables : []
      };
    } catch (e) {
      console.error("Error parsing query content:", e);
      return {
        version: 1,
        sourceId: query.source_id,
        timeRange: {
          absolute: { start: Date.now() - 3600000, end: Date.now() },
        },
        limit: 100,
        content: '',
        variables: []
      };
    }
  };

  const queries = computed(() => state.data.value.queries);
  const selectedQuery = computed(() => state.data.value.selectedQuery);
  const teams = computed(() => state.data.value.teams);
  const selectedTeamId = computed(() => contextStore.teamId);

  const hasTeams = computed(() => (teams.value?.length || 0) > 0);
  const hasQueries = computed(() => (queries.value?.length || 0) > 0);
  const selectedTeam = computed(() => teams.value?.find((t) => t.id === selectedTeamId.value) || null);

  async function fetchUserTeams() {
    return await state.withLoading('fetchUserTeams', async () => {
      return await state.callApi<Team[]>({
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
    });
  }

  function setSelectedTeam(teamId: number) {
    contextStore.selectTeam(teamId);
  }

  // list fetches saved queries the user can see. Optional sourceId narrows to one source.
  async function list(sourceId?: number) {
    const key = sourceId ? `listSavedQueries-${sourceId}` : 'listSavedQueries';
    return await state.withLoading(key, async () => {
      return await state.callApi<SavedQuery[]>({
        apiCall: () => savedQueriesApi.list(sourceId),
        operationKey: key,
        onSuccess: (responseData) => {
          state.data.value.queries = responseData ?? [];
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  const allQueries = computed(() => state.data.value.allQueries);

  // listAll fetches every saved query (global-admin only) for the Library
  // "All queries" browse surface. Rows the caller can't run come back with
  // runnable=false so the UI can lock them.
  async function listAll() {
    return await state.withLoading('listAllSavedQueries', async () => {
      return await state.callApi<SavedQuery[]>({
        apiCall: () => savedQueriesApi.listAll(),
        operationKey: 'listAllSavedQueries',
        onSuccess: (responseData) => {
          state.data.value.allQueries = responseData ?? [];
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function fetchById(queryId: number | string) {
    const key = `fetchSavedQuery-${queryId}`;
    return await state.withLoading(key, async () => {
      return await state.callApi<SavedQuery>({
        apiCall: () => savedQueriesApi.resolve(queryId, contextStore.teamId),
        operationKey: key,
        onSuccess: (response) => {
          state.data.value.selectedQuery = response;
        },
      });
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
    return await state.withLoading(key, async () => {
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
        successMessage: "Query created successfully",
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
    });
  }

  async function update(
    queryId: number | string,
    payload: Partial<Omit<SavedQuery, "id" | "source_id" | "created_by" | "created_at" | "updated_at">>
  ) {
    const key = `updateSavedQuery-${queryId}`;
    return await state.withLoading(key, async () => {
      return await state.callApi<SavedQuery>({
        apiCall: () => savedQueriesApi.update(queryId, payload),
        operationKey: key,
        successMessage: "Query updated successfully",
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
    });
  }

  async function remove(queryId: number | string) {
    const key = `deleteSavedQuery-${queryId}`;
    return await state.withLoading(key, async () => {
      return await state.callApi<{ message: string }>({
        apiCall: () => savedQueriesApi.delete(queryId),
        operationKey: key,
        successMessage: "Query deleted successfully",
        onSuccess: () => {
          state.data.value.queries = state.data.value.queries.filter(
            (q) => String(q.id) !== String(queryId)
          );
          if (state.data.value.selectedQuery?.id === Number(queryId)) {
            state.data.value.selectedQuery = null;
          }
        }
      });
    });
  }


  function resetState() {
    state.data.value = {
      queries: [],
      allQueries: [],
      selectedQuery: null,
      teams: [],
    };
    state.error.value = null;
  }

  return {
    isLoading: state.isLoading,
    error: state.error,
    data: state.data.value,

    queries,
    allQueries,
    selectedQuery,
    teams,
    selectedTeamId,
    parseQueryContent,
    hasTeams,
    hasQueries,
    selectedTeam,

    fetchUserTeams,
    setSelectedTeam,
    list,
    listAll,
    fetchById,
    create,
    update,
    remove,
    resetState,

    isLoadingOperation: state.isLoadingOperation,
  };
});
