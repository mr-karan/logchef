import { ref, computed, type Ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useExploreStore } from '@/stores/explore'
import { useSavedQueriesStore } from '@/stores/savedQueries'
import { useContextStore } from '@/stores/context'
import { useAuthStore } from '@/stores/auth';
import { useVariableStore } from '@/stores/variables'
import type { VariableState } from '@/stores/variables'
import { useToast } from '@/composables/useToast'
import { TOAST_DURATION } from '@/lib/constants'
import { getErrorMessage } from '@/api/types'
import type { SaveQueryFormData } from '@/views/explore/types'
import { savedQueriesApi, type SavedQuery } from '@/api/savedQueries'
import { getLocalTimeZone, CalendarDateTime, type DateValue } from '@internationalized/date'
import type { Source } from "@/api/sources";

function calendarDateTimeToTimestamp(dateTime: DateValue | null | undefined): number | null {
  if (!dateTime) return null;
  try {
    const date = dateTime.toDate(getLocalTimeZone());
    return date.getTime();
  } catch (e) {
    console.error("Error converting DateValue to timestamp:", e);
    return null;
  }
}

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
  const contextStore = useContextStore()
  const authStore = useAuthStore();
  const variableStore = useVariableStore();
  const { toast } = useToast()

  const showSaveQueryModal = ref(false)
  const editingQuery = ref<SavedQuery | null>(null)
  const isLoading = ref(false)
  const isLoadingQueryDetails = ref(false)
  const openingQueryId = ref<number | null>(null)
  const searchQuery = ref('')

  const isEditingExistingQuery = computed(() => !!route.query.id);

  // Saved queries are no longer team-scoped. Anyone authenticated can attempt
  // to save against a source they have access to; the backend gates by source
  // access. Per-row edit/delete authorization lives in canEditQuery() below.
  const canManageCollections = computed(() => authStore.isAuthenticated);

  function canEditQuery(query: SavedQuery | null | undefined): boolean {
    if (!query || !authStore.user) return false;
    if (authStore.user.role === 'admin') return true;
    return query.created_by != null && String(query.created_by) === String(authStore.user.id);
  }

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

  async function handleSaveQueryClick() {
    const query = exploreStore.activeMode === 'logchefql'
        ? exploreStore.logchefqlCode
        : exploreStore.rawSql

    if (!query?.trim()) {
      toast({
        title: 'Cannot Add to Collection',
        variant: 'destructive',
        description: 'Query is empty. Please enter a query to save.',
        duration: TOAST_DURATION.WARNING
      })
      return
    }

    const queryId = route.query.id
    if (queryId) {
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

  async function handleSaveQuery(formData: SaveQueryFormData) {
    try {
      let response;

      const queryIdFromUrl = route.query.id as string | undefined;
      const isUpdate = !!editingQuery.value || !!queryIdFromUrl;
      const queryId = editingQuery.value?.id.toString() || queryIdFromUrl;

      if (!formData.source_id) {
        throw new Error("Missing source ID for save/update operation");
      }

      if (isUpdate && queryId) {
        response = await savedQueriesStore.update(queryId, {
          name: formData.name,
          description: formData.description,
          query_type: formData.query_type,
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
            query_type: formData.query_type,
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
              formData.query_type,
          );
        }
      }

      if (response && response.success) {
        showSaveQueryModal.value = false;
        editingQuery.value = null;

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
      const isLogchefQL = queryData.query_type === 'logchefql'
      const queryToLoad = content.content || ''

      exploreStore.clearError()
      exploreStore.setActiveMode(isLogchefQL ? 'logchefql' : 'sql')

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
        exploreStore.setRawSql(queryToLoad)
      }

      if (content.limit) exploreStore.setLimit(content.limit)

      if (content.timeRange === null) {
        // Saved query has timeRange explicitly set to null — keep current range.
      } else if (content.timeRange?.relative) {
        exploreStore.setRelativeTimeRange(content.timeRange.relative);
      } else if (content.timeRange?.absolute?.start && content.timeRange?.absolute?.end) {
        try {
          const startDate = new Date(content.timeRange.absolute.start);
          const endDate = new Date(content.timeRange.absolute.end);

          if (!isNaN(startDate.getTime()) && !isNaN(endDate.getTime())) {
            const startDateTime = new CalendarDateTime(
                startDate.getFullYear(),
                startDate.getMonth() + 1,
                startDate.getDate(),
                startDate.getHours(),
                startDate.getMinutes(),
                startDate.getSeconds()
            );

            const endDateTime = new CalendarDateTime(
                endDate.getFullYear(),
                endDate.getMonth() + 1,
                endDate.getDate(),
                endDate.getHours(),
                endDate.getMinutes(),
                endDate.getSeconds()
            );

            exploreStore.setTimeConfiguration({
              absoluteRange: {
                start: startDateTime,
                end: endDateTime
              }
            });
          }
        } catch (error) {
          console.error("Error converting timestamps to CalendarDateTime:", error);
        }
      }

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
  async function loadSourceQueries(sourceId: number) {
    try {
      isLoading.value = true
      searchQuery.value = ''

      if (!sourceId) {
        queriesRef.value = []
        return { success: false, error: 'No source ID provided' }
      }

      const result = await savedQueriesStore.list(sourceId)

      if (result.success) {
        queriesRef.value = result.data ?? []
        return { success: true, data: result.data }
      }

      queriesRef.value = []
      if (result.error) {
        toast({
          title: 'Error',
          description: result.error.message,
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        })
      }
      return { success: false, error: result.error }
    } catch (error) {
      queriesRef.value = []
      toast({
        title: 'Error',
        description: getErrorMessage(error),
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      })
      return { success: false, error }
    } finally {
      isLoading.value = false
    }
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

    const startTime = calendarDateTimeToTimestamp(exploreStore.timeRange?.start);
    const endTime = calendarDateTimeToTimestamp(exploreStore.timeRange?.end);
    if (startTime !== null && endTime !== null) {
      newQuery.start_time = startTime.toString();
      newQuery.end_time = endTime.toString();
    }

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
        query_type: 'logchefql' | 'sql';
      }
  ) {
    isLoading.value = true;
    try {
      const result = await savedQueriesStore.update(queryId, {
        name: updateData.name,
        description: updateData.description,
        query_content: updateData.query_content,
        query_type: updateData.query_type,
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
    canManageCollections,
    canEditQuery,

    handleSaveQueryClick,
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
