package core

import (
	"context"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestCreateAPITokenPersistsScopes(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	user := newTestUser(t, db, "token-scopes@example.com", "Token Scopes")
	authCfg := &config.AuthConfig{APITokenSecret: "0123456789abcdef0123456789abcdef"}

	created, err := CreateAPIToken(ctx, db, log, authCfg, user.ID, "readonly", nil, []models.TokenScope{
		models.TokenScopeLogsRead,
		models.TokenScopeSourcesRead,
	})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	if access.APITokenPrincipal(nil, created.APIToken).Require(models.TokenScopeLogsRead) != nil {
		t.Fatal("created token does not grant logs:read")
	}
	if access.APITokenPrincipal(nil, created.APIToken).Require(models.TokenScopeSavedQueriesWrite) == nil {
		t.Fatal("created token unexpectedly grants saved_queries:write")
	}

	_, authenticatedToken, err := AuthenticateAPIToken(ctx, db, log, authCfg, created.Token)
	if err != nil {
		t.Fatalf("AuthenticateAPIToken: %v", err)
	}
	if access.APITokenPrincipal(nil, authenticatedToken).Require(models.TokenScopeSourcesRead) != nil {
		t.Fatal("authenticated token lost sources:read scope")
	}
	if access.APITokenPrincipal(nil, authenticatedToken).Require(models.TokenScopeTokensWrite) == nil {
		t.Fatal("authenticated token unexpectedly grants tokens:write")
	}
}

func TestCreateServiceAccount(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	account, err := CreateServiceAccount(context.Background(), db, discardLogger(), "Log reader bot")
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	if account.AccountType != models.UserAccountTypeService {
		t.Fatalf("account type = %q, want service", account.AccountType)
	}
	if account.Role != models.UserRoleMember {
		t.Fatalf("role = %q, want member", account.Role)
	}

	accounts, err := ListServiceAccounts(context.Background(), db)
	if err != nil {
		t.Fatalf("ListServiceAccounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != account.ID {
		t.Fatalf("service account list = %+v, want created account", accounts)
	}
}

func TestAuthorizeAPITokenCreation(t *testing.T) {
	t.Parallel()
	expiry := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	earlier, later := expiry.Add(-time.Hour), expiry.Add(time.Hour)
	scopes := []models.TokenScope{models.TokenScopeTokensWrite, models.TokenScopeLogsRead}
	for _, tc := range []struct {
		name         string
		parentScopes []models.TokenScope
		parentExpiry *time.Time
		scopes       []models.TokenScope
		expiry       *time.Time
		wantErr      bool
	}{
		{"subset", scopes, nil, []models.TokenScope{models.TokenScopeLogsRead}, nil, false},
		{"equal scopes", scopes, nil, scopes, nil, false},
		{"superset", scopes, nil, append([]models.TokenScope{models.TokenScopeUsersWrite}, scopes...), nil, true},
		{"child wildcard", scopes, nil, []models.TokenScope{models.TokenScopeAll}, nil, true},
		{"parent wildcard", []models.TokenScope{models.TokenScopeAll}, nil, scopes, nil, false},
		{"both wildcards", []models.TokenScope{models.TokenScopeAll}, nil, []models.TokenScope{models.TokenScopeAll}, nil, false},
		{"expiry beyond parent", scopes, &expiry, scopes, &later, true},
		{"missing expiry", scopes, &expiry, scopes, nil, true},
		{"equal expiry", scopes, &expiry, scopes, &expiry, false},
		{"earlier expiry", scopes, &expiry, scopes, &earlier, false},
		{"unlimited parent", scopes, nil, scopes, &later, false},
		{"wildcard parent expiry", []models.TokenScope{models.TokenScopeAll}, &expiry, scopes, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := &models.APIToken{Scopes: tc.parentScopes, ExpiresAt: tc.parentExpiry}
			err := AuthorizeAPITokenCreation(parent, tc.scopes, tc.expiry)
			if (err != nil) != tc.wantErr {
				t.Fatalf("AuthorizeAPITokenCreation = %v, want error %t", err, tc.wantErr)
			}
		})
	}
}
