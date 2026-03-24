import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useContextStore } from '../context'

describe('useContextStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    vi.restoreAllMocks()
  })

  describe('initial state', () => {
    it('starts with null teamId and sourceId', () => {
      const store = useContextStore()
      expect(store.teamId).toBeNull()
      expect(store.sourceId).toBeNull()
    })

    it('has false getters initially', () => {
      const store = useContextStore()
      expect(store.hasTeam).toBe(false)
      expect(store.hasSource).toBe(false)
      expect(store.hasValidContext).toBe(false)
    })
  })

  describe('selectTeam', () => {
    it('sets teamId and persists to localStorage', () => {
      const store = useContextStore()
      store.selectTeam(1)
      expect(store.teamId).toBe(1)
      expect(store.hasTeam).toBe(true)

      const stored = JSON.parse(localStorage.getItem('logchef_context')!)
      expect(stored.lastTeamId).toBe(1)
    })

    it('clears sourceId when team changes', () => {
      const store = useContextStore()
      store.selectTeam(1)
      store.selectSource(10)
      expect(store.sourceId).toBe(10)

      store.selectTeam(2)
      expect(store.sourceId).toBeNull()
    })

    it('does not clear sourceId when selecting same team', () => {
      const store = useContextStore()
      store.selectTeam(1)
      store.selectSource(10)

      store.selectTeam(1)
      expect(store.sourceId).toBe(10)
    })
  })

  describe('selectSource', () => {
    it('sets sourceId when team is selected', () => {
      const store = useContextStore()
      store.selectTeam(1)
      store.selectSource(10)
      expect(store.sourceId).toBe(10)
      expect(store.hasSource).toBe(true)
      expect(store.hasValidContext).toBe(true)
    })

    it('does not set sourceId when no team is selected', () => {
      const store = useContextStore()
      store.selectSource(10)
      expect(store.sourceId).toBeNull()
    })

    it('persists sourceId per team in localStorage', () => {
      const store = useContextStore()
      store.selectTeam(1)
      store.selectSource(10)
      store.selectTeam(2)
      store.selectSource(20)

      const stored = JSON.parse(localStorage.getItem('logchef_context')!)
      expect(stored.sourcePerTeam[1]).toBe(10)
      expect(stored.sourcePerTeam[2]).toBe(20)
    })
  })

  describe('setFromRoute', () => {
    it('sets both team and source from route params', () => {
      const store = useContextStore()
      store.setFromRoute(1, 10)
      expect(store.teamId).toBe(1)
      expect(store.sourceId).toBe(10)
    })

    it('clears state when teamId is null', () => {
      const store = useContextStore()
      store.selectTeam(1)
      store.selectSource(10)

      store.setFromRoute(null, null)
      expect(store.teamId).toBeNull()
      expect(store.sourceId).toBeNull()
    })

    it('clears sourceId when team changes via route', () => {
      const store = useContextStore()
      store.selectTeam(1)
      store.selectSource(10)

      store.setFromRoute(2, null)
      expect(store.teamId).toBe(2)
      expect(store.sourceId).toBeNull()
    })
  })

  describe('getStoredDefaults', () => {
    it('returns stored teamId and matching sourceId', () => {
      const store = useContextStore()
      store.selectTeam(1)
      store.selectSource(10)

      const defaults = store.getStoredDefaults()
      expect(defaults.teamId).toBe(1)
      expect(defaults.sourceId).toBe(10)
    })

    it('returns null sourceId when no team stored', () => {
      const store = useContextStore()
      const defaults = store.getStoredDefaults()
      expect(defaults.teamId).toBeNull()
      expect(defaults.sourceId).toBeNull()
    })
  })

  describe('getStoredSourceForTeam', () => {
    it('returns stored source for specific team', () => {
      const store = useContextStore()
      store.selectTeam(1)
      store.selectSource(10)

      expect(store.getStoredSourceForTeam(1)).toBe(10)
    })

    it('returns null for unknown team', () => {
      const store = useContextStore()
      expect(store.getStoredSourceForTeam(999)).toBeNull()
    })
  })

  describe('clear / clearStorage', () => {
    it('clear resets both to null', () => {
      const store = useContextStore()
      store.selectTeam(1)
      store.selectSource(10)

      store.clear()
      expect(store.teamId).toBeNull()
      expect(store.sourceId).toBeNull()
    })

    it('clearStorage removes localStorage key', () => {
      const store = useContextStore()
      store.selectTeam(1)
      expect(localStorage.getItem('logchef_context')).not.toBeNull()

      store.clearStorage()
      expect(localStorage.getItem('logchef_context')).toBeNull()
    })
  })

  describe('localStorage resilience', () => {
    it('handles corrupted localStorage gracefully', () => {
      localStorage.setItem('logchef_context', 'not-json')
      const store = useContextStore()
      const defaults = store.getStoredDefaults()
      expect(defaults.teamId).toBeNull()
      expect(defaults.sourceId).toBeNull()
    })

    it('handles missing fields in localStorage gracefully', () => {
      localStorage.setItem('logchef_context', '{}')
      const store = useContextStore()
      const defaults = store.getStoredDefaults()
      expect(defaults.teamId).toBeNull()
      expect(defaults.sourceId).toBeNull()
    })
  })
})
