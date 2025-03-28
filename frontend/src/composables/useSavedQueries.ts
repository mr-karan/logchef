import { ref } from 'vue'
import { useExploreStore } from '@/stores/explore'
import { useSavedQueriesStore } from '@/stores/savedQueries'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import { getErrorMessage } from '@/api/types'
import type { SavedQueryFormData } from '@/views/explore/types'

export function useSavedQueries() {
  const exploreStore = useExploreStore()
  const savedQueriesStore = useSavedQueriesStore()
  const { toast } = useToast()

  const showSaveQueryModal = ref(false)

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
      const response = await savedQueriesStore.createQuery(formData.team_id, {
        team_id: formData.team_id,
        name: formData.name,
        description: formData.description,
        source_id: formData.source_id,
        query_type: formData.query_type,
        query_content: formData.query_content
      })

      if (response.success) {
        showSaveQueryModal.value = false
        await savedQueriesStore.fetchTeamQueries(formData.team_id)
        toast({
          title: 'Success',
          description: 'Query saved successfully.',
          duration: TOAST_DURATION.SUCCESS
        })
      } else {
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
      exploreStore.clearError()

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

  return {
    showSaveQueryModal,
    handleSaveQueryClick,
    handleSaveQuery,
    loadSavedQuery
  }
}