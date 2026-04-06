import { ref, computed } from 'vue'
import { defineStore } from 'pinia'

// Persists team/source selection. Stores last source PER TEAM so switching teams restores previous source.
const STORAGE_KEY = 'logchef_context'

interface PersistedContext {
  lastTeamId: number | null
  sourcePerTeam: Record<number, number>  // teamId → sourceId
}

function loadFromStorage(): PersistedContext {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored) {
      const parsed = JSON.parse(stored)
      return {
        lastTeamId: parsed.lastTeamId ?? null,
        sourcePerTeam: parsed.sourcePerTeam ?? {},
      }
    }
  } catch (e) {
    console.warn('ContextStore: Failed to load from localStorage', e)
  }
  return { lastTeamId: null, sourcePerTeam: {} }
}

function saveToStorage(data: PersistedContext): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(data))
  } catch (e) {
    console.warn('ContextStore: Failed to save to localStorage', e)
  }
}

export const useContextStore = defineStore('context', () => {
  const teamId = ref<number | null>(null)
  const sourceId = ref<number | null>(null)

  const hasTeam = computed(() => teamId.value !== null && teamId.value > 0)
  const hasSource = computed(() => sourceId.value !== null && sourceId.value > 0)
  const hasValidContext = computed(() =>
    teamId.value !== null && teamId.value > 0 &&
    sourceId.value !== null && sourceId.value > 0
  )

  function selectTeam(newTeamId: number) {
    
    const previousTeamId = teamId.value
    teamId.value = newTeamId

    // IMPORTANT: Clear source when team changes to prevent 403 errors
    // The cached source may not belong to the new team. The sourcesStore
    // will restore a valid source after loading the new team's sources.
    if (previousTeamId !== newTeamId) {
      sourceId.value = null
    }

    const persisted = loadFromStorage()
    persisted.lastTeamId = newTeamId
    saveToStorage(persisted)
  }

  function selectSource(newSourceId: number) {
    if (!hasTeam.value) {
      console.warn(`ContextStore: Cannot select source ${newSourceId} without team`)
      return
    }
    sourceId.value = newSourceId

    const persisted = loadFromStorage()
    persisted.sourcePerTeam[teamId.value!] = newSourceId
    saveToStorage(persisted)
  }

  function clear() {
    console.log('ContextStore: Clearing all context')
    teamId.value = null
    sourceId.value = null
  }

  function clearStorage() {
    console.log('ContextStore: Clearing localStorage')
    try {
      localStorage.removeItem(STORAGE_KEY)
    } catch (e) {
      console.warn('ContextStore: Failed to clear localStorage', e)
    }
  }

  function setFromRoute(routeTeamId: number | null, routeSourceId: number | null) {

    const teamChanged = routeTeamId !== teamId.value

    if (teamChanged && routeTeamId) {
      selectTeam(routeTeamId)
    } else if (routeTeamId && !teamId.value) {
      selectTeam(routeTeamId)
    } else if (!routeTeamId) {
      teamId.value = null
      sourceId.value = null
      return
    }

    if (routeSourceId && teamId.value) {
      selectSource(routeSourceId)
    }
  }

  function getStoredDefaults(): { teamId: number | null; sourceId: number | null } {
    const persisted = loadFromStorage()
    const storedTeamId = persisted.lastTeamId
    const storedSourceId = storedTeamId ? (persisted.sourcePerTeam[storedTeamId] ?? null) : null
    return { teamId: storedTeamId, sourceId: storedSourceId }
  }

  function getStoredSourceForTeam(forTeamId: number): number | null {
    const persisted = loadFromStorage()
    return persisted.sourcePerTeam[forTeamId] ?? null
  }

  return {
    teamId,
    sourceId,
    hasTeam,
    hasSource,
    hasValidContext,
    selectTeam,
    selectSource,
    clear,
    clearStorage,
    setFromRoute,
    getStoredDefaults,
    getStoredSourceForTeam,
  }
})
