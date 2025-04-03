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
      teamsStore.setCurrentTeam(teamId)
      sourcesStore.clearCurrentSourceDetails()
      exploreStore.setSource(0)

      // Ensure we use the user teams endpoint
      await teamsStore.loadTeams(false, false)
      const sourcesResult = await sourcesStore.loadTeamSources(teamId)

      if (sourcesResult.success && sourcesResult.data?.length) {
        const previousSourceId = currentSourceId.value
        const sourceExists = sourcesResult.data.some(s => s.id === previousSourceId)

        if (!sourceExists) {
          exploreStore.setSource(sourcesResult.data[0].id)
        } else {
          await sourcesStore.loadSourceDetails(previousSourceId)
        }
      }
    } catch (error) {
      console.error('Error changing team:', error)
    } finally {
      // Use nextTick to ensure state updates propagate before resetting the flag
      nextTick(() => {
        isProcessingTeamChange.value = false;
      });
    }
  }

  // Source change handler
  async function handleSourceChange(sourceIdStr: string) {
    if (isProcessingSourceChange.value) return
    isProcessingSourceChange.value = true

    const sourceId = parseInt(sourceIdStr)
    if (isNaN(sourceId) || sourceId <= 0) {
      exploreStore.setSource(0)
      sourcesStore.clearCurrentSourceDetails()
      isProcessingSourceChange.value = false
      return
    }

    if (!availableSources.value.some(s => s.id === sourceId)) {
      console.warn(`Source ${sourceId} not found in team ${currentTeamId.value}'s sources`)
      isProcessingSourceChange.value = false
      return
    }

    try {
      exploreStore.setSource(sourceId)
      await sourcesStore.loadSourceDetails(sourceId)
    } catch (error) {
      console.error('Error changing source:', error)
    } finally {
      // Use nextTick for consistency
      nextTick(() => {
        isProcessingSourceChange.value = false;
      });
    }
  }

  return {
    // State
    isProcessingTeamChange,
    isProcessingSourceChange,
    currentTeamId,
    currentSourceId,
    sourceDetails,
    hasValidSource,

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
