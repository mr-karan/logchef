package app

import (
	"io"
	"log/slog"
	"strconv"
	"testing"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/pkg/models"
)

// RFC 0009 records each user that startup creates as user.create by the
// system actor; a later start that finds the admin records nothing.
func TestEnsureLocalAdminAuditsCreate(t *testing.T) {
	ctx := t.Context()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sqlite.New(ctx, sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: sqlitetest.Path(t)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rec := audit.New(audit.Options{Store: db, Logger: log})
	cfg := &config.Config{Auth: config.AuthConfig{Local: config.LocalAuthConfig{
		Enabled: true, AdminEmail: "admin@example.com", AdminPassword: "correct horse battery",
	}}}

	if err := ensureLocalAdmin(ctx, cfg, db, rec, log); err != nil {
		t.Fatal(err)
	}
	admin, err := db.GetUserByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	events, err := db.ListAuditEvents(ctx, models.AuditFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("stored %d events, want one user.create: %+v", len(events), events)
	}
	e := events[0]
	if e.Action != string(audit.UserCreate) || e.Outcome != models.AuditOutcomeSuccess ||
		e.TargetType != "user" || e.TargetID != strconv.Itoa(int(admin.ID)) || e.TargetLabel != "admin@example.com" {
		t.Errorf("event = %s %s on %s %s (%s), want a successful user.create on user %d",
			e.Action, e.Outcome, e.TargetType, e.TargetID, e.TargetLabel, admin.ID)
	}
	if e.ActorType != models.AuditActorSystem || e.AuthMethod != models.AuditAuthStartup || e.ActorID != nil || e.ActorLabel != "auth.local" {
		t.Errorf("actor = %s/%s/%v/%q, want system/startup/nil/auth.local", e.ActorType, e.AuthMethod, e.ActorID, e.ActorLabel)
	}

	if err := ensureLocalAdmin(ctx, cfg, db, rec, log); err != nil {
		t.Fatal(err)
	}
	if again, _ := db.ListAuditEvents(ctx, models.AuditFilter{Limit: 10}); len(again) != 1 {
		t.Errorf("a second start left %d events, want the one from the first", len(again))
	}
}
