package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestCreateTokenDelegationHTTP(t *testing.T) {
	parentExpiry := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	later := parentExpiry.Add(time.Hour)
	scoped := []models.TokenScope{models.TokenScopeTokensWrite, models.TokenScopeLogsRead}
	for _, serviceAccount := range []bool{false, true} {
		for _, tc := range []struct {
			name         string
			parentScopes []models.TokenScope
			parentExpiry *time.Time
			request      models.CreateAPITokenRequest
			session      bool
			want         int
		}{
			{"subset", scoped, nil, models.CreateAPITokenRequest{Scopes: []models.TokenScope{models.TokenScopeLogsRead}}, false, fiber.StatusCreated},
			{"superset", scoped, nil, models.CreateAPITokenRequest{Scopes: []models.TokenScope{models.TokenScopeUsersWrite}}, false, fiber.StatusForbidden},
			{"child wildcard", scoped, nil, models.CreateAPITokenRequest{Scopes: []models.TokenScope{models.TokenScopeAll}}, false, fiber.StatusForbidden},
			{"parent wildcard", []models.TokenScope{models.TokenScopeAll}, nil, models.CreateAPITokenRequest{Scopes: []models.TokenScope{models.TokenScopeAll}}, false, fiber.StatusCreated},
			{"expiry beyond parent", scoped, &parentExpiry, models.CreateAPITokenRequest{Scopes: scoped, ExpiresAt: &later}, false, fiber.StatusForbidden},
			{"missing expiry", scoped, &parentExpiry, models.CreateAPITokenRequest{Scopes: scoped}, false, fiber.StatusForbidden},
			{"equal expiry", scoped, &parentExpiry, models.CreateAPITokenRequest{Scopes: scoped, ExpiresAt: &parentExpiry}, false, fiber.StatusCreated},
			{"session unrestricted", nil, nil, models.CreateAPITokenRequest{Scopes: []models.TokenScope{models.TokenScopeAll}}, true, fiber.StatusCreated},
			{"default scopes", scoped, nil, models.CreateAPITokenRequest{}, false, fiber.StatusForbidden},
		} {
			if tc.name == "default scopes" && !serviceAccount {
				continue
			}
			t.Run(fmt.Sprintf("service=%t/%s", serviceAccount, tc.name), func(t *testing.T) {
				s := newAuthzTestServer(t)
				user := mkTestUser(t, s.sqlite, "admin@example.com", models.UserRoleAdmin)
				path := "/api/v1/me/tokens"
				target := user
				if serviceAccount {
					account, err := core.CreateServiceAccount(context.Background(), s.sqlite, s.log, "Test bot")
					if err != nil {
						t.Fatal(err)
					}
					target = account
					path = fmt.Sprintf("/api/v1/admin/service-accounts/%d/tokens", account.ID)
				}
				var auth func(*http.Request)
				if tc.session {
					auth = withSession(sessionCookie(t, s.sqlite, user))
				} else {
					parent, err := core.CreateAPIToken(context.Background(), s.sqlite, s.log, &s.config.Auth, user.ID, "parent", tc.parentExpiry, tc.parentScopes)
					if err != nil {
						t.Fatal(err)
					}
					auth = withBearer(parent.Token)
				}
				before, err := core.ListAPITokensForUser(context.Background(), s.sqlite, target.ID)
				if err != nil {
					t.Fatal(err)
				}
				tc.request.Name = "child"
				body, err := json.Marshal(tc.request)
				if err != nil {
					t.Fatal(err)
				}
				resp := doAuthzRequest(t, s, http.MethodPost, path, string(body), auth)
				if resp.status != tc.want {
					t.Fatalf("status = %d (%q), want %d", resp.status, resp.message, tc.want)
				}
				if tc.want == fiber.StatusForbidden {
					after, err := core.ListAPITokensForUser(context.Background(), s.sqlite, target.ID)
					if err != nil {
						t.Fatal(err)
					}
					if len(after) != len(before) {
						t.Fatalf("denied request created a token: before=%d after=%d", len(before), len(after))
					}
				}
			})
		}
	}
}

func TestUpdateLastAdminHTTP(t *testing.T) {
	for _, body := range []string{`{"role":"member"}`, `{"status":"inactive"}`, `{"role":"member","status":"inactive"}`} {
		t.Run(body, func(t *testing.T) {
			s := newAuthzTestServer(t)
			admin := mkTestUser(t, s.sqlite, "admin@example.com", models.UserRoleAdmin)
			resp := doAuthzRequest(t, s, http.MethodPut, fmt.Sprintf("/api/v1/admin/users/%d", admin.ID), body, withSession(sessionCookie(t, s.sqlite, admin)))
			if resp.status != fiber.StatusBadRequest {
				t.Fatalf("status = %d (%q), want 400", resp.status, resp.message)
			}
			if resp.message != core.ErrCannotDeleteLastAdmin.Error() {
				t.Fatalf("message = %q, want %q", resp.message, core.ErrCannotDeleteLastAdmin.Error())
			}
		})
	}
}
