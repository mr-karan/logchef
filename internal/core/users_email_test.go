package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestAccountEmailLength(t *testing.T) {
	for _, path := range []struct {
		name  string
		apply func(*testing.T, string) error
	}{
		{"create", func(t *testing.T, email string) error {
			db := newTestDB(t)
			_, err := CreateUser(t.Context(), db, discardLogger(), access.SystemPrincipal(), email, "Test User", models.UserRoleMember, models.UserStatusActive)
			if err != nil {
				if _, lookupErr := db.GetUserByEmail(t.Context(), email); !models.IsNotFound(lookupErr) {
					t.Fatal("rejected email was persisted")
				}
			}
			return err
		}},
		{"OIDC auto-provision", func(t *testing.T, email string) error {
			db := newTestDB(t)
			_, err := GetOrCreateAutoProvisionedUser(t.Context(), db, discardLogger(), email, "Test User")
			if err != nil {
				if _, lookupErr := db.GetUserByEmail(t.Context(), email); !models.IsNotFound(lookupErr) {
					t.Fatal("rejected email was persisted")
				}
			}
			return err
		}},
		{"update", func(t *testing.T, email string) error {
			db := newTestDB(t)
			user := newTestUser(t, db, "existing@example.com", "Existing User")
			err := UpdateUser(t.Context(), db, discardLogger(), access.SystemPrincipal(), user.ID, models.User{Email: email, FullName: "Changed User"})
			saved, lookupErr := db.GetUser(t.Context(), user.ID)
			if lookupErr != nil {
				t.Fatal(lookupErr)
			}
			if err != nil && saved.FullName != "Existing User" {
				t.Fatal("rejected update changed the user")
			}
			return err
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
		} {
			t.Run(path.name+"/"+tc.name, func(t *testing.T) {
				err := path.apply(t, tc.email)
				if !tc.rejected {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				var validation *ValidationError
				if !errors.As(err, &validation) || validation.Field != "email" || validation.Message != "email must be at most 254 bytes" {
					t.Fatalf("oversized email error = %v, want email length validation error", err)
				}
			})
		}
	}
}

func TestInitAdminUsersEmailLength(t *testing.T) {
	db := newTestDB(t)
	email := strings.Repeat("a", 243) + "@example.com"
	err := InitAdminUsers(t.Context(), db, discardLogger(), []string{email})
	if err == nil || !strings.Contains(err.Error(), "email must be at most 254 bytes") {
		t.Fatalf("oversized bootstrap admin error = %v", err)
	}
	if _, err := db.GetUserByEmail(t.Context(), email); !models.IsNotFound(err) {
		t.Fatal("oversized bootstrap admin was persisted")
	}
}
