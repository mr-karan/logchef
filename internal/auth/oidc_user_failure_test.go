package auth

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
)

func TestCallbackUserFailureIncludesEmail(t *testing.T) {
	for _, failure := range []string{"lookup", "auto-provision"} {
		t.Run(failure, func(t *testing.T) {
			fake := newFakeOIDCServer(t)
			const email = "user@example.com"
			fake.registerCode("code", OIDCClaims{Email: email, Name: "Test User", EmailVerified: new(true)}, "nonce")
			log := discardLoggerAP()
			p, err := NewOIDCProvider(t.Context(), testOIDCConfig(fake), log)
			if err != nil {
				t.Fatal(err)
			}
			path := sqlitetest.Path(t)
			db, err := sqlite.New(t.Context(), sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: path}})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			rawDB, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer rawDB.Close()
			query := `DROP TABLE users`
			if failure == "auto-provision" {
				query = `CREATE TRIGGER fail_user BEFORE INSERT ON users BEGIN SELECT RAISE(ABORT, 'user insert failed'); END`
			}
			if _, err := rawDB.ExecContext(t.Context(), query); err != nil {
				t.Fatal(err)
			}
			cfg := &config.AuthConfig{AutoProvision: config.AutoProvisionConfig{Enabled: true, AllowedDomains: []string{"example.com"}}}
			_, _, err = p.HandleCallback(t.Context(), db, log, cfg, "code", "nonce", "test-pkce-verifier")
			if err == nil || !strings.HasPrefix(err.Error(), "failed to "+failure+" user for email="+email+":") {
				t.Fatalf("%s failure lost email context: %v", failure, err)
			}
		})
	}
}
