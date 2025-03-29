import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useExploreStore } from '@/stores/explore'
import { useSavedQueriesStore } from '@/stores/savedQueries'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import { getErrorMessage } from '@/api/types'
import type { SavedQueryFormData } from '@/views/explore/types'
import type { SavedTeamQuery } from '@/api/savedQueries'

export function useSavedQueries() {
  const router = useRouter()
  const exploreStore = useExploreStore()
  const savedQueriesStore = useSavedQueriesStore()
  const { toast } = useToast()

  const showSaveQueryModal = ref(false)
  const editingQuery = ref<SavedTeamQuery | null>(null)
  const isLoading = ref(false)
  const queries = ref<SavedTeamQuery[]>([])
  const searchQuery = ref('')

  // Computed for filtered queries based on search
  const filteredQueries = computed(() => {
    if (!searchQuery.value.trim()) {
      return queries.value
    }

    const search = searchQuery.value.toLowerCase()
    return queries.value.filter(query =>
      query.name.toLowerCase().includes(search) ||
      (query.description && query.description.toLowerCase().includes(search))
    )
  })

  // Has queries computed property
  const hasQueries = computed(() => filteredQueries.value.length > 0)

  // Total query count
  const totalQueryCount = computed(() => queries.value.length)

  // Clear search function
  function clearSearch() {
    searchQuery.value = ''
  }

  // Save query modal trigger
  function handleSaveQueryClick() {
    const query = exploreStore.activeMode === 'logchefql'
      ? exploreStore.logchefqlCode
      : exploreStore.rawSql

    if (!query?.trim()) {
      toast({
        title: 'Cannot Save',
        description: 'Query is empty. Please enter a query to save.',
        variant: 'warning',
        duration: TOAST_DURATION.WARNING
      })
      return
    }

    showSaveQueryModal.value = true
  }

  // Handle actual saving
  async function handleSaveQuery(formData: SavedQueryFormData) {
    try {
      let response;
      
      // Check if we're updating an existing query (explicit edit mode)
      if (editingQuery.value) {
        // Update existing query
        response = await savedQueriesStore.updateQuery(
          formData.team_id,
          editingQuery.value.id.toString(),
          {
            name: formData.name,
            description: formData.description,
            source_id: formData.source_id,
            query_type: formData.query_type,
            query_content: formData.query_content
          }
        )
        console.log('Updated query:', response)
      } 
      // Check if a query with this name already exists (possible overwrite)
      else {
        // Get existing queries to check for duplicates
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
            // Update the existing query
            response = await savedQueriesStore.updateQuery(
              formData.team_id,
              existingQuery.id.toString(),
              {
                name: formData.name,
                description: formData.description,
                source_id: formData.source_id,
                query_type: formData.query_type,
                query_content: formData.query_content
              }
            )
            console.log('Overwrote existing query:', response)
          } else {
            // User cancelled the overwrite
            return;
          }
        } else {
          // Create new query
          response = await savedQueriesStore.createQuery(formData.team_id, {
            team_id: formData.team_id,
            name: formData.name,
            description: formData.description,
            source_id: formData.source_id,
            query_type: formData.query_type,
            query_content: formData.query_content
          })
          console.log('Created new query:', response)
        }
      }

      if (response && response.success) {
        showSaveQueryModal.value = false
        editingQuery.value = null  // Clear editing state
        await savedQueriesStore.fetchTeamQueries(formData.team_id)
        
        // Show appropriate success message
        const actionVerb = editingQuery.value ? 'updated' : 'saved';
        toast({
          title: 'Success',
          description: `Query ${actionVerb} successfully.`,
          duration: TOAST_DURATION.SUCCESS
        })
      } else if (response) {
        throw new Error(response.error || 'Failed to save query')
      }
    } catch (error) {
      console.error("Error saving query:", error)
      toast({
        title: 'Error',
        description: getErrorMessage(error),
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      })
    }
  }

  // Load saved query
  async function loadSavedQuery(queryData: any) {
    if (!queryData?.query_content) {
      toast({
        title: 'Error',
        description: 'Invalid saved query data.',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      })
      return
    }

    try {
      const content = JSON.parse(queryData.query_content)
      const isLogchefQL = queryData.query_type === 'logchefql'
      const queryToLoad = content.content || ''

      // Reset state
      if (exploreStore.clearError) {
        exploreStore.clearError()
      }

      // Set mode
      exploreStore.setActiveMode(isLogchefQL ? 'logchefql' : 'sql')

      // Set content
      if (isLogchefQL) {
        exploreStore.setLogchefqlCode(queryToLoad)
      } else {
        exploreStore.setRawSql(queryToLoad)
      }

      // Optional: Set other parameters from saved query
      if (content.limit) exploreStore.setLimit(content.limit)
      if (content.timeRange) exploreStore.setTimeRange(content.timeRange)

      toast({
        title: 'Success',
        description: 'Query loaded successfully.',
        duration: TOAST_DURATION.SUCCESS
      })

      return true
    } catch (error) {
      console.error('Error loading saved query:', error)
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

      // Add limit if available
      if (queryContent.limit) {
        url += `&limit=${queryContent.limit}`
      }

      // Add time range if available
      if (queryContent.timeRange?.absolute) {
        url += `&start_time=${queryContent.timeRange.absolute.start}`
        url += `&end_time=${queryContent.timeRange.absolute.end}`
      }

      // Add mode parameter based on query type - always include this early in the URL
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
    // Force reload component when navigating to the same route with different params
    if (router.currentRoute.value.path === '/logs/explore') {
      // If we're already on the explore page, replace instead of push to avoid navigation guards
      router.replace(url)
    } else {
      router.push(url)
    }
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
        await savedQueriesStore.deleteQuery(query.team_id, query.id.toString())

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
        queries.value = []
        return { success: false, error: 'No team or source ID provided' }
      }

      const result = await savedQueriesStore.fetchTeamSourceQueries(teamId, sourceId)

      if (result.success) {
        queries.value = result.data ?? []
        return { success: true, data: result.data }
      } else {
        queries.value = []
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
      queries.value = []
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
    if (sourceId) {
      router.push(`/logs/explore?source=${sourceId}`)
    } else {
      router.push('/logs/explore')
    }
  }

  return {
    // State
    showSaveQueryModal,
    editingQuery,
    isLoading,
    queries,
    filteredQueries,
    hasQueries,
    totalQueryCount,
    searchQuery,
    
    // Functions
    handleSaveQueryClick,
    handleSaveQuery,
    loadSavedQuery,
    loadSourceQueries,
    getQueryUrl,
    openQuery,
    editQuery,
    deleteQuery,
    createNewQuery,
    clearSearch
  }
}
