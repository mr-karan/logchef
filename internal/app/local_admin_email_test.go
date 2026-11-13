package app

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestLocalAdminEmailLength(t *testing.T) {
	for _, tc := range []struct {
		name, email string
		rejected    bool
	}{
		{"255 bytes", strings.Repeat("a", 243) + "@example.com", true},
		{"lowercase grows beyond limit", strings.Repeat("Ⱥ", 121) + "@example.com", true},
		{"lowercase grows within limit", strings.Repeat("Ⱥ", 80) + "@example.com", false},
		{"padded short address", strings.Repeat(" ", 254) + "User@Example.com ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			db, err := sqlite.New(t.Context(), sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: sqlitetest.Path(t)}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			email := tc.email
			cfg := &config.Config{Auth: config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true, AdminEmail: email}}}
			err = ensureLocalAdmin(t.Context(), cfg, db, log)
			if !tc.rejected {
				if err != nil {
					t.Fatal(err)
				}
				saved, err := db.GetUserByEmail(t.Context(), email)
				if err != nil {
					t.Fatal(err)
				}
				if want := strings.ToLower(strings.TrimSpace(email)); saved.Email != want {
					t.Fatalf("stored email = %q, want %q", saved.Email, want)
				}
				return
			}
			var validation *core.ValidationError
			if !errors.As(err, &validation) || validation.Field != "email" || validation.Message != fmt.Sprintf("email must be at most %d bytes", models.MaxEmailLength) {
				t.Fatalf("oversized local admin error = %v", err)
			}
			if _, err := db.GetUserByEmail(t.Context(), email); !models.IsNotFound(err) {
				t.Fatal("oversized local admin was persisted")
			}
		})
	}
}
