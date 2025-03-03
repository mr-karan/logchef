import { defineStore } from 'pinia'
import { ref } from 'vue'
import { useColorMode } from '@vueuse/core'

// Possible theme mode values
export type ThemeMode = 'light' | 'dark' | 'auto'

export const useThemeStore = defineStore('theme', () => {
  // Initialize using vueuse's useColorMode
  const colorMode = useColorMode()
  
  // The user's selected preference - may differ from actual mode when 'auto' is selected
  const preference = ref<ThemeMode>(colorMode.value as ThemeMode || 'auto')
  
  // Set the theme preference
  function setTheme(mode: ThemeMode) {
    preference.value = mode
    colorMode.value = mode
  }
  
  // Return current theme and setter
  return {
    preference,
    setTheme
  }
}, {
  // Enable persistence between page refreshes
  persist: {
    key: 'logchef-theme',
    storage: localStorage
  }
})