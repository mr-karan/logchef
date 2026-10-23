import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useExploreStore } from '@/stores/explore'
import { useSavedQueriesStore } from '@/stores/savedQueries'
import { useCollectionsStore } from '@/stores/collections'
import { useContextStore } from '@/stores/context'
import { useTeamPermissions } from '@/composables/useTeamPermissions'
import { useToast } from '@/composables/useToast'
import { TOAST_DURATION } from '@/lib/constants'
import { getErrorMessage } from '@/api/types'
import type { SaveQueryFormData } from '@/views/explore/types'
import { savedQueriesApi, type SavedQuery } from '@/api/savedQueries'

export function useSavedQueries() {
  const router = useRouter()
  const exploreStore = useExploreStore()
  const savedQueriesStore = useSavedQueriesStore()
  const collectionsStore = useCollectionsStore()
  const contextStore = useContextStore()
  const { toast } = useToast()

  const showSaveQueryModal = ref(false)
  const editingQuery = ref<SavedQuery | null>(null)
  const isUpdating = ref(false)
  const isLoadingQueryDetails = ref(false)
  const openingQueryId = ref<number | null>(null)
  const searchQuery = ref('')
  const saveAsNewQuery = ref(false)
  const queriesRef = computed(() => savedQueriesStore.queries)
  const isLoading = computed(() =>
    isUpdating.value || savedQueriesStore.isLoadingOperation(`listSavedQueries-${contextStore.sourceId}`))

  const isEditingExistingQuery = computed(() => !!exploreStore.selectedQueryId);

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

    const queryId = exploreStore.selectedQueryId
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

  /** Saves the form's query and returns it, or null when canceled or failed. The caller marks Explorer with it. */
  async function handleSaveQuery(formData: SaveQueryFormData): Promise<SavedQuery | null> {
    try {
      let response;

      const openQueryId = exploreStore.selectedQueryId;
      const isUpdate = !saveAsNewQuery.value && (!!editingQuery.value || !!openQueryId);
      const queryId = editingQuery.value?.id.toString() || openQueryId;

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
            return null;
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

        if (formData.source_id) {
          await loadSourceQueries(formData.source_id);
        }

        // Pin the query to the chosen collection in the same step (inline save).
        // Best-effort: the query is already saved, so a pin failure shouldn't
        // surface as a save failure — addItem shows its own error toast.
        if (formData.collection_id && response.data?.id) {
          await collectionsStore.addItem(formData.collection_id, { saved_query_id: response.data.id }, { notify: false });
        }
        return response.data ?? null;
      } else if (response) {
        throw new Error(getErrorMessage(response.error) || 'Failed to save query');
      } else {
        return null;
      }
    } catch (error) {
      console.error("Error saving query:", error);
      toast({
        title: 'Error',
        description: getErrorMessage(error),
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      });
      return null;
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

  // loadSourceQueries fetches saved queries for a single source. teamId is no
  // longer needed — visibility is gated by source access via any team membership.
  async function loadSourceQueries(sourceId: number) {
    searchQuery.value = ''
    if (!sourceId) return { success: false, error: 'No source ID provided' }

    const result = await savedQueriesStore.list(sourceId)
    if (result.error && contextStore.sourceId === sourceId) {
      toast({
        title: 'Error',
        description: result.error.message,
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      })
    }
    return result
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
    isUpdating.value = true;
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
      isUpdating.value = false;
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
    updateSavedQuery,
    loadSourceQueries,
    getQueryUrl,
    openQuery,
    editQuery,
    clearSearch,
  }
}
