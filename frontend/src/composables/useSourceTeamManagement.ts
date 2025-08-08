import { ref, computed, nextTick } from 'vue'
import { useExploreStore } from '@/stores/explore'
import { useSourcesStore } from '@/stores/sources'
import { useTeamsStore } from '@/stores/teams'
import { formatSourceName } from '@/utils/format'
import type { FieldInfo } from '@/views/explore/types'
import { useRouteSync } from '@/composables/useRouteSync'
import { useRouter, useRoute } from 'vue-router'

// Export a shared flag to prevent URL sync during context transitions
export const contextTransitionInProgress = ref(false)

export function useSourceTeamManagement() {
  const exploreStore = useExploreStore()
  const sourcesStore = useSourcesStore()
  const teamsStore = useTeamsStore()
  const router = useRouter()
  const route = useRoute()

  const isProcessingTeamChange = ref(false)
  const isProcessingSourceChange = ref(false)

  // Computed properties for current state
  const currentTeamId = computed(() => teamsStore.currentTeamId)
  const currentSourceId = computed(() => exploreStore.sourceId)
  const sourceDetails = computed(() => sourcesStore.currentSourceDetails)
  const hasValidSource = computed(() => sourcesStore.hasValidCurrentSource)

  // Lists
  const availableTeams = computed(() => teamsStore.teams || [])
  const availableSources = computed(() => sourcesStore.teamSources || [])

  // Display names
  const selectedTeamName = computed(() => teamsStore.currentTeam?.name || 'Select team')
  const selectedSourceName = computed(() => {
    if (!currentSourceId.value) return 'Select source'
    const source = availableSources.value.find(s => s.id === currentSourceId.value)
    return source ? formatSourceName(source) : 'Select source'
  })

  // Loading states
  const isFetchingTeams = computed(() => teamsStore.isLoadingTeams())
  const isFetchingSources = computed(() =>
    currentTeamId.value && sourcesStore.isLoadingTeamSources(currentTeamId.value)
  )

  // Available fields for sidebar/autocompletion
  const availableFields = computed(() => {
    // Debugging: Log the source details being used
    console.log('useSourceTeamManagement: Calculating availableFields from:', sourceDetails.value);

    if (!sourceDetails.value?.columns) return [];
    // Return columns sorted alphabetically by name
    return [...sourceDetails.value.columns].sort((a, b) => a.name.localeCompare(b.name));
  })

  const { changeTeam: routeChangeTeam, changeSource: routeChangeSource } = useRouteSync()

  // Coordinated team change handler - loads resources in proper sequence
  async function handleTeamChange(teamIdStr: string) {
    if (isProcessingTeamChange.value) return
    isProcessingTeamChange.value = true
    contextTransitionInProgress.value = true

    const teamId = parseInt(teamIdStr)
    if (isNaN(teamId)) {
      console.warn('Invalid team ID:', teamIdStr)
      isProcessingTeamChange.value = false
      contextTransitionInProgress.value = false
      return
    }

    try {
      // Step 1: Set team in store (this stops most reactive watchers)
      if (teamsStore.currentTeamId !== teamId) {
        teamsStore.setCurrentTeam(teamId)
      }

      // Step 2: Load team sources 
      await sourcesStore.loadTeamSources(teamId)

      // Step 3: Select first available source for this team
      const firstSource = sourcesStore.teamSources[0]
      if (firstSource) {
        // Update stores with new source
        exploreStore.setSource(firstSource.id, { origin: 'user' })
        await sourcesStore.loadSourceDetails(firstSource.id)
        
        // Step 4: Update URL with both team and source
        await router.replace({ 
          query: { 
            ...route.query, 
            team: String(teamId), 
            source: String(firstSource.id) 
          } 
        })
      } else {
        // No sources available, just update team
        await router.replace({ 
          query: { 
            ...route.query, 
            team: String(teamId), 
            source: undefined 
          } 
        })
      }
    } catch (error) {
      console.error('Error changing team:', error)
    } finally {
      // Allow brief time for all state to settle
      setTimeout(() => {
        isProcessingTeamChange.value = false
        contextTransitionInProgress.value = false
      }, 100)
    }
  }

  // Coordinated source change handler
  async function handleSourceChange(sourceIdStr: string) {
    if (isProcessingSourceChange.value) return
    isProcessingSourceChange.value = true
    contextTransitionInProgress.value = true

    const sourceId = parseInt(sourceIdStr)
    if (isNaN(sourceId) || sourceId <= 0) {
      sourcesStore.clearCurrentSourceDetails()
      exploreStore.setSource(0)
      setTimeout(() => {
        isProcessingSourceChange.value = false
        contextTransitionInProgress.value = false
      }, 50)
      return
    }

    if (!availableSources.value.some(s => s.id === sourceId)) {
      console.warn(`Source ${sourceId} not found in team ${currentTeamId.value}'s sources`)
      isProcessingSourceChange.value = false
      contextTransitionInProgress.value = false
      return
    }

    try {
      // Step 1: Update explore store with new source
      exploreStore.setSource(sourceId, { origin: 'user' })
      
      // Step 2: Load source details
      await sourcesStore.loadSourceDetails(sourceId)
      
      // Step 3: Update URL
      await router.replace({ 
        query: { 
          ...route.query, 
          source: String(sourceId) 
        } 
      })
    } catch (error) {
      console.error('Error changing source:', error)
    } finally {
      setTimeout(() => {
        isProcessingSourceChange.value = false
        contextTransitionInProgress.value = false
      }, 100)
    }
  }

// Add computed property for source details loading
const isLoadingSourceDetails = computed(() => {
  if (!currentSourceId.value) return false;
  return sourcesStore.isLoadingSourceDetails(currentSourceId.value);
});

// Create a combined loading state - specifically for major context changes
// We separate this from source details loading to allow different UX treatments
const isChangingContext = computed(() => {
  // True if either team or source selection is being processed
  if (isProcessingTeamChange.value || isProcessingSourceChange.value) {
    return true;
  }
  // True if team sources are being fetched for the current team
  if (currentTeamId.value && sourcesStore.isLoadingTeamSources(currentTeamId.value)) {
    return true;
  }
  // We don't include isLoadingSourceDetails here, as it's a different type of loading
  // that happens after initial context is established
  return false;
});

  return {
    // State
    isProcessingTeamChange,
    isProcessingSourceChange,
    currentTeamId,
    currentSourceId,
    sourceDetails,
    hasValidSource,
    isChangingContext,
    isLoadingSourceDetails,

    // Lists and names
    availableTeams,
    availableSources,
    selectedTeamName,
    selectedSourceName,

    // Loading states
    isFetchingTeams,
    isFetchingSources,

    // Fields
    availableFields,

    // Handlers
    handleTeamChange,
    handleSourceChange
  }
}
