import { apiClient } from "./apiUtils";
import type { ThemeMode } from "@/stores/theme";

export type TimezonePreference = "local" | "utc";
export type DisplayModePreference = "table" | "compact";

export interface UserPreferences {
  theme: ThemeMode;
  timezone: TimezonePreference;
  display_mode: DisplayModePreference;
  fields_panel_open: boolean;
}

export interface UserPreferencesResponse {
  preferences: UserPreferences;
  is_default: boolean;
}

export type UpdateUserPreferencesRequest = Partial<UserPreferences>;

export const preferencesApi = {
  getPreferences: () => apiClient.get<UserPreferencesResponse>("/me/preferences"),
  updatePreferences: (data: UpdateUserPreferencesRequest) =>
    apiClient.put<UserPreferencesResponse>("/me/preferences", data),
};
