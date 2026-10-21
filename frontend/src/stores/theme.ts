import { defineStore } from "pinia";
import { computed } from "vue";
import { useColorMode } from "@vueuse/core";

export type ThemeMode = "light" | "dark" | "auto";

export const useThemeStore = defineStore("theme", () => {
  const colorMode = useColorMode({ emitAuto: true });
  const preference = computed(() => colorMode.value);
  const isDark = computed(() => colorMode.state.value === "dark");

  function setTheme(mode: ThemeMode) {
    colorMode.value = mode;
  }

  return { preference, isDark, setTheme };
});
