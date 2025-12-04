import { defineStore } from "pinia";
import { useBaseStore } from "./base";
import { settingsApi, type SystemSetting, type SettingsByCategory, type UpdateSettingRequest } from "@/api/settings";
import { computed } from "vue";

interface SettingsState {
  settingsByCategory: SettingsByCategory[];
}

export const useSettingsStore = defineStore("settings", () => {
  const state = useBaseStore<SettingsState>({
    settingsByCategory: [],
  });

  // Computed properties
  const settingsByCategory = computed(() => state.data.value.settingsByCategory || []);

  // Get all settings as a flat array
  const allSettings = computed(() => {
    return state.data.value.settingsByCategory.flatMap((category) => category.settings);
  });

  // Get settings for a specific category
  const getSettingsByCategory = (category: string) => {
    const categoryData = state.data.value.settingsByCategory.find(
      (c) => c.category === category
    );
    return categoryData?.settings || [];
  };

  // Get a specific setting by key
  const getSetting = (key: string) => {
    return allSettings.value.find((setting) => setting.key === key);
  };

  // Load all settings grouped by category
  async function loadSettings(forceReload = false) {
    return await state.withLoading('loadSettings', async () => {
      // Skip loading if we already have settings and not forcing reload
      if (!forceReload && state.data.value.settingsByCategory && state.data.value.settingsByCategory.length > 0) {
        return { success: true, data: state.data.value.settingsByCategory };
      }

      console.log("Loading settings from API...");
      return await state.callApi({
        apiCall: () => settingsApi.listSettings(),
        operationKey: 'loadSettings',
        onSuccess: (response) => {
          console.log("Settings API response:", response);
          state.data.value.settingsByCategory = response || [];
        },
        showToast: false,
      });
    });
  }

  // Load settings for a specific category
  async function loadSettingsForCategory(category: string) {
    return await state.withLoading(`loadCategory-${category}`, async () => {
      return await state.callApi({
        apiCall: () => settingsApi.listSettingsByCategory(category),
        operationKey: `loadCategory-${category}`,
        onSuccess: (response) => {
          // Update the specific category in our state
          const existingCategoryIndex = state.data.value.settingsByCategory.findIndex(
            (c) => c.category === category
          );

          if (existingCategoryIndex !== -1) {
            state.data.value.settingsByCategory[existingCategoryIndex].settings = response || [];
          } else {
            state.data.value.settingsByCategory.push({
              category,
              settings: response || [],
            });
          }
        },
        showToast: false,
      });
    });
  }

  // Update or create a setting
  async function updateSetting(key: string, data: UpdateSettingRequest) {
    return await state.withLoading(`updateSetting-${key}`, async () => {
      const result = await state.callApi({
        apiCall: () => settingsApi.updateSetting(key, data),
        successMessage: "Setting updated successfully",
        operationKey: `updateSetting-${key}`,
      });

      if (result && result.success) {
        // Reload settings to ensure frontend state is in sync
        await loadSettings(true);
        console.log("Settings reloaded after updating setting");
      }

      return result;
    });
  }

  // Delete a setting
  async function deleteSetting(key: string) {
    return await state.withLoading(`deleteSetting-${key}`, async () => {
      const result = await state.callApi({
        apiCall: () => settingsApi.deleteSetting(key),
        successMessage: "Setting deleted successfully",
        operationKey: `deleteSetting-${key}`,
      });

      if (result && result.success) {
        // Reload settings to ensure frontend state is in sync
        await loadSettings(true);
        console.log("Settings reloaded after deleting setting");
      }

      return result;
    });
  }

  return {
    // State
    settingsByCategory,
    allSettings,
    isLoading: state.isLoading,
    error: state.error,

    // Actions
    loadSettings,
    loadSettingsForCategory,
    updateSetting,
    deleteSetting,

    // Helpers
    getSettingsByCategory,
    getSetting,
    isLoadingOperation: state.isLoadingOperation,
  };
});
