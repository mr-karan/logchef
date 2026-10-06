package access

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// T-AZ-1 (principal level), T-AZ-2, T-AZ-5.
func TestPrincipalRequire(t *testing.T) {
	t.Parallel()
	logsToken := &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeLogsRead}}
	sourcesToken := &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeSourcesRead}}
	allToken := &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeAll}}

	for _, tc := range []struct {
		name string
		p    Principal
		ok   bool
	}{
		{"session holds every scope", SessionPrincipal(&models.User{}), true},
		{"api token with scope", APITokenPrincipal(nil, logsToken), true},
		{"api token without scope", APITokenPrincipal(nil, sourcesToken), false},
		{"api token with wildcard", APITokenPrincipal(nil, allToken), true},
		{"api token missing", APITokenPrincipal(nil, nil), false},
		{"oauth with scope", Principal{Method: AuthOAuth, scopes: []models.TokenScope{models.TokenScopeLogsRead}}, true},
		{"oauth without scope", Principal{Method: AuthOAuth, scopes: []models.TokenScope{models.TokenScopeSourcesRead}}, false},
		{"oauth wildcard is not a grant", Principal{Method: AuthOAuth, scopes: []models.TokenScope{models.TokenScopeAll}}, false},
		{"zero method", Principal{User: &models.User{Role: models.UserRoleAdmin}}, false},
		{"unknown method", Principal{Method: AuthMethod(99), scopes: []models.TokenScope{models.TokenScopeLogsRead}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Require(models.TokenScopeLogsRead)
			if tc.ok && err != nil {
				t.Fatalf("Require = %v, want nil", err)
			}
			if !tc.ok && !errors.Is(err, ErrInsufficientScope) {
				t.Fatalf("Require = %v, want ErrInsufficientScope", err)
			}
		})
	}
}

func TestAPITokenPrincipalCopiesScopes(t *testing.T) {
	t.Parallel()
	token := &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeLogsRead}}
	p := APITokenPrincipal(nil, token)
	token.Scopes[0] = models.TokenScopeAll
	if err := p.Require(models.TokenScopeSourcesRead); err == nil {
		t.Fatal("changing the token after the principal was built widened the principal")
	}
}

type fixture struct {
	db       *sqlite.DB
	member   *models.User
	admin    *models.User
	outsider *models.User
	team     models.TeamID
	bareTeam models.TeamID // has the member but no source link
	source   models.SourceID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.New(ctx, sqlite.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "access.db")},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	user := func(email string, role models.UserRole) *models.User {
		u := &models.User{Email: email, FullName: email, Role: role, Status: models.UserStatusActive}
		if err := db.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser(%s): %v", email, err)
		}
		return u
	}
	team := func(name string) models.TeamID {
		tm := &models.Team{Name: name}
		if err := db.CreateTeam(ctx, tm); err != nil {
			t.Fatalf("CreateTeam(%s): %v", name, err)
		}
		return tm.ID
	}

	f := fixture{
		db:       db,
		member:   user("member@example.com", models.UserRoleMember),
		admin:    user("admin@example.com", models.UserRoleAdmin),
		outsider: user("outsider@example.com", models.UserRoleMember),
		team:     team("linked"),
		bareTeam: team("bare"),
	}
	src := &models.Source{Name: "logs", Connection: models.ConnectionInfo{
		Host: "ch:9000", Username: "default", Database: "default", TableName: "logs",
	}}
	if err := db.CreateSource(ctx, src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	f.source = src.ID
	if err := db.AddTeamSource(ctx, f.team, f.source); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	for _, tm := range []models.TeamID{f.team, f.bareTeam} {
		if err := db.AddTeamMember(ctx, tm, f.member.ID, models.TeamRoleMember); err != nil {
			t.Fatalf("AddTeamMember: %v", err)
		}
	}
	return f
}

// T-AZ-3 and T-AZ-5 at the core boundary. The HTTP matrix in
// internal/server/authz_test.go runs the same verdicts through requireAuth.
func TestAuthorizeTeamSource(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	noLogs := &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeSourcesRead}}

	for _, tc := range []struct {
		name    string
		p       Principal
		team    models.TeamID
		wantErr error
	}{
		{"member with linked source", SessionPrincipal(f.member), f.team, nil},
		{"global admin outside the team", SessionPrincipal(f.admin), f.team, nil},
		{"non-member", SessionPrincipal(f.outsider), f.team, ErrNotTeamMember},
		{"team without the source", SessionPrincipal(f.member), f.bareTeam, ErrSourceNotInTeam},
		{"global admin, team without the source", SessionPrincipal(f.admin), f.bareTeam, ErrSourceNotInTeam},
		{"token without scope", APITokenPrincipal(f.member, noLogs), f.team, ErrInsufficientScope},
		{"oauth without scope", Principal{User: f.member, Method: AuthOAuth}, f.team, ErrInsufficientScope},
		{"unknown method", Principal{User: f.admin}, f.team, ErrInsufficientScope},
		{"membership is checked before scope", APITokenPrincipal(f.outsider, noLogs), f.team, ErrNotTeamMember},
		{"link is checked before scope", APITokenPrincipal(f.member, noLogs), f.bareTeam, ErrSourceNotInTeam},
		{"no user", Principal{Method: AuthSession}, f.team, ErrNotTeamMember},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, err := AuthorizeTeamSource(context.Background(), f.db, tc.p, tc.team, f.source, models.TokenScopeLogsRead)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AuthorizeTeamSource error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if src != (AuthorizedSource{}) {
					t.Fatalf("denied call returned %+v, want the zero value", src)
				}
				return
			}
			if src.TeamID() != tc.team || src.SourceID() != f.source || src.UserID() != tc.p.User.ID {
				t.Fatalf("AuthorizedSource = %+v, want team %d source %d user %d", src, tc.team, f.source, tc.p.User.ID)
			}
		})
	}
}

// T-AZ-4 at the core boundary: nothing is cached between calls.
func TestAuthorizeTeamSourceSeesMembershipRemoval(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	p := SessionPrincipal(f.member)

	if _, err := AuthorizeTeamSource(ctx, f.db, p, f.team, f.source, models.TokenScopeLogsRead); err != nil {
		t.Fatalf("before removal: %v", err)
	}
	if err := f.db.RemoveTeamMember(ctx, f.team, f.member.ID); err != nil {
		t.Fatalf("RemoveTeamMember: %v", err)
	}
	if _, err := AuthorizeTeamSource(ctx, f.db, p, f.team, f.source, models.TokenScopeLogsRead); !errors.Is(err, ErrNotTeamMember) {
		t.Fatalf("after removal: %v, want ErrNotTeamMember", err)
	}
}

func TestAuthorizeTeam(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	noSources := &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeLogsRead}}

	for _, tc := range []struct {
		name    string
		p       Principal
		wantErr error
	}{
		{"member", SessionPrincipal(f.member), nil},
		{"global admin outside the team", SessionPrincipal(f.admin), nil},
		{"non-member", SessionPrincipal(f.outsider), ErrNotTeamMember},
		{"token without scope", APITokenPrincipal(f.member, noSources), ErrInsufficientScope},
		{"unknown method", Principal{User: f.member}, ErrInsufficientScope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := AuthorizeTeam(context.Background(), f.db, tc.p, f.team, models.TokenScopeSourcesRead)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AuthorizeTeam error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
