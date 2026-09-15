import { defineStore } from "pinia";
import { computed } from "vue";
import { useBaseStore } from "./base";
import { preferencesApi, type UserPreferences, type UserPreferencesResponse } from "@/api/preferences";
import { useThemeStore, type ThemeMode } from "./theme";
import { useAuthStore } from "./auth";
import { useMetaStore } from "./meta";
import { isLocalePreference } from "@/i18n/locales";

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
  locale: "auto",
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
    locale: isLocalePreference(preferences.locale) ? preferences.locale : "auto",
    theme: isThemeMode(preferences.theme) ? preferences.theme : DEFAULT_PREFERENCES.theme,
    timezone: preferences.timezone === "utc" || preferences.timezone === "local" ? preferences.timezone : DEFAULT_PREFERENCES.timezone,
    display_mode:
      preferences.display_mode === "compact" ||
      preferences.display_mode === "table" ||
      preferences.display_mode === "json"
        ? preferences.display_mode
        : DEFAULT_PREFERENCES.display_mode,
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
  if (legacyDisplayMode === "table" || legacyDisplayMode === "compact" || legacyDisplayMode === "json") {
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
    a.locale === b.locale &&
    a.theme === b.theme &&
    a.timezone === b.timezone &&
    a.display_mode === b.display_mode &&
    a.fields_panel_open === b.fields_panel_open
  );
}

export const usePreferencesStore = defineStore("preferences", () => {
  const themeStore = useThemeStore();
  const authStore = useAuthStore();
  const metaStore = useMetaStore();

  const initialPreferences = readStoredPreferences(themeStore.preference);
  let loadedForUser: string | null | undefined;
  let preferencesRevision = 0;
  let pendingSave: Promise<unknown> = Promise.resolve();
  let pendingLoad: Promise<unknown> | undefined;

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
    const userId = authStore.user?.id ?? null;
    if (loadedForUser !== userId && !metaStore.demoReadOnly) {
      state.data.value.isLoaded = false;
      applyPreferences({ ...preferences.value, locale: "auto" });
      loadedForUser = userId;
    }
    // A public demo uses one shared account, so server preferences would make
    // visitors overwrite each other's theme and explorer defaults. Keep the
    // already-persisted browser copy authoritative in demo mode.
    if (!authStore.isAuthenticated || metaStore.demoReadOnly) {
      state.data.value.isLoaded = true;
      loadedForUser = userId;
      return { success: true, data: preferences.value };
    }

    if (isLoaded.value && loadedForUser === userId && !forceReload) {
      return { success: true, data: preferences.value };
    }

    const revision = ++preferencesRevision;
    const loading = state.withLoading("loadPreferences", async () => {
      // A read during a save must see the saved value, not the old server copy.
      await pendingSave;
      if ((authStore.user?.id ?? null) !== userId || revision !== preferencesRevision) return { success: false };
      const result = await state.callApi<UserPreferencesResponse>({
        apiCall: () => preferencesApi.getPreferences(),
        operationKey: "loadPreferences",
        showToast: false,
      });
      if (!result.success || !result.data || (authStore.user?.id ?? null) !== userId || revision !== preferencesRevision) return result;
      const payload = result.data;
      const serverPreferences = normalizePreferences({ ...DEFAULT_PREFERENCES, ...payload.preferences });
      if (payload.is_default) {
        const merged = normalizePreferences({
          ...serverPreferences,
          ...preferences.value,
          // Do not copy another account's language from this browser.
          locale: serverPreferences.locale,
        });
        applyPreferences(merged);
        state.data.value.isDefault = true;
        if (!preferencesEqual(serverPreferences, merged)) await savePreferences(merged, "syncPreferences");
      } else {
        applyPreferences(serverPreferences);
        state.data.value.isDefault = false;
      }
      if ((authStore.user?.id ?? null) === userId && revision === preferencesRevision) {
        state.data.value.isLoaded = true;
        loadedForUser = userId;
      }
      return result;
    });
    pendingLoad = loading;
    try {
      return await loading;
    } finally {
      if (pendingLoad === loading) pendingLoad = undefined;
    }
  }

  function savePreferences(partial: Partial<UserPreferences>, operationKey: string) {
    const userId = authStore.user?.id;
    const result = pendingSave.then(() => {
      if (!userId || authStore.user?.id !== userId) return { success: false };
      return state.callApi<UserPreferencesResponse>({
        apiCall: () => preferencesApi.updatePreferences(partial),
        operationKey,
        showToast: false,
      });
    });
    pendingSave = result;
    return result;
  }

  async function updatePreferences(partial: Partial<UserPreferences>, options?: { syncTheme?: boolean }) {
    const userId = authStore.user?.id;
    if (userId && !metaStore.demoReadOnly && !isLoaded.value) {
      await (pendingLoad ?? loadPreferences());
      if (authStore.user?.id !== userId) return { success: false };
    }
    preferencesRevision++;
    const next = normalizePreferences({
      ...preferences.value,
      ...partial,
    });

    applyPreferences(next, { syncTheme: options?.syncTheme });

    if (!authStore.isAuthenticated || metaStore.demoReadOnly) {
      return { success: true, data: next };
    }

    return await savePreferences(partial, "updatePreferences");
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
