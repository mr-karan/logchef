package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestAdminUserEmailLength(t *testing.T) {
	for _, tc := range []struct {
		name, email string
		rejected    bool
	}{
		{"254 bytes", strings.Repeat("a", 242) + "@example.com", false},
		{"255 bytes", strings.Repeat("a", 243) + "@example.com", true},
		{"lowercase grows beyond limit", strings.Repeat("Ⱥ", 121) + "@example.com", true},
		{"lowercase grows within limit", strings.Repeat("Ⱥ", 80) + "@example.com", false},
		{"padded short address", strings.Repeat(" ", 254) + "User@Example.com ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newAuthzTestServer(t)
			admin := mkTestUser(t, s.store, "admin@example.com", models.UserRoleAdmin)
			body, err := json.Marshal(map[string]string{"email": tc.email, "full_name": "Test User", "role": "member", "status": "active"})
			if err != nil {
				t.Fatal(err)
			}
			want := fiber.StatusCreated
			if tc.rejected {
				want = fiber.StatusBadRequest
			}
			resp := doAuthzRequest(t, s, http.MethodPost, "/api/v1/admin/users", string(body), withSession(sessionCookie(t, s.store, admin)))
			if resp.status != want {
				t.Fatalf("status = %d, want %d", resp.status, want)
			}
			saved, err := s.store.GetUserByEmail(t.Context(), tc.email)
			if tc.rejected {
				if resp.message != fmt.Sprintf("email: email must be at most %d bytes", models.MaxEmailLength) {
					t.Fatalf("validation message = %q", resp.message)
				}
				if !models.IsNotFound(err) {
					t.Fatalf("rejected email lookup error = %v, want not found", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := strings.ToLower(strings.TrimSpace(tc.email)); saved.Email != want {
				t.Fatalf("stored email = %q, want %q", saved.Email, want)
			}
		})
	}
}

func TestAdminUserUpdateIgnoresEmail(t *testing.T) {
	s := newAuthzTestServer(t)
	admin := mkTestUser(t, s.store, "admin@example.com", models.UserRoleAdmin)
	target := mkTestUser(t, s.store, "existing@example.com", models.UserRoleMember)
	body, err := json.Marshal(map[string]string{
		"email":     strings.Repeat("a", models.MaxEmailLength+1) + "@example.com",
		"full_name": "Updated User",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doAuthzRequest(t, s, http.MethodPut, fmt.Sprintf("/api/v1/admin/users/%d", target.ID), string(body), withSession(sessionCookie(t, s.store, admin)))
	if resp.status != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.status)
	}
	saved, err := s.store.GetUser(t.Context(), target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Email != target.Email || saved.FullName != "Updated User" {
		t.Fatalf("saved email/name = %q/%q, want %q/Updated User", saved.Email, saved.FullName, target.Email)
	}
}
