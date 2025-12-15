import { defineStore } from "pinia";
import { computed } from "vue";
import { useColorMode } from "@vueuse/core";
import { useBaseStore } from "./base";

// Possible theme mode values
export type ThemeMode = "light" | "dark" | "auto";

interface ThemeState {
  preference: ThemeMode;
}

export const useThemeStore = defineStore("theme", () => {
  // Initialize base store with default state
  const state = useBaseStore<ThemeState>({
    preference: "auto",
  });

  // Initialize using vueuse's useColorMode
  const colorMode = useColorMode();
  
  // Initialize the theme preference from color mode
  state.data.value.preference = (colorMode.value as ThemeMode) || "auto";

  // Computed property for preference
  const preference = computed(() => state.data.value.preference);

  // Set the theme preference
  function setTheme(mode: ThemeMode) {
    state.data.value.preference = mode;
    colorMode.value = mode;
  }

  // Return current theme and setter
  return {
    preference,
    setTheme,
  };
});
