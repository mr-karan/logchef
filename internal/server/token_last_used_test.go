package server

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestTokenAuthenticationCoalescesBlockedWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.db")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sqlite.New(context.Background(), sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	cfg := testOAuthConfig()
	srv := newServerForTest(t, cfg, db, nil)
	user := mkTestUser(t, db, "token-worker@example.com", models.UserRoleMember)
	token, err := core.CreateAPIToken(context.Background(), db, log, &cfg.Auth, user.ID, "worker", nil, []models.TokenScope{models.TokenScopeProfileRead})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(context.Background(), `CREATE TABLE token_writes (id INTEGER); CREATE TRIGGER count_token_writes AFTER UPDATE OF last_used_at ON api_tokens BEGIN INSERT INTO token_writes VALUES (NEW.id); END;`); err != nil {
		t.Fatal(err)
	}
	tx, err := raw.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), "INSERT INTO token_writes VALUES (0)"); err != nil {
		t.Fatal(err)
	}
	released := make(chan error, 1)
	go func() { time.Sleep(500 * time.Millisecond); released <- tx.Rollback() }()
	for range 32 {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token.Token)
		start := time.Now()
		resp, err := srv.app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("auth status = %d", resp.StatusCode)
		}
		if elapsed := time.Since(start); elapsed >= 80*time.Millisecond {
			t.Errorf("token request waited %s for blocked metadata write", elapsed)
		}
	}
	if err := <-released; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := db.GetAPIToken(context.Background(), token.APIToken.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.LastUsedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("token metadata was never persisted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var count int
	if err := raw.QueryRowContext(context.Background(), "SELECT count(*) FROM token_writes").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("last-used writes = %d, want 1 for 32 requests within 60 seconds", count)
	}
}
