package storetest

import (
	"context"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
)

// ReportExternalSettingsSeed is portable SQL that seeds settings at the
// schema version before add_report_external_settings (RFC 0008 unit R9).
var ReportExternalSettingsSeed = []string{
	`INSERT INTO system_settings (key, value, value_type, category, description, is_sensitive) VALUES
		('server.frontend_url', 'https://logs.example.com', 'string', 'server', 'Frontend', FALSE)`,
}

// CheckReportExternalSettings asserts what the migration did to a table
// that had rows: they keep their values, both report settings start off
// and empty, and the reports category is accepted.
func CheckReportExternalSettings(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()
	for key, want := range map[string]string{
		"server.frontend_url":           "https://logs.example.com",
		"reports.external_recipients":   "false",
		"reports.allowed_email_domains": "",
	} {
		if got, err := s.GetSetting(ctx, key); err != nil || got != want {
			t.Errorf("setting %s = %q, %v; want %q", key, got, err, want)
		}
	}
	settings, err := s.ListSettingsByCategory(ctx, "reports")
	if err != nil || len(settings) != 2 {
		t.Errorf("reports settings = %+v, %v; want the two external-recipient settings", settings, err)
	}
	if err := s.UpsertSetting(ctx, "reports.allowed_email_domains", "example.com", "string", "reports", "", false); err != nil {
		t.Errorf("a reports setting is refused: %v", err)
	}
	if err := s.UpsertSetting(ctx, "x.y", "1", "string", "elsewhere", "", false); err == nil {
		t.Error("the category check accepted an unknown category")
	}
}
