package core

import (
	"context"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
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
	if !TokenHasScope(created.APIToken, models.TokenScopeLogsRead) {
		t.Fatal("created token does not grant logs:read")
	}
	if TokenHasScope(created.APIToken, models.TokenScopeSavedQueriesWrite) {
		t.Fatal("created token unexpectedly grants saved_queries:write")
	}

	_, authenticatedToken, err := AuthenticateAPIToken(ctx, db, log, authCfg, created.Token)
	if err != nil {
		t.Fatalf("AuthenticateAPIToken: %v", err)
	}
	if !TokenHasScope(authenticatedToken, models.TokenScopeSourcesRead) {
		t.Fatal("authenticated token lost sources:read scope")
	}
	if TokenHasScope(authenticatedToken, models.TokenScopeTokensWrite) {
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
