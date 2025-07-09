import { ref, computed, type Ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useExploreStore } from '@/stores/explore'
import { useSavedQueriesStore } from '@/stores/savedQueries'
import { useAuthStore } from '@/stores/auth';
import { useTeamsStore } from '@/stores/teams'; // Corrected path
import { useVariableStore } from '@/stores/variables'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import { getErrorMessage } from '@/api/types'
import type { SaveQueryFormData } from '@/views/explore/types'
import type { SavedTeamQuery } from '@/api/savedQueries'
import type { TeamMember } from '@/api/teams'; // Import TeamMember type
import { CalendarDateTime, getLocalTimeZone, type DateValue } from '@internationalized/date'
import type { Source } from "@/api/sources";

// Add this helper function before the useSavedQueries function definition
function calendarDateTimeToTimestamp(dateTime: DateValue | null | undefined): number | null {
  if (!dateTime) return null;
  try {
    // Convert DateValue to JS Date object using the local timezone
    const date = dateTime.toDate(getLocalTimeZone());
    return date.getTime();
  } catch (e) {
    console.error("Error converting DateValue to timestamp:", e);
    return null;
  }
}

export function useSavedQueries(
    queries?: Ref<SavedTeamQuery[] | undefined>,
    currentSource?: Ref<Source | undefined>
) {
  // Create a local queries ref if none is provided
  const localQueries = ref<SavedTeamQuery[]>([]);
  // Use provided queries ref or fall back to local one
  const queriesRef = queries || localQueries;
  const router = useRouter()
  const route = useRoute()
  const exploreStore = useExploreStore()
  const savedQueriesStore = useSavedQueriesStore()
  const authStore = useAuthStore();
  const teamsStore = useTeamsStore();
  const variableStore = useVariableStore();
  const { toast } = useToast()

  const showSaveQueryModal = ref(false)
  const editingQuery = ref<SavedTeamQuery | null>(null)
  const isLoading = ref(false)
  const isLoadingQueryDetails = ref(false)
  const searchQuery = ref('')

  const isEditingExistingQuery = computed(() => !!route.query.collection_id);

  const canManageCollections = computed(() => {
    if (!authStore.isAuthenticated || !authStore.user) {
      return false;
    }
    // Global admins can always manage collections
    if (authStore.user.role === "admin") {
      return true;
    }

    const teamIdParam = route.query.team;
    if (!teamIdParam) {
      // If no team context, disallow (or decide default behavior)
      return false;
    }
    const teamId = Number(teamIdParam);
    if (isNaN(teamId)) {
      return false;
    }

    // Use the new getter from teamsStore
    const userRoleInTeam = teamsStore.getUserRoleInTeam(teamId);

    // Allow if user is team admin or team editor for the current team
    return userRoleInTeam === "admin" || userRoleInTeam === "editor";
  });

  // This is the primary computed property for displaying queries after filtering.
  // It uses queriesRef (which is either the passed in queries or our local fallback)
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

  // Has queries computed property, uses the above filteredQueries
  const hasQueries = computed(() => {
    // Ensure filteredQueries.value exists before accessing its length
    return filteredQueries.value ? filteredQueries.value.length > 0 : false;
  });

  // Total query count
  const totalQueryCount = computed(() => {
    // Ensure queriesRef.value exists before accessing its length
    return queriesRef.value ? queriesRef.value.length : 0;
  });

  // Clear search function
  function clearSearch() {
    searchQuery.value = ''
  }

  // Save query modal trigger
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

    // Check if we have a query_id in the URL, which means we're editing an existing query
    const queryId = route.query.query_id
    if (queryId) {
      // We are editing an existing query - load the query details
      const teamId = route.query.team as string
      const sourceId = route.query.source as string

      if (!teamId || !sourceId) {
        toast({
          title: 'Error',
          description: 'Missing team or source ID in URL.',
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR
        })
        return
      }

      try {
        isLoadingQueryDetails.value = true
        // Fetch query details from the backend
        const result = await savedQueriesStore.fetchTeamSourceQueries(
            parseInt(teamId),
            parseInt(sourceId)
        )

        if (result.success && savedQueriesStore.queries) {
          // Find the specific query from the results (or maybe the store state?)
          const foundQuery = savedQueriesStore.queries.find(q => q.id.toString() === queryId);
          if (foundQuery) {
            editingQuery.value = foundQuery;
            showSaveQueryModal.value = true;
          } else {
            // If not found after fetch, maybe it was deleted? Or fetch didn't return it?
            throw new Error(`Query details for ID ${queryId} not found after fetch.`);
          }
        } else {
          throw new Error(result.error?.message || 'Failed to load query details')
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
      // We're creating a new query
      editingQuery.value = null
      showSaveQueryModal.value = true
    }
  }

  // Handle actual saving
  async function handleSaveQuery(formData: SaveQueryFormData) {
    try {
      let response;

      // Check if we're updating an existing query from the URL or editingQuery state
      const queryIdFromUrl = route.query.query_id as string | undefined;
      const isUpdate = !!editingQuery.value || !!queryIdFromUrl;
      const queryId = editingQuery.value?.id.toString() || queryIdFromUrl;

      // Ensure team ID is present
      if (!formData.team_id) {
        throw new Error("Missing team ID for save/update operation");
      }

      if (isUpdate && queryId) {
        // Ensure source ID is present for update
        if (!formData.source_id) {
          throw new Error("Missing source ID for update operation");
        }

        // Use the correct store action for updates
        console.log(`useSavedQueries.handleSaveQuery: Updating query ${queryId} for team ${formData.team_id}, source ${formData.source_id}`);
        response = await savedQueriesStore.updateTeamSourceQuery(
            formData.team_id,
            formData.source_id, // Pass source ID
            queryId,
            {
              // Payload includes only relevant fields for updateTeamSourceQuery
              name: formData.name,
              description: formData.description,
              query_type: formData.query_type,
              query_content: formData.query_content
            }
        );
        console.log('Updated query via updateTeamSourceQuery:', response);

      } else {
        // --- Create or Overwrite Flow ---
        // Ensure source ID is present for create/overwrite
        if (!formData.source_id) {
          throw new Error("Missing source ID for create/overwrite operation");
        }

        // Check for existing query by name/team/source (potential overwrite)
        const existingQueries = savedQueriesStore.data.queries || [];
        const existingQuery = existingQueries.find(q =>
            q.name === formData.name &&
            q.team_id === formData.team_id &&
            q.source_id === formData.source_id
        );

        if (existingQuery) {
          // Ask for confirmation before overwriting
          const confirmOverwrite = window.confirm(
              `A query named "${formData.name}" already exists for this source. Do you want to overwrite it?`
          );

          if (confirmOverwrite) {
            // Overwrite existing using updateTeamSourceQuery
            console.log(`useSavedQueries.handleSaveQuery: Overwriting query ${existingQuery.id} for team ${formData.team_id}, source ${formData.source_id}`);
            response = await savedQueriesStore.updateTeamSourceQuery(
                formData.team_id,
                formData.source_id,
                existingQuery.id.toString(),
                {
                  name: formData.name,
                  description: formData.description,
                  query_type: formData.query_type,
                  query_content: formData.query_content
                }
            );
            console.log('Overwrote existing query via updateTeamSourceQuery:', response);
          } else {
            // User cancelled the overwrite
            return { success: false, canceled: true }; // Return indication that nothing happened
          }
        } else {
          // Create new query using createSourceQuery
          console.log(`useSavedQueries.handleSaveQuery: Creating new query for team ${formData.team_id}, source ${formData.source_id}`);
          // The createSourceQuery action internally stringifies the content
          // We need to parse the formData.query_content first if it's a string here
          let parsedContent;
          try {
            parsedContent = JSON.parse(formData.query_content);
          } catch (e) {
            console.error("Failed to parse formData.query_content before create:", e);
            throw new Error("Invalid query content format for create operation");
          }

          response = await savedQueriesStore.createSourceQuery(
              formData.team_id,
              formData.source_id,
              formData.name,
              formData.description,
              parsedContent, // Pass the parsed content object
              formData.query_type // Add the query_type parameter
          );
          console.log('Created new query via createSourceQuery:', response);
        }
      }

      if (response && response.success) {
        showSaveQueryModal.value = false;
        editingQuery.value = null; // Clear editing state

        // Set the active query name for new or updated query
        const savedQueryName = formData.name;
        if (savedQueryName) {
          exploreStore.setActiveSavedQueryName(savedQueryName);
        }

        // Add this: Set the selectedQueryId in the store
        if (response.data && response.data.id) {
          // Save the query ID to the store
          exploreStore.setSelectedQueryId(response.data.id.toString());

          // Update URL with the new query_id
          const currentQuery = { ...route.query };
          currentQuery.query_id = response.data.id.toString();
          router.replace({ query: currentQuery });
        }

        // Ensure queries are refreshed for the current source
        if (formData.team_id && formData.source_id) {
          await loadSourceQueries(formData.team_id, formData.source_id);
        }

        toast({
          title: 'Success',
          description: isUpdate ? 'Query updated successfully.' : 'Query saved successfully.',
          duration: TOAST_DURATION.SUCCESS
        });

        // Only clear query_id from URL if we were editing and now want to create a new one
        // NOT when we just created a new query
        if (queryIdFromUrl && response.data && response.data.id && queryIdFromUrl !== response.data.id.toString()) {
          const currentQuery = { ...route.query };
          // Update to the new query_id instead of deleting it
          currentQuery.query_id = response.data.id.toString();
          router.replace({ query: currentQuery });
        }
        return { success: true, data: response.data }; // Return success state
      } else if (response) {
        // Handle failure from the store action
        throw new Error(getErrorMessage(response.error) || 'Failed to save query');
      } else {
        // Handle case where no action was taken (e.g., overwrite cancelled)
        return { success: false }; // Indicate no successful action occurred
      }
    } catch (error) {
      console.error("Error saving query:", error);
      toast({
        title: 'Error',
        description: getErrorMessage(error),
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      });
      return { success: false, error }; // Return error state
    }
  }

  // Load saved query
  async function loadSavedQuery(queryData: SavedTeamQuery) {
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

      // Reset state
      exploreStore.clearError()

      // Set the correct mode based on the saved query type
      exploreStore.setActiveMode(isLogchefQL ? 'logchefql' : 'sql')

      // Set content
      if (isLogchefQL) {
        exploreStore.setLogchefqlCode(queryToLoad)
      } else {
        exploreStore.setRawSql(queryToLoad)
      }

      // Set limit if available
      if (content.limit) exploreStore.setLimit(content.limit)

      // Check if timeRange is explicitly null - this means to keep the current time range
      if (content.timeRange === null) {
        console.log("Saved query has timeRange explicitly set to null, keeping current range");
        // Keep the current time range from the store
      }
      // Set time range from saved query if available and valid
      else if (content.timeRange &&
          content.timeRange.absolute &&
          content.timeRange.absolute.start &&
          content.timeRange.absolute.end) {
        console.log("Setting time range from saved query:", content.timeRange);

        // Convert timestamps to CalendarDateTime objects
        try {
          const startDate = new Date(content.timeRange.absolute.start);
          const endDate = new Date(content.timeRange.absolute.end);

          if (!isNaN(startDate.getTime()) && !isNaN(endDate.getTime())) {
            // Create CalendarDateTime objects
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

            // Set the time range in the store
            exploreStore.setTimeConfiguration({
              absoluteRange: {
                start: startDateTime,
                end: endDateTime
              }
            });
          } else {
            console.warn("Invalid timestamp in saved query timeRange, keeping current range");
          }
        } catch (error) {
          console.error("Error converting timestamps to CalendarDateTime:", error);
        }
      } else {
        console.log("Saved query has no time range specified or it's invalid, keeping current range");
        // Keep existing time range from the store
      }

      // save variable data into store.
      if (Array.isArray(content.variables)) {
        try {
          variableStore.setAllVariable(content.variables);
          console.log("Restored variables from saved query.");
        } catch (e) {
          console.error("Failed to restore variables from saved query:", e);
        }
      } else {
        console.warn("No valid variables found in saved query.");
      }


      // Set the selected query ID in the store
      exploreStore.setSelectedQueryId(queryData.id.toString());

      // Set the active saved query name in the store
      if (queryData.name) {
        exploreStore.setActiveSavedQueryName(queryData.name);
      }

      // CENTRALIZED URL HANDLING: Create URL query parameters directly
      // This ensures consistency between dropdown and saved queries view
      const queryParams = { ...route.query }; // Start with current params

      // Always include these critical parameters for proper state tracking
      queryParams.team = queryData.team_id.toString();
      queryParams.source = queryData.source_id.toString();
      queryParams.query_id = queryData.id.toString(); // Most important - makes "New Query" button appear

      // Set time range params from the current exploreStore state (after we've updated it)
      const startTime = calendarDateTimeToTimestamp(exploreStore.timeRange?.start);
      const endTime = calendarDateTimeToTimestamp(exploreStore.timeRange?.end);
      if (startTime !== null && endTime !== null) {
        queryParams.start_time = startTime.toString();
        queryParams.end_time = endTime.toString();
      }

      // Set limit from current store state
      queryParams.limit = exploreStore.limit.toString();

      // Set mode and query content
      queryParams.mode = isLogchefQL ? 'logchefql' : 'sql';
      if (queryToLoad) {
        queryParams.q = encodeURIComponent(queryToLoad);
      }

      // Update URL with complete state (replaces syncUrlFromState call)
      console.log("Updating URL with saved query state, including query_id:", queryData.id.toString());
      router.replace({ query: queryParams });

      // toast({
      //   title: 'Success',
      //   description: `Query "${queryData.name}" loaded successfully.`,
      //   duration: TOAST_DURATION.SUCCESS
      // })

      // Don't call syncUrlFromState() since we're explicitly setting the URL

      return true
    } catch (error) {
      console.error('Error loading saved query:', error)
      // Clear active saved query name on error
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

  // Generate URL for a saved query
  function getQueryUrl(query: SavedTeamQuery): string {
    try {
      // Get query type from the saved query - ensure it's normalized
      const queryType = query.query_type?.toLowerCase() === 'logchefql' ? 'logchefql' : 'sql'
      console.log(`Building URL for query ${query.id} with type ${queryType}`)

      // Parse the query content
      const queryContent = JSON.parse(query.query_content)

      // Build the URL with the appropriate parameters
      let url = `/logs/explore?team=${query.team_id}`

      // Add source ID if available
      if (query.source_id) {
        url += `&source=${query.source_id}`
      }

      // Add query ID for editing
      url += `&query_id=${query.id}`

      // Add limit if available
      if (queryContent.limit) {
        url += `&limit=${queryContent.limit}`
      }

      // Always add time range if available - this is crucial for query execution
      if (queryContent.timeRange !== null &&
          queryContent.timeRange?.absolute?.start &&
          queryContent.timeRange?.absolute?.end) {
        url += `&start_time=${queryContent.timeRange.absolute.start}`
        url += `&end_time=${queryContent.timeRange.absolute.end}`
      }

      // Add mode parameter based on query type
      url += `&mode=${queryType}`

      // Add the query content (actual query text)
      if (queryContent.content) {
        url += `&q=${encodeURIComponent(queryContent.content)}`
      }

      return url
    } catch (error) {
      console.error('Error generating query URL:', error)
      // Fallback URL with explicit mode parameter
      return `/logs/explore?team=${query.team_id}&source=${query.source_id}&mode=${query.query_type?.toLowerCase() === 'logchefql' ? 'logchefql' : 'sql'}`
    }
  }

  // Handle opening query in explorer
  function openQuery(query: SavedTeamQuery) {
    const url = getQueryUrl(query)
    // Always use router.push to create a proper history entry
    // This ensures the back button works correctly when navigating between queries
    router.push(url)
  }

  // Handle edit query
  function editQuery(query: SavedTeamQuery) {
    try {
      // Deep clone the query to avoid reference issues
      editingQuery.value = JSON.parse(JSON.stringify(query))
      console.log('Editing query:', editingQuery.value)
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

  // Handle delete query
  async function deleteQuery(query: SavedTeamQuery) {
    if (window.confirm(`Are you sure you want to delete "${query.name}"? This action cannot be undone.`)) {
      try {
        await savedQueriesStore.deleteQuery(query.team_id, query.source_id, query.id.toString())

        // Check if the deleted query is the active one
        if (exploreStore.selectedQueryId === query.id.toString()) {
          // Clear the active query name and ID
          exploreStore.setActiveSavedQueryName(null);
          exploreStore.setSelectedQueryId(null);

          // Remove query_id from URL if present
          if (route.query.query_id) {
            const currentQuery = { ...route.query };
            delete currentQuery.query_id;
            router.replace({ query: currentQuery });
          }
        }

        // Refresh the queries list - assuming loadSourceQueries will be called externally
        toast({
          title: 'Success',
          description: 'Query deleted successfully',
          duration: TOAST_DURATION.SUCCESS,
        })

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

  // Load queries for a team and source
  async function loadSourceQueries(teamId: number, sourceId: number) {
    try {
      isLoading.value = true

      // Reset search when loading new queries
      searchQuery.value = ''

      if (!teamId || !sourceId) {
        console.warn("No team or source ID provided for loading queries")
        queriesRef.value = []
        return { success: false, error: 'No team or source ID provided' }
      }

      const result = await savedQueriesStore.fetchTeamSourceQueries(teamId, sourceId)

      if (result.success) {
        queriesRef.value = result.data ?? []
        return { success: true, data: result.data }
      } else {
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
      }
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

  // Create a new query in the explorer
  function createNewQuery(sourceId?: number) {
    console.log("Creating new query in useSavedQueries...");

    // Reset the query state to defaults
    // Use the centralized reset function in the store
    exploreStore.resetQueryToDefaults();

    // Build new query parameters without query_id
    const newQuery: Record<string, string> = {};

    // Keep the current team if available
    if (route.query.team) {
      newQuery.team = route.query.team as string;
    }

    // Set source ID if provided, otherwise keep current
    if (sourceId) {
      newQuery.source = sourceId.toString();
    } else if (route.query.source) {
      newQuery.source = route.query.source as string;
    }

    // Set limit from current store state
    newQuery.limit = exploreStore.limit.toString();

    // Set time range from current store state
    const startTime = calendarDateTimeToTimestamp(exploreStore.timeRange?.start);
    const endTime = calendarDateTimeToTimestamp(exploreStore.timeRange?.end);
    if (startTime !== null && endTime !== null) {
      newQuery.start_time = startTime.toString();
      newQuery.end_time = endTime.toString();
    }

    // Set mode from current store state
    newQuery.mode = exploreStore.activeMode;

    // Apply the new URL - use push instead of replace to preserve history
    router.push({
      path: '/logs/explore',
      query: newQuery
    }).then(() => {
      // Focus the editor after navigation completes
      setTimeout(() => {
        // Try to find the QueryEditor component
        const queryEditor = document.querySelector('.monaco-editor');
        if (queryEditor) {
          (queryEditor as HTMLElement).focus();
        }
      }, 50);
    });
  }

  // Local helper to fetch details, now using store action
  async function getQueryDetails(teamId: number, sourceId: number, queryId: string) {
    console.warn("`getQueryDetails` function in useSavedQueries is deprecated. Use store action directly.");
    return await savedQueriesStore.fetchTeamSourceQueries(teamId, sourceId);
    // Note: This fetches *all* queries for the source, not a single one by ID.
    // The store doesn't seem to have a dedicated action for one source query by ID.
    // We might need to add one if `fetchTeamSourceQueries` returning a list is inefficient.
  }

  // Function to update an existing query
  async function updateSavedQuery(
      teamId: number,
      sourceId: number,
      queryId: string,
      updateData: { // Define the expected update payload shape
        name?: string;
        description?: string;
        query_content: string; // Content is required for update here
        query_type: 'logchefql' | 'sql'; // Type is required
      }
  ) {
    console.log(`useSavedQueries: Updating query ${queryId} for team ${teamId}, source ${sourceId}`);
    isLoading.value = true;
    try {
      const payload = {
        name: updateData.name, // Pass along if provided
        description: updateData.description, // Pass along if provided
        query_content: updateData.query_content,
        query_type: updateData.query_type,
      };

      // Call the specific store action for updating a team-source query
      const result = await savedQueriesStore.updateTeamSourceQuery(teamId, sourceId, queryId, payload);

      if (result.success) {
        console.log(`useSavedQueries: Query ${queryId} updated successfully.`);
        // No need to manually update local 'queries' ref here,
        // as the store action already updates the store's query list.
        // The component using the store should react to the store change.
        return { success: true, data: result.data };
      } else {
        throw new Error(result.error?.message || 'Failed to update query in store action');
      }
    } catch (error) {
      console.error(`Error updating saved query ${queryId}:`, error);
      toast({
        title: 'Update Failed',
        description: getErrorMessage(error),
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      });
      // Rethrow or return error indicator
      throw error; // Rethrow to allow caller to handle
    } finally {
      isLoading.value = false;
    }
  }

  return {
    // State
    showSaveQueryModal,
    editingQuery,
    isLoading,
    isLoadingQueryDetails,
    queries: queriesRef, // Return the queriesRef instead of direct parameter
    filteredQueries,
    hasQueries,
    totalQueryCount,
    searchQuery,
    isEditingExistingQuery,
    canManageCollections,

    // Functions
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
    getQueryDetails
  }
}