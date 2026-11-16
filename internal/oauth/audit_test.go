package oauth

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/pkg/models"
)

// A replay that the rotation itself detects, which over HTTP only happens
// when two refreshes race past the check, records one revocation.
func TestRotationReplayRecordsOnce(t *testing.T) {
	t.Parallel()
	db := openDB(t, sqlitetest.Path(t))
	s := newTestServer(t, db)
	ctx := context.Background()
	user := &models.User{Email: "customer-a@example.com", Status: models.UserStatusActive, AccountType: models.UserAccountTypeHuman, Role: models.UserRoleMember}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	req := &models.OAuthAuthRequest{
		ID: models.OAuthAuthRequestID(randomID()), ClientID: "chatgpt", RedirectURI: "https://chatgpt.example/cb",
		Resource: s.mcpResource, Scopes: []models.TokenScope{models.TokenScopeLogsRead}, OfflineAccess: true,
		CodeChallenge: "challenge", State: "s", ExpiresAt: now.Add(time.Minute), CreatedAt: now,
	}
	if err := db.CreateOAuthAuthRequest(ctx, req); err != nil {
		t.Fatal(err)
	}
	grant, err := db.ApproveOAuthAuthRequest(ctx, req.ID, user.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	st := &storage{server: s}
	_, issue, r0 := st.newIssue(scopeStrings(grant.Scopes, true), now, true)
	if err := db.IssueOAuthTokens(ctx, grant.ID, issue, now); err != nil {
		t.Fatal(err)
	}

	refresh := &refreshRequest{grant: grant, scopes: scopeStrings(grant.Scopes, true)}
	if _, _, _, err := st.CreateAccessAndRefreshTokens(ctx, refresh, r0); err != nil {
		t.Fatalf("first rotation: %v", err)
	}
	for range 2 {
		if _, _, _, err := st.CreateAccessAndRefreshTokens(ctx, refresh, r0); !errors.Is(err, oidc.ErrInvalidGrant()) {
			t.Fatalf("replayed rotation err = %v, want invalid_grant", err)
		}
	}

	events, err := db.ListAuditEvents(ctx, models.AuditFilter{Action: string(audit.OAuthGrantRevoke), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("stored %d revoke events, want 1: %+v", len(events), events)
	}
	ev := events[0]
	if ev.ActorType != models.AuditActorSystem || ev.TargetID != strconv.Itoa(int(grant.ID)) || ev.ClientID != "chatgpt" ||
		string(ev.Detail) != `{"cause":"refresh_replay"}` {
		t.Errorf("revoke event %+v", ev)
	}
}
