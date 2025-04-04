import { ref, computed, nextTick } from 'vue'
import { useExploreStore } from '@/stores/explore'
import { useSourcesStore } from '@/stores/sources'
import { useTeamsStore } from '@/stores/teams'
import { formatSourceName } from '@/utils/format'
import type { FieldInfo } from '@/views/explore/types'

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
  const availableFields = computed((): FieldInfo[] => {
    const columns = sourceDetails.value?.columns || exploreStore.columns || []
    const fields: FieldInfo[] = columns.map(col => ({
      name: col.name,
      type: col.type
    }))

    // Add metadata fields if not present
    const tsFieldName = sourceDetails.value?._meta_ts_field || 'timestamp'
    const severityFieldName = sourceDetails.value?._meta_severity_field || 'severity_text'

    let tsFound = false
    let severityFound = false

    fields.forEach(f => {
      if (f.name === tsFieldName) {
        f.isTimestamp = true
        tsFound = true
      }
      if (f.name === severityFieldName) {
        f.isSeverity = true
        severityFound = true
      }
    })

    if (!tsFound) {
      fields.push({ name: tsFieldName, type: 'timestamp', isTimestamp: true })
    }
    if (!severityFound) {
      fields.push({ name: severityFieldName, type: 'string', isSeverity: true })
    }

    return fields
  })

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
      // Set immediately to avoid FOUC
      teamsStore.setCurrentTeam(teamId)
      
      // Clear source details BEFORE changing sourceId to prevent flicker
      sourcesStore.clearCurrentSourceDetails()
      exploreStore.setSource(0)

      // Load team sources
      const sourcesResult = await sourcesStore.loadTeamSources(teamId)

      // Only after we have sources loaded, set the first source as active
      if (sourcesResult.success && sourcesResult.data?.length) {
        // Always set the first available source when changing teams
        const firstSourceId = sourcesResult.data[0].id
        exploreStore.setSource(firstSourceId)
        
        // Load source details AFTER setting source ID
        await sourcesStore.loadSourceDetails(firstSourceId)
      }
    } catch (error) {
      console.error('Error changing team:', error)
    } finally {
      // Use nextTick to ensure state updates propagate before resetting the flag
      // This gives a smoother transition between states
      setTimeout(() => {
        isProcessingTeamChange.value = false
      }, 50) // Short delay to ensure state transitions properly
    }
  }

  // Source change handler
  async function handleSourceChange(sourceIdStr: string) {
    if (isProcessingSourceChange.value) return
    isProcessingSourceChange.value = true

    const sourceId = parseInt(sourceIdStr)
    if (isNaN(sourceId) || sourceId <= 0) {
      // First clear source details to avoid showing old source info
      sourcesStore.clearCurrentSourceDetails()
      exploreStore.setSource(0)
      
      // Use setTimeout instead of nextTick for consistent timing
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
      // Clear source details BEFORE setting source ID to prevent flickering
      sourcesStore.clearCurrentSourceDetails()
      
      // Then set source ID (this will trigger UI changes)
      exploreStore.setSource(sourceId)
      
      // Load source details
      await sourcesStore.loadSourceDetails(sourceId)
    } catch (error) {
      console.error('Error changing source:', error)
    } finally {
      // Use setTimeout for consistent timing
      setTimeout(() => {
        isProcessingSourceChange.value = false
      }, 50) // Short delay to ensure state transitions properly
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
