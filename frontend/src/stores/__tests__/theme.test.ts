import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
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
