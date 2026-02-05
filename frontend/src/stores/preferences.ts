import { defineStore } from "pinia";
import { computed } from "vue";
import { useBaseStore } from "./base";
import { preferencesApi, type UserPreferences, type UserPreferencesResponse } from "@/api/preferences";
import { useThemeStore, type ThemeMode } from "./theme";
import { useAuthStore } from "./auth";

interface PreferencesState {
  preferences: UserPreferences;
  isLoaded: boolean;
  isDefault: boolean;
}

const STORAGE_KEY = "logchef_user_preferences";
const LEGACY_TIMEZONE_KEY = "logchef_timezone";
const LEGACY_DISPLAY_MODE_KEY = "logchef_display_mode";
const LEGACY_FIELDS_PANEL_KEY = "logchef_fields_panel";

const DEFAULT_PREFERENCES: UserPreferences = {
  theme: "auto",
  timezone: "local",
  display_mode: "table",
  fields_panel_open: true,
};

function isThemeMode(value: string): value is ThemeMode {
  return value === "light" || value === "dark" || value === "auto";
}

function normalizePreferences(preferences: UserPreferences): UserPreferences {
  return {
    theme: isThemeMode(preferences.theme) ? preferences.theme : DEFAULT_PREFERENCES.theme,
    timezone: preferences.timezone === "utc" || preferences.timezone === "local" ? preferences.timezone : DEFAULT_PREFERENCES.timezone,
    display_mode: preferences.display_mode === "compact" || preferences.display_mode === "table" ? preferences.display_mode : DEFAULT_PREFERENCES.display_mode,
    fields_panel_open: typeof preferences.fields_panel_open === "boolean" ? preferences.fields_panel_open : DEFAULT_PREFERENCES.fields_panel_open,
  };
}

function readStoredPreferences(themeFallback: ThemeMode): UserPreferences {
  if (typeof window === "undefined") {
    return { ...DEFAULT_PREFERENCES, theme: themeFallback };
  }

  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored) {
    try {
      const parsed = JSON.parse(stored) as Partial<UserPreferences>;
      return normalizePreferences({
        ...DEFAULT_PREFERENCES,
        theme: themeFallback,
        ...parsed,
      } as UserPreferences);
    } catch (error) {
      console.warn("Failed to parse stored preferences, using defaults.", error);
    }
  }

  const preferences: UserPreferences = { ...DEFAULT_PREFERENCES, theme: themeFallback };

  const legacyTimezone = localStorage.getItem(LEGACY_TIMEZONE_KEY);
  if (legacyTimezone === "utc" || legacyTimezone === "local") {
    preferences.timezone = legacyTimezone;
  }

  const legacyDisplayMode = localStorage.getItem(LEGACY_DISPLAY_MODE_KEY);
  if (legacyDisplayMode === "table" || legacyDisplayMode === "compact") {
    preferences.display_mode = legacyDisplayMode;
  }

  const legacyFieldsPanel = localStorage.getItem(LEGACY_FIELDS_PANEL_KEY);
  if (legacyFieldsPanel === "open") {
    preferences.fields_panel_open = true;
  } else if (legacyFieldsPanel === "closed") {
    preferences.fields_panel_open = false;
  }

  return preferences;
}

function persistPreferences(preferences: UserPreferences) {
  if (typeof window === "undefined") return;
  localStorage.setItem(STORAGE_KEY, JSON.stringify(preferences));
}

function persistLegacyKeys(preferences: UserPreferences) {
  if (typeof window === "undefined") return;
  localStorage.setItem(LEGACY_TIMEZONE_KEY, preferences.timezone);
  localStorage.setItem(LEGACY_DISPLAY_MODE_KEY, preferences.display_mode);
  localStorage.setItem(LEGACY_FIELDS_PANEL_KEY, preferences.fields_panel_open ? "open" : "closed");
}

function preferencesEqual(a: UserPreferences, b: UserPreferences) {
  return (
    a.theme === b.theme &&
    a.timezone === b.timezone &&
    a.display_mode === b.display_mode &&
    a.fields_panel_open === b.fields_panel_open
  );
}

export const usePreferencesStore = defineStore("preferences", () => {
  const themeStore = useThemeStore();
  const authStore = useAuthStore();

  const initialPreferences = readStoredPreferences(themeStore.preference);

  const state = useBaseStore<PreferencesState>({
    preferences: initialPreferences,
    isLoaded: false,
    isDefault: false,
  });

  const preferences = computed(() => state.data.value.preferences);
  const isLoaded = computed(() => state.data.value.isLoaded);
  const isDefault = computed(() => state.data.value.isDefault);

  function applyPreferences(next: UserPreferences, options?: { syncTheme?: boolean }) {
    const normalized = normalizePreferences(next);
    state.data.value.preferences = normalized;
    persistPreferences(normalized);
    persistLegacyKeys(normalized);

    if (options?.syncTheme !== false && themeStore.preference !== normalized.theme) {
      themeStore.setTheme(normalized.theme);
    }
  }

  applyPreferences(state.data.value.preferences, { syncTheme: false });

  async function loadPreferences(forceReload = false) {
    if (!authStore.isAuthenticated) {
      state.data.value.isLoaded = true;
      return { success: true, data: preferences.value };
    }

    if (isLoaded.value && !forceReload) {
      return { success: true, data: preferences.value };
    }

    return await state.withLoading("loadPreferences", async () => {
      return await state.callApi<UserPreferencesResponse>({
        apiCall: () => preferencesApi.getPreferences(),
        operationKey: "loadPreferences",
        showToast: false,
        onSuccess: async (response) => {
          const payload = response as UserPreferencesResponse | null;
          if (!payload) {
            state.data.value.isLoaded = true;
            return;
          }

          const serverPreferences = normalizePreferences({
            ...DEFAULT_PREFERENCES,
            ...payload.preferences,
          });

          if (payload.is_default) {
            const localPreferences = normalizePreferences(preferences.value);
            const merged = normalizePreferences({
              ...serverPreferences,
              ...localPreferences,
            });

            applyPreferences(merged);
            state.data.value.isDefault = true;

            if (!preferencesEqual(serverPreferences, merged)) {
              await syncPreferencesToServer(merged);
            }
          } else {
            applyPreferences(serverPreferences);
            state.data.value.isDefault = false;
          }

          state.data.value.isLoaded = true;
        },
      });
    });
  }

  async function syncPreferencesToServer(next: UserPreferences) {
    if (!authStore.isAuthenticated) {
      return { success: true, data: next };
    }

    return await state.callApi<UserPreferencesResponse>({
      apiCall: () => preferencesApi.updatePreferences(next),
      operationKey: "syncPreferences",
      showToast: false,
    });
  }

  async function updatePreferences(partial: Partial<UserPreferences>, options?: { syncTheme?: boolean }) {
    const next = normalizePreferences({
      ...preferences.value,
      ...partial,
    } as UserPreferences);

    applyPreferences(next, { syncTheme: options?.syncTheme });

    if (!authStore.isAuthenticated) {
      return { success: true, data: next };
    }

    return await state.callApi<UserPreferencesResponse>({
      apiCall: () => preferencesApi.updatePreferences(partial),
      operationKey: "updatePreferences",
      successMessage: "Preferences updated",
      showToast: false,
    });
  }

  return {
    preferences,
    isLoaded,
    isDefault,
    isLoading: state.isLoading,
    error: state.error,
    loadPreferences,
    updatePreferences,
    isLoadingOperation: state.isLoadingOperation,
  };
});
