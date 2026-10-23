package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestAccountEmailLength(t *testing.T) {
	for _, path := range []struct {
		name  string
		apply func(*testing.T, store.Store, string) error
	}{
		{"create", func(t *testing.T, db store.Store, email string) error {
			_, err := CreateUser(t.Context(), db, discardLogger(), access.SystemPrincipal(), email, "Test User", models.UserRoleMember, models.UserStatusActive)
			return err
		}},
		{"OIDC auto-provision", func(t *testing.T, db store.Store, email string) error {
			_, err := GetOrCreateAutoProvisionedUser(t.Context(), db, discardLogger(), email, "Test User")
			return err
		}},
		{"bootstrap admin", func(t *testing.T, db store.Store, email string) error {
			return InitAdminUsers(t.Context(), db, discardLogger(), []string{email})
		}},
	} {
		for _, tc := range []struct {
			name, email string
			rejected    bool
		}{
			{"254 bytes", strings.Repeat("a", 242) + "@example.com", false},
			{"255 bytes", strings.Repeat("a", 243) + "@example.com", true},
			{"multibyte 254 bytes", strings.Repeat("é", 121) + "@example.com", false},
			{"multibyte 256 bytes", strings.Repeat("é", 122) + "@example.com", true},
			{"lowercase grows beyond limit", strings.Repeat("Ⱥ", 121) + "@example.com", true},
			{"lowercase grows within limit", strings.Repeat("Ⱥ", 80) + "@example.com", false},
			{"padded short address", strings.Repeat(" ", 254) + "User@Example.com ", false},
		} {
			t.Run(path.name+"/"+tc.name, func(t *testing.T) {
				db := newTestDB(t)
				err := path.apply(t, db, tc.email)
				saved, lookupErr := db.GetUserByEmail(t.Context(), tc.email)
				if saved != nil {
					t.Logf("raw email bytes = %d, stored email bytes = %d", len(tc.email), len(saved.Email))
				}
				if tc.rejected {
					message := fmt.Sprintf("email must be at most %d bytes", models.MaxEmailLength)
					if err == nil || !strings.Contains(err.Error(), "email: "+message) {
						t.Fatalf("oversized email error = %v, want email length validation error", err)
					}
					if !models.IsNotFound(lookupErr) {
						t.Fatalf("rejected email lookup error = %v, want not found", lookupErr)
					}
					t.Logf("raw email bytes = %d, normalized email bytes = %d, rejected without persistence", len(tc.email), len(strings.ToLower(strings.TrimSpace(tc.email))))
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if lookupErr != nil {
					t.Fatal(lookupErr)
				}
				if want := strings.ToLower(strings.TrimSpace(tc.email)); saved.Email != want {
					t.Fatalf("stored email = %q, want %q", saved.Email, want)
				}
			})
		}
	}
}
