import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { api } from '@/api/config'
import { useMetaStore } from '../meta'
import { useThemeStore } from '../theme'
import { usePreferencesStore } from '../preferences'

describe('theme ownership', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.className = ''
    setActivePinia(createPinia())
  })

  it('keeps system mode as a preference instead of persisting the resolved color', () => {
    const theme = useThemeStore()
    expect(theme.preference).toBe('auto')
  })

  it.each(['logged out', 'demo'])('migrates a base-era auto theme for %s users', async (user) => {
    localStorage.setItem('vueuse-color-scheme', 'dark')
    localStorage.setItem('logchef_user_preferences', JSON.stringify({ theme: 'auto' }))
    if (user === 'demo') {
      const adapter = api.defaults.adapter
      api.defaults.adapter = async (config) => ({
        status: 200, statusText: 'OK', headers: {}, config,
        data: { status: 'success', data: { version: 'test', http_server_timeout: '30s', max_query_limit: 100, demo_read_only: true } },
      })
      try { await useMetaStore().loadMeta() } finally { api.defaults.adapter = adapter }
      expect(useMetaStore().demoReadOnly).toBe(true)
    }
    const preferences = usePreferencesStore()
    await preferences.loadPreferences()
    await nextTick()
    expect(useThemeStore().preference).toBe('auto')
    expect(preferences.preferences.theme).toBe('auto')
    expect(localStorage.getItem('vueuse-color-scheme')).toBe('auto')
    expect(JSON.parse(localStorage.getItem('logchef_user_preferences') ?? '{}')).not.toHaveProperty('theme')
  })

  it('stores the theme once and restores it after a reload', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const theme = useThemeStore()
    const preferences = usePreferencesStore()
    await preferences.updatePreferences({ theme: 'dark' })
    await nextTick()
    expect(theme.preference).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('vueuse-color-scheme')).toBe('dark')
    expect(JSON.parse(localStorage.getItem('logchef_user_preferences') ?? '{}')).not.toHaveProperty('theme')
    theme.setTheme('light')
    await nextTick()
    expect(preferences.preferences.theme).toBe('light')
    disposePinia(pinia)
    setActivePinia(createPinia())
    expect(useThemeStore().preference).toBe('light')
    expect(usePreferencesStore().preferences.theme).toBe('light')
  })
})
