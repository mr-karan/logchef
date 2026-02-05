package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// DefaultUserPreferences defines the baseline preferences for users.
var DefaultUserPreferences = models.UserPreferences{
	Theme:           models.ThemePreferenceAuto,
	Timezone:        models.TimezonePreferenceLocal,
	DisplayMode:     models.DisplayModeTable,
	FieldsPanelOpen: true,
}

// GetUserPreferences returns stored preferences for a user.
// If none are stored, defaults are returned with isDefault=true.
func GetUserPreferences(ctx context.Context, db *sqlite.DB, userID models.UserID) (models.UserPreferences, bool, error) {
	prefsJSON, err := db.GetUserPreferencesJSON(ctx, userID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return DefaultUserPreferences, true, nil
		}
		return DefaultUserPreferences, false, fmt.Errorf("failed to load user preferences: %w", err)
	}

	trimmed := strings.TrimSpace(prefsJSON)
	if trimmed == "" || trimmed == "{}" {
		return DefaultUserPreferences, true, nil
	}

	var stored models.UserPreferences
	if err := json.Unmarshal([]byte(prefsJSON), &stored); err != nil {
		return DefaultUserPreferences, true, fmt.Errorf("failed to parse user preferences: %w", err)
	}

	return normalizeUserPreferences(stored), false, nil
}

// UpdateUserPreferences applies updates and persists user preferences.
func UpdateUserPreferences(ctx context.Context, db *sqlite.DB, userID models.UserID, update models.UpdateUserPreferencesRequest) (models.UserPreferences, error) {
	if err := validateUserPreferencesUpdate(update); err != nil {
		return DefaultUserPreferences, err
	}

	current, _, err := GetUserPreferences(ctx, db, userID)
	if err != nil {
		return DefaultUserPreferences, err
	}

	next := applyUserPreferencesUpdate(current, update)

	payload, err := json.Marshal(next)
	if err != nil {
		return DefaultUserPreferences, fmt.Errorf("failed to serialize user preferences: %w", err)
	}

	if err := db.UpsertUserPreferencesJSON(ctx, userID, string(payload)); err != nil {
		return DefaultUserPreferences, err
	}

	return next, nil
}

func applyUserPreferencesUpdate(current models.UserPreferences, update models.UpdateUserPreferencesRequest) models.UserPreferences {
	next := current
	if update.Theme != nil {
		next.Theme = *update.Theme
	}
	if update.Timezone != nil {
		next.Timezone = *update.Timezone
	}
	if update.DisplayMode != nil {
		next.DisplayMode = *update.DisplayMode
	}
	if update.FieldsPanelOpen != nil {
		next.FieldsPanelOpen = *update.FieldsPanelOpen
	}
	return normalizeUserPreferences(next)
}

func validateUserPreferencesUpdate(update models.UpdateUserPreferencesRequest) error {
	if update.Theme != nil && !isValidThemePreference(*update.Theme) {
		return &ValidationError{Field: "theme", Message: "theme must be one of: light, dark, auto"}
	}
	if update.Timezone != nil && !isValidTimezonePreference(*update.Timezone) {
		return &ValidationError{Field: "timezone", Message: "timezone must be one of: local, utc"}
	}
	if update.DisplayMode != nil && !isValidDisplayModePreference(*update.DisplayMode) {
		return &ValidationError{Field: "display_mode", Message: "display_mode must be one of: table, compact"}
	}
	return nil
}

func normalizeUserPreferences(prefs models.UserPreferences) models.UserPreferences {
	normalized := prefs

	if !isValidThemePreference(normalized.Theme) {
		normalized.Theme = DefaultUserPreferences.Theme
	}
	if !isValidTimezonePreference(normalized.Timezone) {
		normalized.Timezone = DefaultUserPreferences.Timezone
	}
	if !isValidDisplayModePreference(normalized.DisplayMode) {
		normalized.DisplayMode = DefaultUserPreferences.DisplayMode
	}

	return normalized
}

func isValidThemePreference(value models.ThemePreference) bool {
	switch value {
	case models.ThemePreferenceLight, models.ThemePreferenceDark, models.ThemePreferenceAuto:
		return true
	default:
		return false
	}
}

func isValidTimezonePreference(value models.TimezonePreference) bool {
	switch value {
	case models.TimezonePreferenceLocal, models.TimezonePreferenceUTC:
		return true
	default:
		return false
	}
}

func isValidDisplayModePreference(value models.DisplayModePreference) bool {
	switch value {
	case models.DisplayModeTable, models.DisplayModeCompact:
		return true
	default:
		return false
	}
}
