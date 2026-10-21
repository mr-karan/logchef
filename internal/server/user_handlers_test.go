package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/oauth"
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
			serviceOnly  bool
			want         int
		}{
			{"subset", scoped, nil, models.CreateAPITokenRequest{Scopes: []models.TokenScope{models.TokenScopeLogsRead}}, false, false, fiber.StatusCreated},
			{"superset", scoped, nil, models.CreateAPITokenRequest{Scopes: []models.TokenScope{models.TokenScopeUsersWrite}}, false, false, fiber.StatusForbidden},
			{"child wildcard", scoped, nil, models.CreateAPITokenRequest{Scopes: []models.TokenScope{models.TokenScopeAll}}, false, false, fiber.StatusForbidden},
			{"parent wildcard", []models.TokenScope{models.TokenScopeAll}, nil, models.CreateAPITokenRequest{Scopes: []models.TokenScope{models.TokenScopeAll}}, false, false, fiber.StatusCreated},
			{"expiry beyond parent", scoped, &parentExpiry, models.CreateAPITokenRequest{Scopes: scoped, ExpiresAt: &later}, false, false, fiber.StatusForbidden},
			{"missing expiry", scoped, &parentExpiry, models.CreateAPITokenRequest{Scopes: scoped}, false, false, fiber.StatusForbidden},
			{"equal expiry", scoped, &parentExpiry, models.CreateAPITokenRequest{Scopes: scoped, ExpiresAt: &parentExpiry}, false, false, fiber.StatusCreated},
			{"session unrestricted", nil, nil, models.CreateAPITokenRequest{Scopes: []models.TokenScope{models.TokenScopeAll}}, true, false, fiber.StatusCreated},
			{"default scopes", scoped, nil, models.CreateAPITokenRequest{}, false, true, fiber.StatusForbidden},
			{"default session scopes", nil, nil, models.CreateAPITokenRequest{}, true, true, fiber.StatusCreated},
			{"unknown scope", scoped, nil, models.CreateAPITokenRequest{Scopes: []models.TokenScope{"unknown"}}, false, false, fiber.StatusBadRequest},
			{"unknown after wildcard", []models.TokenScope{models.TokenScopeAll}, nil, models.CreateAPITokenRequest{Scopes: []models.TokenScope{models.TokenScopeAll, "unknown"}}, false, false, fiber.StatusBadRequest},
		} {
			if tc.serviceOnly && !serviceAccount {
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
				after, err := core.ListAPITokensForUser(context.Background(), s.sqlite, target.ID)
				if err != nil {
					t.Fatal(err)
				}
				if tc.want != fiber.StatusCreated {
					if len(after) != len(before) {
						t.Fatalf("denied request created a token: before=%d after=%d", len(before), len(after))
					}
					return
				}
				if len(after) != len(before)+1 {
					t.Fatalf("token count = %d, want %d", len(after), len(before)+1)
				}
				var child *models.APIToken
				for _, token := range after {
					if token.Name == "child" {
						child = token
					}
				}
				if child == nil {
					t.Fatal("created child token not stored")
				}
				wantScopes := tc.request.Scopes
				if serviceAccount && len(wantScopes) == 0 {
					wantScopes = core.ReadOnlyTokenScopes()
				}
				if !slices.Equal(child.Scopes, wantScopes) {
					t.Errorf("stored scopes = %v, want %v", child.Scopes, wantScopes)
				}
				if wantExpiry := tc.request.ExpiresAt; wantExpiry == nil {
					if child.ExpiresAt != nil {
						t.Errorf("stored expiry = %v, want nil", child.ExpiresAt)
					}
				} else if child.ExpiresAt == nil || !child.ExpiresAt.Equal(*wantExpiry) {
					t.Errorf("stored expiry = %v, want %v", child.ExpiresAt, wantExpiry)
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

func TestCreateTokenRejectsOtherAuthMethods(t *testing.T) {
	for _, serviceAccount := range []bool{false, true} {
		for _, method := range []string{"oauth", "unknown", "token"} {
			t.Run(fmt.Sprintf("service=%t/%s", serviceAccount, method), func(t *testing.T) {
				s := newAuthzTestServer(t)
				user := mkTestUser(t, s.sqlite, "admin@example.com", models.UserRoleAdmin)
				target := user
				path := "/tokens"
				handler := s.handleCreateAPIToken
				if serviceAccount {
					var err error
					target, err = core.CreateServiceAccount(context.Background(), s.sqlite, s.log, "Test bot")
					if err != nil {
						t.Fatal(err)
					}
					path = fmt.Sprintf("/%d/tokens", target.ID)
					handler = s.handleCreateServiceAccountToken
				}
				app := fiber.New()
				app.Use(func(c fiber.Ctx) error {
					c.Locals("user", user)
					c.Locals("auth_method", method)
					if method == "oauth" {
						c.Locals("oauth_token", &oauth.AccessToken{Principal: access.OAuthPrincipal(user, 1, "client", []models.TokenScope{models.TokenScopeTokensWrite})})
					}
					return c.Next()
				})
				app.Post("/tokens", handler)
				app.Post("/:userID/tokens", handler)
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"name":"child","scopes":["logs:read"]}`))
				req.Header.Set("Content-Type", "application/json")
				resp, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != fiber.StatusForbidden {
					t.Fatalf("status = %d, want 403", resp.StatusCode)
				}
				tokens, err := core.ListAPITokensForUser(context.Background(), s.sqlite, target.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(tokens) != 0 {
					t.Fatalf("denied request stored %d tokens", len(tokens))
				}
			})
		}
	}
}

func TestUserAdminGrantHTTP(t *testing.T) {
	for _, tc := range []struct {
		name    string
		scope   models.TokenScope
		session bool
		update  bool
		body    string
		want    int
		role    models.UserRole
	}{
		{"scoped create admin", models.TokenScopeUsersWrite, false, false, `{"role":"admin"}`, fiber.StatusForbidden, ""},
		{"scoped promote member", models.TokenScopeUsersWrite, false, true, `{"role":"admin"}`, fiber.StatusForbidden, models.UserRoleMember},
		{"scoped create member", models.TokenScopeUsersWrite, false, false, `{"role":"member"}`, fiber.StatusCreated, models.UserRoleMember},
		{"scoped default member", models.TokenScopeUsersWrite, false, false, `{}`, fiber.StatusCreated, models.UserRoleMember},
		{"wildcard create admin", models.TokenScopeAll, false, false, `{"role":"admin"}`, fiber.StatusCreated, models.UserRoleAdmin},
		{"session create admin", "", true, false, `{"role":"admin"}`, fiber.StatusCreated, models.UserRoleAdmin},
		{"wildcard promote member", models.TokenScopeAll, false, true, `{"role":"admin"}`, fiber.StatusOK, models.UserRoleAdmin},
		{"session promote member", "", true, true, `{"role":"admin"}`, fiber.StatusOK, models.UserRoleAdmin},
		{"scoped edit member", models.TokenScopeUsersWrite, false, true, `{"full_name":"Updated Member"}`, fiber.StatusOK, models.UserRoleMember},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newAuthzTestServer(t)
			admin := mkTestUser(t, s.sqlite, "admin@example.com", models.UserRoleAdmin)
			var auth func(*http.Request)
			if tc.session {
				auth = withSession(sessionCookie(t, s.sqlite, admin))
			} else {
				auth = withBearer(apiToken(t, s, admin, tc.scope))
			}
			method, path := http.MethodPost, "/api/v1/admin/users"
			var body map[string]string
			if err := json.Unmarshal([]byte(tc.body), &body); err != nil {
				t.Fatal(err)
			}
			if tc.update {
				member := mkTestUser(t, s.sqlite, "target@example.com", models.UserRoleMember)
				method = http.MethodPut
				path = fmt.Sprintf("%s/%d", path, member.ID)
			} else {
				body["email"] = "target@example.com"
				body["full_name"] = "Target User"
			}
			payload, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			resp := doAuthzRequest(t, s, method, path, string(payload), auth)
			if resp.status != tc.want {
				t.Fatalf("status = %d (%q), want %d", resp.status, resp.message, tc.want)
			}
			if tc.want == fiber.StatusForbidden && (!strings.Contains(resp.message, "admin") || !strings.Contains(resp.message, "*")) {
				t.Errorf("denial must explain the admin grant scope requirement, got %q", resp.message)
			}
			user, err := core.GetUserByEmail(context.Background(), s.sqlite, "target@example.com")
			if tc.role == "" {
				if !errors.Is(err, core.ErrUserNotFound) {
					t.Fatalf("denied creation persisted a user: user=%v err=%v", user, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if user.Role != tc.role {
				t.Errorf("stored role = %q, want %q", user.Role, tc.role)
			}
			if tc.update && resp.status == fiber.StatusOK && body["full_name"] != "" && user.FullName != body["full_name"] {
				t.Errorf("stored name = %q, want %q", user.FullName, body["full_name"])
			}
		})
	}
}

func TestServiceAccountRoleHTTP(t *testing.T) {
	s := newAuthzTestServer(t)
	admin := mkTestUser(t, s.sqlite, "admin@example.com", models.UserRoleAdmin)
	auth := withBearer(apiToken(t, s, admin, models.TokenScopeUsersWrite))
	resp := doAuthzRequest(t, s, http.MethodPost, "/api/v1/admin/service-accounts", `{"name":"Test bot","role":"admin"}`, auth)
	if resp.status != fiber.StatusCreated {
		t.Fatalf("status = %d (%q), want 201", resp.status, resp.message)
	}
	accounts, err := core.ListServiceAccounts(context.Background(), s.sqlite)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].Role != models.UserRoleMember {
		t.Fatalf("service-account creation must produce one member, got %v", accounts)
	}
	path := fmt.Sprintf("/api/v1/admin/users/%d", accounts[0].ID)
	resp = doAuthzRequest(t, s, http.MethodPut, path, `{"role":"admin"}`, auth)
	if resp.status != fiber.StatusNotFound {
		t.Fatalf("human-user update status = %d (%q), want 404", resp.status, resp.message)
	}
	account, err := core.GetUser(context.Background(), s.sqlite, accounts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Role != models.UserRoleMember {
		t.Errorf("stored service-account role = %q, want member", account.Role)
	}
}
