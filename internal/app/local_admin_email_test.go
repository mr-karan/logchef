package app

import (
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestLocalAdminEmailLength(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sqlite.New(t.Context(), sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "local-admin.db")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	email := strings.Repeat("a", 243) + "@example.com"
	cfg := &config.Config{Auth: config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true, AdminEmail: email}}}
	err = ensureLocalAdmin(t.Context(), cfg, db, log)
	var validation *core.ValidationError
	if !errors.As(err, &validation) || validation.Field != "email" || validation.Message != "email must be at most 254 bytes" {
		t.Fatalf("oversized local admin error = %v", err)
	}
	if _, err := db.GetUserByEmail(t.Context(), email); !models.IsNotFound(err) {
		t.Fatal("oversized local admin was persisted")
	}
}
