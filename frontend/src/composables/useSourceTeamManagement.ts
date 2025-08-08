import { ref, computed, nextTick } from 'vue'
import { useExploreStore } from '@/stores/explore'
import { useSourcesStore } from '@/stores/sources'
import { useTeamsStore } from '@/stores/teams'
import { formatSourceName } from '@/utils/format'
import type { FieldInfo } from '@/views/explore/types'
import { useRouteSync } from '@/composables/useRouteSync'

export function useSourceTeamManagement() {
  const exploreStore = useExploreStore()
  const sourcesStore = useSourcesStore()
  const teamsStore = useTeamsStore()

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

  // Team change handler
  async function handleTeamChange(teamIdStr: string) {
    if (isProcessingTeamChange.value) return
    isProcessingTeamChange.value = true

    const teamId = parseInt(teamIdStr)
    if (isNaN(teamId)) {
      console.warn('Invalid team ID:', teamIdStr)
      isProcessingTeamChange.value = false
      return
    }

    try {
      await routeChangeTeam(teamId)
    } catch (error) {
      console.error('Error changing team:', error)
    } finally {
      setTimeout(() => {
        isProcessingTeamChange.value = false
      }, 50)
    }
  }

  // Source change handler
  async function handleSourceChange(sourceIdStr: string) {
    if (isProcessingSourceChange.value) return
    isProcessingSourceChange.value = true

    const sourceId = parseInt(sourceIdStr)
    if (isNaN(sourceId) || sourceId <= 0) {
      sourcesStore.clearCurrentSourceDetails()
      exploreStore.setSource(0)
      setTimeout(() => {
        isProcessingSourceChange.value = false
      }, 50)
      return
    }

    if (!availableSources.value.some(s => s.id === sourceId)) {
      console.warn(`Source ${sourceId} not found in team ${currentTeamId.value}'s sources`)
      isProcessingSourceChange.value = false
      return
    }

    try {
      await routeChangeSource(sourceId)
    } catch (error) {
      console.error('Error changing source:', error)
    } finally {
      setTimeout(() => {
        isProcessingSourceChange.value = false
      }, 50)
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
