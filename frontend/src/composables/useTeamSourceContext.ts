import { computed, watch } from 'vue'
import { useContextStore } from '@/stores/context'
import { useSourcesStore } from '@/stores/sources'
import { useTeamsStore } from '@/stores/teams'

export function useTeamSourceContext() {
  const contextStore = useContextStore()
  const teamsStore = useTeamsStore()
  const sourcesStore = useSourcesStore()

  const currentTeamId = computed(() => contextStore.teamId)
  const currentSourceId = computed(() => contextStore.sourceId)
  const availableTeams = computed(() => teamsStore.teams || [])
  const availableSources = computed(() => sourcesStore.teamSources || [])

  function parseId(value: unknown): number | null {
    if (value === undefined || value === null || value === '') {
      return null
    }

    const parsed = parseInt(String(value), 10)
    return Number.isNaN(parsed) ? null : parsed
  }

  function resolveTeamId(requestedTeamId: number | null): number | null {
    if (requestedTeamId && availableTeams.value.some(team => team.id === requestedTeamId)) {
      return requestedTeamId
    }

    const storedDefaults = contextStore.getStoredDefaults()
    if (storedDefaults.teamId && availableTeams.value.some(team => team.id === storedDefaults.teamId)) {
      return storedDefaults.teamId
    }

    return teamsStore.currentTeamId ?? availableTeams.value[0]?.id ?? null
  }

  function resolveSourceId(teamId: number, requestedSourceId: number | null): number | null {
    if (requestedSourceId && availableSources.value.some(source => source.id === requestedSourceId)) {
      return requestedSourceId
    }

    const storedSourceId = contextStore.getStoredSourceForTeam(teamId)
    if (storedSourceId && availableSources.value.some(source => source.id === storedSourceId)) {
      return storedSourceId
    }

    return availableSources.value[0]?.id ?? null
  }

  function waitForTeamSources(teamId: number, maxWaitMs = 5000): Promise<void> {
    if (currentTeamId.value === teamId && !sourcesStore.isLoadingTeamSources) {
      return Promise.resolve()
    }

    return new Promise((resolve) => {
      const timeout = setTimeout(() => {
        stopWatch()
        resolve()
      }, maxWaitMs)

      const stopWatch = watch(
        [
          () => currentTeamId.value,
          () => sourcesStore.isLoadingTeamSources,
        ],
        ([selectedTeamId, isLoadingTeamSources]) => {
          if (selectedTeamId === teamId && !isLoadingTeamSources) {
            clearTimeout(timeout)
            stopWatch()
            resolve()
          }
        },
        { immediate: true }
      )
    })
  }

  function waitForSourceDetails(teamId: number, sourceId: number, maxWaitMs = 5000): Promise<void> {
    if (
      currentTeamId.value === teamId &&
      currentSourceId.value === sourceId &&
      !sourcesStore.isLoadingSourceDetails
    ) {
      return Promise.resolve()
    }

    return new Promise((resolve) => {
      const timeout = setTimeout(() => {
        stopWatch()
        resolve()
      }, maxWaitMs)

      const stopWatch = watch(
        [
          () => currentTeamId.value,
          () => currentSourceId.value,
          () => sourcesStore.isLoadingSourceDetails,
        ],
        ([selectedTeamId, selectedSourceId, isLoadingSourceDetails]) => {
          if (
            selectedTeamId === teamId &&
            selectedSourceId === sourceId &&
            !isLoadingSourceDetails
          ) {
            clearTimeout(timeout)
            stopWatch()
            resolve()
          }
        },
        { immediate: true }
      )
    })
  }

  async function applyContextSelection(
    teamId: number,
    requestedSourceId: number | null,
    options: { maxWaitMs?: number } = {}
  ): Promise<number | null> {
    const maxWaitMs = options.maxWaitMs ?? 5000

    if (currentTeamId.value !== teamId) {
      contextStore.selectTeam(teamId)
    }

    await waitForTeamSources(teamId, maxWaitMs)

    const resolvedSourceId = resolveSourceId(teamId, requestedSourceId)
    if (!resolvedSourceId) {
      return null
    }

    if (currentSourceId.value !== resolvedSourceId) {
      contextStore.selectSource(resolvedSourceId)
    }

    await waitForSourceDetails(teamId, resolvedSourceId, maxWaitMs)
    return resolvedSourceId
  }

  return {
    currentTeamId,
    currentSourceId,
    availableTeams,
    availableSources,
    parseId,
    resolveTeamId,
    resolveSourceId,
    waitForTeamSources,
    waitForSourceDetails,
    applyContextSelection,
  }
}
