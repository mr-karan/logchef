package models

// ThemePreference represents the UI theme preference.
type ThemePreference string

const (
	ThemePreferenceLight ThemePreference = "light"
	ThemePreferenceDark  ThemePreference = "dark"
	ThemePreferenceAuto  ThemePreference = "auto"
)

// TimezonePreference represents the preferred timezone display.
type TimezonePreference string

const (
	TimezonePreferenceLocal TimezonePreference = "local"
	TimezonePreferenceUTC   TimezonePreference = "utc"
)

// DisplayModePreference represents the preferred log display mode.
type DisplayModePreference string

const (
	DisplayModeTable   DisplayModePreference = "table"
	DisplayModeCompact DisplayModePreference = "compact"
)

// UserPreferences represents persisted user preferences.
type UserPreferences struct {
	Theme           ThemePreference       `json:"theme"`
	Timezone        TimezonePreference    `json:"timezone"`
	DisplayMode     DisplayModePreference `json:"display_mode"`
	FieldsPanelOpen bool                  `json:"fields_panel_open"`
}

// UpdateUserPreferencesRequest represents a partial update to user preferences.
type UpdateUserPreferencesRequest struct {
	Theme           *ThemePreference       `json:"theme,omitempty"`
	Timezone        *TimezonePreference    `json:"timezone,omitempty"`
	DisplayMode     *DisplayModePreference `json:"display_mode,omitempty"`
	FieldsPanelOpen *bool                  `json:"fields_panel_open,omitempty"`
}
