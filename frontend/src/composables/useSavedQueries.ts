import { ref, computed, watch, type Ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useExploreStore } from '@/stores/explore'
import { useSavedQueriesStore } from '@/stores/savedQueries'
import { useCollectionsStore } from '@/stores/collections'
import { useContextStore } from '@/stores/context'
import { useVariableStore } from '@/stores/variables'
import type { VariableState } from '@/stores/variables'
import { useTeamPermissions } from '@/composables/useTeamPermissions'
import { useToast } from '@/composables/useToast'
import { TOAST_DURATION } from '@/lib/constants'
import { getErrorMessage, type APIErrorResponse } from '@/api/types'
import type { SaveQueryFormData } from '@/views/explore/types'
import { savedQueriesApi, type SavedQuery } from '@/api/savedQueries'
import { decodeStoredRange, encodeWindowForUrl } from '@/utils/timeWindowCodec'
import type { Source } from "@/api/sources";
import { getExploreModeForQueryLanguage, resolveSavedQueryMetadata } from "@/lib/queryMetadata";

export function useSavedQueries(
    queries?: Ref<SavedQuery[] | undefined>,
    _currentSource?: Ref<Source | undefined>
) {
  const localQueries = ref<SavedQuery[]>([]);
  const queriesRef = queries || localQueries;
  const router = useRouter()
  const route = useRoute()
  const exploreStore = useExploreStore()
  const savedQueriesStore = useSavedQueriesStore()
  const collectionsStore = useCollectionsStore()
  const contextStore = useContextStore()
  const variableStore = useVariableStore();
  const { toast } = useToast()

  const showSaveQueryModal = ref(false)
  const editingQuery = ref<SavedQuery | null>(null)
  const isLoading = ref(false)
  const isLoadingQueryDetails = ref(false)
  const openingQueryId = ref<number | null>(null)
  const searchQuery = ref('')
  const saveAsNewQuery = ref(false)
  let sourceQueryVersion = 0
  let inFlightSourceQueries: {
    sourceId: number;
    promise: Promise<{ success: boolean; data?: SavedQuery[] | null; error?: APIErrorResponse | string | null }>;
  } | null = null

  watch(
    [() => contextStore.teamId, () => contextStore.sourceId],
    () => {
      sourceQueryVersion++
      inFlightSourceQueries = null
      queriesRef.value = []
      isLoading.value = false
    },
    { flush: 'sync' },
  )

  const isEditingExistingQuery = computed(() => !!route.query.id);

  // Role gates delegate to useTeamPermissions so the matrix stays
  // consistent with the backend role contract.
  const { canSaveQuery, canEditSavedQuery, isAnyTeamCollectionMutator } = useTeamPermissions();
  const canEditQuery = canEditSavedQuery;

  const filteredQueries = computed(() => {
    if (!searchQuery.value.trim()) {
      return queriesRef.value;
    }
    const search = searchQuery.value.toLowerCase();
    return queriesRef.value?.filter(query =>
        query.name.toLowerCase().includes(search) ||
        (query.description && query.description.toLowerCase().includes(search))
    );
  });

  const hasQueries = computed(() => {
    return filteredQueries.value ? filteredQueries.value.length > 0 : false;
  });

  const totalQueryCount = computed(() => {
    return queriesRef.value ? queriesRef.value.length : 0;
  });

  function clearSearch() {
    searchQuery.value = ''
  }

  async function handleSaveQueryClick(options: { asNew?: boolean } = {}) {
    saveAsNewQuery.value = options.asNew === true
    const query = exploreStore.activeMode === 'logchefql'
        ? exploreStore.logchefqlCode
        : exploreStore.nativeQuery

    if (!query?.trim()) {
      saveAsNewQuery.value = false
      toast({
        title: 'Cannot Add to Collection',
        variant: 'destructive',
        description: 'Query is empty. Please enter a query to save.',
        duration: TOAST_DURATION.WARNING
      })
      return
    }

    const queryId = route.query.id
    if (queryId && !saveAsNewQuery.value) {
      try {
        isLoadingQueryDetails.value = true
        const result = await savedQueriesApi.get(queryId as string);
        if (result.data) {
          editingQuery.value = result.data;
          showSaveQueryModal.value = true;
        } else {
          throw new Error(`Query details for ID ${queryId} not found.`);
        }
      } catch (error) {
        console.error('Error loading query details:', error)
        toast({
          title: 'Error',
          description: getErrorMessage(error),
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR
        })
      } finally {
        isLoadingQueryDetails.value = false
      }
    } else {
      editingQuery.value = null
      showSaveQueryModal.value = true
    }
  }

  function handleSaveAsNewQueryClick() {
    return handleSaveQueryClick({ asNew: true })
  }

  function closeSaveQueryModal() {
    showSaveQueryModal.value = false
    editingQuery.value = null
    saveAsNewQuery.value = false
  }

  async function handleSaveQuery(formData: SaveQueryFormData) {
    try {
      let response;

      const queryIdFromUrl = route.query.id as string | undefined;
      const isUpdate = !saveAsNewQuery.value && (!!editingQuery.value || !!queryIdFromUrl);
      const queryId = editingQuery.value?.id.toString() || queryIdFromUrl;

      if (!formData.source_id) {
        throw new Error("Missing source ID for save/update operation");
      }

      if (isUpdate && queryId) {
        response = await savedQueriesStore.update(queryId, {
          name: formData.name,
          description: formData.description,
          query_language: formData.query_language,
          editor_mode: formData.editor_mode,
          query_content: formData.query_content,
        });
      } else {
        const existingQueries = savedQueriesStore.data.queries || [];
        const existingQuery = existingQueries.find(q =>
            q.name === formData.name &&
            q.source_id === formData.source_id
        );

        if (existingQuery) {
          const confirmOverwrite = window.confirm(
              `A query named "${formData.name}" already exists for this source. Do you want to overwrite it?`
          );
          if (!confirmOverwrite) {
            return { success: false, canceled: true };
          }
          response = await savedQueriesStore.update(existingQuery.id, {
            name: formData.name,
            description: formData.description,
            query_language: formData.query_language,
            editor_mode: formData.editor_mode,
            query_content: formData.query_content,
          });
        } else {
          let parsedContent;
          try {
            parsedContent = JSON.parse(formData.query_content);
          } catch (e) {
            console.error("Failed to parse formData.query_content before create:", e);
            throw new Error("Invalid query content format for create operation");
          }

          response = await savedQueriesStore.create(
              formData.source_id,
              formData.created_from_team_id,
              formData.name,
              formData.description,
              parsedContent,
              formData.query_language,
              formData.editor_mode,
          );
        }
      }

      if (response && response.success) {
        showSaveQueryModal.value = false;
        editingQuery.value = null;
        saveAsNewQuery.value = false;

        const savedQueryName = formData.name;
        if (savedQueryName) {
          exploreStore.setActiveSavedQueryName(savedQueryName);
        }

        if (response.data && response.data.id) {
          exploreStore.setSelectedQueryId(response.data.id.toString());

          const next = { ...route.query };
          next.source = formData.source_id.toString();
          next.id = response.data.id.toString();
          router.replace({ query: next });
        }

        if (formData.source_id) {
          await loadSourceQueries(formData.source_id);
        }

        // Pin the query to the chosen collection in the same step (inline save).
        // Best-effort: the query is already saved, so a pin failure shouldn't
        // surface as a save failure — addItem shows its own error toast.
        if (formData.collection_id && response.data?.id) {
          await collectionsStore.addItem(formData.collection_id, { saved_query_id: response.data.id }, { notify: false });
        }
        return { success: true, data: response.data };
      } else if (response) {
        throw new Error(getErrorMessage(response.error) || 'Failed to save query');
      } else {
        return { success: false };
      }
    } catch (error) {
      console.error("Error saving query:", error);
      toast({
        title: 'Error',
        description: getErrorMessage(error),
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      });
      return { success: false, error };
    }
  }

  async function loadSavedQuery(queryData: SavedQuery) {
    if (!queryData?.query_content || !queryData?.id) {
      toast({
        title: 'Error',
        description: 'Invalid saved query data.',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      })
      return false
    }

    try {
      const content = JSON.parse(queryData.query_content)
      const metadata = resolveSavedQueryMetadata({
        query_language: queryData.query_language,
        editor_mode: queryData.editor_mode,
        source_type: _currentSource?.value?.source_type ?? (queryData.query_language === "logsql" ? "victorialogs" : "clickhouse"),
        query_languages: _currentSource?.value?.query_languages,
        saved_query_editor_modes: _currentSource?.value?.saved_query_editor_modes,
      })
      const isLogchefQL = metadata.queryLanguage === 'logchefql'
      const queryToLoad = content.content || ''

      exploreStore.clearError()

      // Set the correct mode based on the saved query language
      exploreStore.setActiveMode(getExploreModeForQueryLanguage(metadata.queryLanguage))

      const resolvedTeamId = 'resolved_team_id' in queryData
        ? Number((queryData as SavedQuery & { resolved_team_id?: number }).resolved_team_id)
        : null;

      if (resolvedTeamId && resolvedTeamId !== contextStore.teamId) {
        contextStore.selectTeam(resolvedTeamId);
      }

      if (queryData.source_id && queryData.source_id !== contextStore.sourceId) {
        exploreStore.suppressNextSourceReset(queryData.source_id);
        contextStore.selectSource(queryData.source_id);
      }

      if (isLogchefQL) {
        exploreStore.setLogchefqlCode(queryToLoad)
      } else {
        exploreStore.setNativeQuery(queryToLoad, metadata.queryLanguage)
      }

      if (content.limit) exploreStore.setLimit(content.limit)

      // A saved query without a time range keeps the current one.
      const window = decodeStoredRange(content.timeRange, {
        format: 'saved-query',
        version: typeof content.version === 'number' ? content.version : 1,
        prefZone: exploreStore.resolved.zone,
      })
      if (window) exploreStore.setWindow(window)

      if (Array.isArray(content.variables)) {
        try {
          const normalizedVariables = (content.variables as VariableState[]).map((variable) => {
            const hasValue = variable.value !== '' && variable.value !== null && variable.value !== undefined;
            if (!hasValue && variable.defaultValue !== undefined && variable.defaultValue !== null && variable.defaultValue !== '') {
              return { ...variable, value: variable.defaultValue };
            }
            return variable;
          });
          variableStore.setAllVariable(normalizedVariables);
        } catch (e) {
          console.error("Failed to restore variables from saved query:", e);
        }
      }

      exploreStore.setSelectedQueryId(queryData.id.toString());
      if (queryData.name) {
        exploreStore.setActiveSavedQueryName(queryData.name);
      }

      // Only include resolved execution context + id; don't carry forward stale
      // limit/time/mode params from the previous explorer state.
      const queryParams: Record<string, string> = {
        ...(resolvedTeamId ? { team: resolvedTeamId.toString() } : {}),
        source: queryData.source_id.toString(),
        id: queryData.id.toString(),
      };

      const currentId = route.query.id as string | undefined;
      if (
        currentId !== queryData.id.toString() ||
        route.query.source !== queryParams.source ||
        (queryParams.team && route.query.team !== queryParams.team)
      ) {
        router.replace({ path: '/logs/explore', query: queryParams });
      }

      return true
    } catch (error) {
      console.error('Error loading saved query:', error)
      exploreStore.setActiveSavedQueryName(null);
      exploreStore.setSelectedQueryId(null);

      toast({
        title: 'Error',
        description: getErrorMessage(error),
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      })
      return false
    }
  }

  function getQueryUrl(query: SavedQuery): string {
    return `/logs/saved/${query.id}`
  }

  async function openQuery(query: SavedQuery) {
    if (openingQueryId.value !== null) {
      return
    }

    openingQueryId.value = query.id

    try {
      await router.push({
        path: `/logs/saved/${query.id}`,
        query: {},
      })
    } catch (error: unknown) {
      const err = error as { name?: string }
      const isExpectedNavigationError = err?.name === 'NavigationDuplicated' || err?.name === 'NavigationCancelled'
      if (!isExpectedNavigationError) {
        console.error('Error navigating to query:', error)
        toast({
          title: 'Navigation Error',
          description: 'Failed to open the query. Please try again.',
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        })
      }
    } finally {
      openingQueryId.value = null
    }
  }

  function editQuery(query: SavedQuery) {
    try {
      editingQuery.value = JSON.parse(JSON.stringify(query))
      showSaveQueryModal.value = true
    } catch (error) {
      console.error('Error preparing query for edit:', error)
      toast({
        title: 'Error',
        description: 'Failed to prepare query for editing. Please try again.',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      })
    }
  }

  async function deleteQuery(query: SavedQuery) {
    if (window.confirm(`Are you sure you want to delete "${query.name}"? This action cannot be undone.`)) {
      try {
        await savedQueriesStore.remove(query.id)

        if (exploreStore.selectedQueryId === query.id.toString()) {
          exploreStore.setActiveSavedQueryName(null);
          exploreStore.setSelectedQueryId(null);

          if (route.query.id) {
            const currentQuery = { ...route.query };
            delete currentQuery.id;
            router.replace({ query: currentQuery });
          }
        }

        return { success: true }
      } catch (error) {
        toast({
          title: 'Error',
          description: getErrorMessage(error),
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        })
        return { success: false, error }
      }
    }
    return { success: false, canceled: true }
  }

  // loadSourceQueries fetches saved queries for a single source. teamId is no
  // longer needed — visibility is gated by source access via any team membership.
  function loadSourceQueries(sourceId: number) {
    if (inFlightSourceQueries?.sourceId === sourceId) return inFlightSourceQueries.promise
    const version = ++sourceQueryVersion
    isLoading.value = true
    searchQuery.value = ''
    const promise = (async () => {
      if (!sourceId) {
        queriesRef.value = []
        isLoading.value = false
        return { success: false, error: 'No source ID provided' }
      }

      const result = await savedQueriesStore.list(sourceId)
      if (version !== sourceQueryVersion) return { success: false }

      isLoading.value = false
      queriesRef.value = result.success ? result.data ?? [] : []
      if (result.error) {
        toast({
          title: 'Error',
          description: result.error.message,
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        })
      }
      return result
    })().finally(() => {
      if (inFlightSourceQueries?.promise === promise) inFlightSourceQueries = null
    })
    inFlightSourceQueries = { sourceId, promise }
    return promise
  }

  function createNewQuery(sourceId?: number) {
    exploreStore.resetQueryToDefaults();

    const newQuery: Record<string, string> = {};
    if (route.query.team) {
      newQuery.team = route.query.team as string;
    }
    if (sourceId) {
      newQuery.source = sourceId.toString();
    } else if (route.query.source) {
      newQuery.source = route.query.source as string;
    }

    newQuery.limit = exploreStore.limit.toString();

    Object.assign(newQuery, encodeWindowForUrl(exploreStore.window));

    newQuery.mode = exploreStore.activeMode;

    return router.push({
      path: '/logs/explore',
      query: newQuery
    });
  }

  // updateSavedQuery wraps the saved-queries store action used by edit dialogs.
  async function updateSavedQuery(
      queryId: string | number,
      updateData: {
        name?: string;
        description?: string;
        query_content: string;
        query_language: 'logchefql' | 'clickhouse-sql' | 'logsql';
        editor_mode: 'builder' | 'native';
      }
  ) {
    isLoading.value = true;
    try {
      const result = await savedQueriesStore.update(queryId, {
        name: updateData.name,
        description: updateData.description,
        query_content: updateData.query_content,
        query_language: updateData.query_language,
        editor_mode: updateData.editor_mode,
      });

      if (result.success) {
        return { success: true, data: result.data };
      }
      throw new Error(result.error?.message || 'Failed to update query');
    } catch (error) {
      console.error(`Error updating saved query ${queryId}:`, error);
      toast({
        title: 'Update Failed',
        description: getErrorMessage(error),
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      });
      throw error;
    } finally {
      isLoading.value = false;
    }
  }

  return {
    showSaveQueryModal,
    editingQuery,
    isLoading,
    isLoadingQueryDetails,
    openingQueryId,
    queries: queriesRef,
    filteredQueries,
    hasQueries,
    totalQueryCount,
    searchQuery,
    isEditingExistingQuery,
    canSaveQuery,
    canEditQuery,
    isAnyTeamCollectionMutator,

    handleSaveQueryClick,
    handleSaveAsNewQueryClick,
    closeSaveQueryModal,
    handleSaveQuery,
    loadSavedQuery,
    updateSavedQuery,
    loadSourceQueries,
    getQueryUrl,
    openQuery,
    editQuery,
    deleteQuery,
    createNewQuery,
    clearSearch,
  }
}
