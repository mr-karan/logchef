package provisioning

import (
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestReconcileEmailLength(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) store.Store
	}{
		{"sqlite", func(t *testing.T) store.Store { return newReconcileTestDB(t) }},
		{"postgres", func(t *testing.T) store.Store { db, _, _ := newProvisioningPostgres(t); return db }},
	} {
		for _, tc := range []struct {
			name, email string
			rejected    bool
		}{
			{"254 bytes", strings.Repeat("a", 242) + "@example.com", false},
			{"255 bytes", strings.Repeat("a", 243) + "@example.com", true},
			{"lowercase grows beyond limit", strings.Repeat("Ⱥ", 121) + "@example.com", true},
			{"lowercase grows within limit", strings.Repeat("Ⱥ", 80) + "@example.com", false},
			{"padded short address", strings.Repeat(" ", 254) + "User@Example.com ", false},
			{"multibyte 256 bytes", strings.Repeat("é", 122) + "@example.com", true},
		} {
			t.Run(backend.name+"/"+tc.name, func(t *testing.T) {
				db := backend.open(t)
				cfg := &config.ProvisioningConfig{ManageTeams: true, Teams: []config.ProvisionTeam{{Name: "Team", Members: []config.ProvisionMember{{Email: tc.email}}}}}
				err := Reconcile(t.Context(), cfg, db, nil, quietLogger(), nil)
				if !tc.rejected {
					if err != nil {
						t.Fatal(err)
					}
					saved, err := db.GetUserByEmail(t.Context(), tc.email)
					if err != nil {
						t.Fatal(err)
					}
					if want := strings.ToLower(strings.TrimSpace(tc.email)); saved.Email != want {
						t.Fatalf("stored email = %q, want %q", saved.Email, want)
					}
					return
				}
				if err == nil || !strings.Contains(err.Error(), "members[0]: email must be at most 254 bytes") {
					t.Fatalf("oversized provisioned email error = %v", err)
				}
				if _, err := db.GetUserByEmail(t.Context(), tc.email); !models.IsNotFound(err) {
					t.Fatal("oversized provisioned user was persisted")
				}
				teams, err := db.ListTeams(t.Context())
				if err != nil || len(teams) != 0 {
					t.Fatal("invalid provisioning config mutated teams")
				}
			})
		}
	}
}
