import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import type { Source } from '@/api/sources'

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

/** The requested team when permitted, else the stored team, else the first. */
export function pickTeam(permittedTeamIds: number[], requestedTeamId: number | null): number | null {
  const permitted = (id: number | null): id is number => id !== null && permittedTeamIds.includes(id)
  if (permitted(requestedTeamId)) return requestedTeamId
  const storedTeamId = loadFromStorage().lastTeamId
  return permitted(storedTeamId) ? storedTeamId : permittedTeamIds[0] ?? null
}

/**
 * The requested source when it is in the team, even when not connected, so
 * a link never silently opens another source. Else the team's stored source,
 * else the first connected source, else the first.
 */
export function pickSource(teamId: number, sources: Source[], requestedSourceId: number | null): number | null {
  const inTeam = (id: number | null): id is number => id !== null && sources.some((source) => source.id === id)
  if (inTeam(requestedSourceId)) return requestedSourceId
  const storedSourceId = loadFromStorage().sourcePerTeam[teamId] ?? null
  if (inTeam(storedSourceId)) return storedSourceId
  return (sources.find((source) => source.is_connected) ?? sources[0])?.id ?? null
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

  // A selected source may be stale (old links, revoked access), so only rememberCurrentSource persists it.
  function selectSource(newSourceId: number) {
    if (!hasTeam.value) {
      console.warn(`ContextStore: Cannot select source ${newSourceId} without team`)
      return
    }
    sourceId.value = newSourceId
  }

  // Called once the team's source list confirms the current source.
  function rememberCurrentSource() {
    if (teamId.value === null || sourceId.value === null) return
    const persisted = loadFromStorage()
    persisted.sourcePerTeam[teamId.value] = sourceId.value
    saveToStorage(persisted)
  }

  function clearSource() {
    sourceId.value = null
  }

  function clear() {
    teamId.value = null
    sourceId.value = null
  }

  function clearStorage() {
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

  // Restores the persisted team only while it is still permitted, so a revoked
  // team never reaches the source loaders. Falls back to the first permitted
  // team, or forgets the selection when no team remains.
  function restoreTeam(permittedTeamIds: number[]) {
    const persisted = loadFromStorage()
    const storedTeamId = persisted.lastTeamId
    if (storedTeamId !== null && permittedTeamIds.includes(storedTeamId)) {
      selectTeam(storedTeamId)
      return
    }

    if (storedTeamId !== null) {
      delete persisted.sourcePerTeam[storedTeamId]
    }
    persisted.lastTeamId = null
    saveToStorage(persisted)

    if (permittedTeamIds.length > 0) {
      selectTeam(permittedTeamIds[0])
    } else {
      clear()
    }
  }

  function getStoredDefaults(): { teamId: number | null; sourceId: number | null } {
    const persisted = loadFromStorage()
    const storedTeamId = persisted.lastTeamId
    const storedSourceId = storedTeamId ? (persisted.sourcePerTeam[storedTeamId] ?? null) : null
    return { teamId: storedTeamId, sourceId: storedSourceId }
  }

  return {
    teamId,
    sourceId,
    hasTeam,
    hasSource,
    hasValidContext,
    selectTeam,
    selectSource,
    rememberCurrentSource,
    clearSource,
    clear,
    clearStorage,
    setFromRoute,
    restoreTeam,
    getStoredDefaults,
  }
})
