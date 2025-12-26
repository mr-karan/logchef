import { defineStore } from 'pinia'

// Persists team/source selection. Stores last source PER TEAM so switching teams restores previous source.
const STORAGE_KEY = 'logchef_context'

interface PersistedContext {
  lastTeamId: number | null
  sourcePerTeam: Record<number, number>  // teamId → sourceId
}

interface ContextState {
  teamId: number | null
  sourceId: number | null
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

export const useContextStore = defineStore('context', {
  state: (): ContextState => ({
    teamId: null,
    sourceId: null,
  }),

  getters: {
    hasTeam: (state) => state.teamId !== null && state.teamId > 0,
    hasSource: (state) => state.sourceId !== null && state.sourceId > 0,
    hasValidContext: (state) => 
      state.teamId !== null && state.teamId > 0 && 
      state.sourceId !== null && state.sourceId > 0,
  },

  actions: {
    selectTeam(teamId: number) {
      console.log(`ContextStore: Selecting team ${teamId}`)
      this.teamId = teamId
      
      const persisted = loadFromStorage()
      const lastSourceForTeam = persisted.sourcePerTeam[teamId]
      this.sourceId = lastSourceForTeam ?? null
      
      persisted.lastTeamId = teamId
      saveToStorage(persisted)
    },

    selectSource(sourceId: number) {
      if (!this.hasTeam) {
        console.warn(`ContextStore: Cannot select source ${sourceId} without team`)
        return
      }
      console.log(`ContextStore: Selecting source ${sourceId} for team ${this.teamId}`)
      this.sourceId = sourceId
      
      const persisted = loadFromStorage()
      persisted.sourcePerTeam[this.teamId!] = sourceId
      saveToStorage(persisted)
    },

    clear() {
      console.log('ContextStore: Clearing all context')
      this.teamId = null
      this.sourceId = null
    },

    clearStorage() {
      console.log('ContextStore: Clearing localStorage')
      try {
        localStorage.removeItem(STORAGE_KEY)
      } catch (e) {
        console.warn('ContextStore: Failed to clear localStorage', e)
      }
    },

    setFromRoute(teamId: number | null, sourceId: number | null) {
      console.log(`ContextStore: Setting from route - team: ${teamId}, source: ${sourceId}`)
      this.teamId = teamId
      this.sourceId = sourceId
      
      if (teamId) {
        const persisted = loadFromStorage()
        persisted.lastTeamId = teamId
        if (sourceId) {
          persisted.sourcePerTeam[teamId] = sourceId
        }
        saveToStorage(persisted)
      }
    },

    getStoredDefaults(): { teamId: number | null; sourceId: number | null } {
      const persisted = loadFromStorage()
      const teamId = persisted.lastTeamId
      const sourceId = teamId ? (persisted.sourcePerTeam[teamId] ?? null) : null
      return { teamId, sourceId }
    },

    getStoredSourceForTeam(teamId: number): number | null {
      const persisted = loadFromStorage()
      return persisted.sourcePerTeam[teamId] ?? null
    },
  },
})
