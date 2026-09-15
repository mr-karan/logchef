package core

import (
	"context"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestUserPreferencesLocale(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	user := newTestUser(t, db, "language@example.test", "Language test")
	other := newTestUser(t, db, "other@example.test", "Other user")
	ctx := context.Background()

	// Existing JSON without a language must remain readable without a migration.
	if err := db.UpsertUserPreferencesJSON(ctx, user.ID, `{"theme":"dark","timezone":"utc","display_mode":"json","fields_panel_open":true}`); err != nil {
		t.Fatal(err)
	}
	prefs, _, err := GetUserPreferences(ctx, db, user.ID)
	if err != nil || prefs.Locale != "auto" || prefs.Theme != models.ThemePreferenceDark {
		t.Fatalf("legacy preferences: %+v, %v", prefs, err)
	}
	for _, locale := range []models.LocalePreference{"en", "zh-CN", "zh-TW", "es", "fr", "de", "pt-BR", "ja", "ko", "hi", "it", "auto"} {
		t.Run(string(locale), func(t *testing.T) {
			updated, err := UpdateUserPreferences(ctx, db, user.ID, models.UpdateUserPreferencesRequest{Locale: &locale})
			if err != nil || updated.Locale != locale || updated.Timezone != models.TimezonePreferenceUTC {
				t.Fatalf("updated preferences: %+v, %v", updated, err)
			}
			stored, _, err := GetUserPreferences(ctx, db, user.ID)
			if err != nil || stored != updated {
				t.Fatalf("round trip: %+v, %v", stored, err)
			}
		})
	}
	for _, locale := range []models.LocalePreference{"", "../en", "unsupported", "EN", "zh"} {
		if _, err := UpdateUserPreferences(ctx, db, user.ID, models.UpdateUserPreferencesRequest{Locale: &locale}); err == nil {
			t.Errorf("accepted unsupported locale %q", locale)
		}
	}
	otherPrefs, _, err := GetUserPreferences(ctx, db, other.ID)
	if err != nil || otherPrefs.Locale != "auto" {
		t.Fatalf("changed another user's preferences: %+v, %v", otherPrefs, err)
	}
}
