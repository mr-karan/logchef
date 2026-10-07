package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// collectionRouteEnv is a full Server (real routes, middleware, and SQLite
// store) for exercising collection endpoints with real API tokens.
type collectionRouteEnv struct {
	t   *testing.T
	s   *Server
	cfg *config.Config
}

func newCollectionRouteEnv(t *testing.T) *collectionRouteEnv {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: logger,
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "collections.db")},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// Test-only HMAC key for hashing generated API tokens; not a credential.
	cfg := &config.Config{Auth: config.AuthConfig{APITokenSecret: strings.Repeat("t", 32)}}
	s := New(ServerOptions{
		Config: cfg,
		SQLite: db,
		FS:     http.FS(fstest.MapFS{"index.html": {Data: []byte(`<base href="/" />`)}}),
		Logger: logger,
	})
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	return &collectionRouteEnv{t: t, s: s, cfg: cfg}
}

// token issues a real API token for user with the given scopes.
func (e *collectionRouteEnv) token(user *models.User, scopes ...models.TokenScope) string {
	e.t.Helper()
	resp, err := core.CreateAPIToken(context.Background(), e.s.sqlite, e.s.log, &e.cfg.Auth, user.ID, "test-"+user.Email, nil, scopes)
	if err != nil {
		e.t.Fatalf("CreateAPIToken(%s): %v", user.Email, err)
	}
	return resp.Token
}

// do sends a request and returns the status code and decoded "data" field.
func (e *collectionRouteEnv) do(token, method, path, body string) (status int, data json.RawMessage) {
	e.t.Helper()
	var reqBody io.Reader = http.NoBody
	if body != "" {
		reqBody = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.s.app.Test(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &envelope)
	return resp.StatusCode, envelope.Data
}

func (e *collectionRouteEnv) expect(token, method, path, body string, want int) json.RawMessage {
	e.t.Helper()
	got, data := e.do(token, method, path, body)
	if got != want {
		e.t.Errorf("%s %s = %d, want %d (data=%s)", method, path, got, want, data)
	}
	return data
}

func (e *collectionRouteEnv) listedCollections(token string) []models.Collection {
	e.t.Helper()
	data := e.expect(token, http.MethodGet, "/api/v1/collections", "", http.StatusOK)
	var list []models.Collection
	if err := json.Unmarshal(data, &list); err != nil {
		e.t.Fatalf("decode collections: %v", err)
	}
	return list
}

var collectionsRW = []models.TokenScope{models.TokenScopeCollectionsRead, models.TokenScopeCollectionsWrite}

// The new team-share routes keep the collections:read / collections:write
// token scopes.
func TestCollectionTeamRoutesTokenScopes(t *testing.T) {
	e := newCollectionRouteEnv(t)
	owner := mkTestUser(t, e.s.sqlite, "scope-owner@test.dev", models.UserRoleMember)
	team, _ := mkTestTeam(t, e.s.sqlite, "scope-team", owner)
	coll, err := core.CreateCollection(context.Background(), e.s.sqlite, e.s.log, "Scoped", "", owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	teamsPath := fmt.Sprintf("/api/v1/collections/%d/teams", coll.ID)
	shareBody := fmt.Sprintf(`{"team_id":%d}`, team.ID)

	noCollections := e.token(owner, models.TokenScopeLogsRead)
	e.expect(noCollections, http.MethodGet, teamsPath, "", http.StatusForbidden)

	readOnly := e.token(owner, models.TokenScopeCollectionsRead)
	e.expect(readOnly, http.MethodGet, teamsPath, "", http.StatusOK)
	e.expect(readOnly, http.MethodPost, teamsPath, shareBody, http.StatusForbidden)
	e.expect(readOnly, http.MethodDelete, fmt.Sprintf("%s/%d", teamsPath, team.ID), "", http.StatusForbidden)

	readWrite := e.token(owner, collectionsRW...)
	e.expect(readWrite, http.MethodPost, teamsPath, shareBody, http.StatusCreated)
	e.expect(readWrite, http.MethodDelete, fmt.Sprintf("%s/%d", teamsPath, team.ID), "", http.StatusOK)
}

// End-to-end API behavior: validation, owner-only mutation, team-derived
// visibility, roster privacy, locked items, server-side execution denial, and
// revocation after the owner leaves the team.
func TestCollectionTeamRoutesSharingFlow(t *testing.T) {
	e := newCollectionRouteEnv(t)
	ctx := context.Background()
	db := e.s.sqlite

	owner := mkTestUser(t, db, "flow-owner@test.dev", models.UserRoleMember)
	member := mkTestUser(t, db, "flow-member@test.dev", models.UserRoleMember)
	outsider := mkTestUser(t, db, "flow-outsider@test.dev", models.UserRoleMember)
	admin := mkTestUser(t, db, "flow-admin@test.dev", models.UserRoleAdmin)

	// owner belongs to both teams; member only to the shared team, whose
	// source differs from the infra source the curated query runs on.
	shared, _ := mkTestTeam(t, db, "flow-project-14", owner, member)
	infra, infraSrc := mkTestTeam(t, db, "flow-infra", owner, outsider)
	foreign, _ := mkTestTeam(t, db, "flow-foreign")

	sq, err := db.CreateSavedQuery(ctx, infraSrc.ID, nil, "infra errors", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, `{"content":"SELECT 1"}`, &owner.ID)
	if err != nil {
		t.Fatalf("CreateSavedQuery: %v", err)
	}
	coll, err := core.CreateCollection(ctx, db, e.s.log, "Flow", "", owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := core.AddCollectionItem(ctx, db, e.s.log, coll.ID, owner.ID, sq.ID, 0); err != nil {
		t.Fatalf("AddCollectionItem: %v", err)
	}

	ownerTok := e.token(owner, collectionsRW...)
	memberTok := e.token(member, append(collectionsRW, models.TokenScopeSavedQueriesRead, models.TokenScopeSavedQueriesWrite, models.TokenScopeLogsRead)...)
	outsiderTok := e.token(outsider, collectionsRW...)
	adminTok := e.token(admin, collectionsRW...)

	base := fmt.Sprintf("/api/v1/collections/%d", coll.ID)
	teamsPath := base + "/teams"

	// Input validation.
	e.expect(ownerTok, http.MethodPost, teamsPath, `{"team_id":0}`, http.StatusBadRequest)
	e.expect(ownerTok, http.MethodPost, teamsPath, `{"team_id":"x"}`, http.StatusBadRequest)
	e.expect(ownerTok, http.MethodPost, teamsPath, `{not json`, http.StatusBadRequest)
	e.expect(ownerTok, http.MethodDelete, teamsPath+"/abc", "", http.StatusBadRequest)
	// Non-admin owners: a foreign team and a missing team look the same.
	e.expect(ownerTok, http.MethodPost, teamsPath, fmt.Sprintf(`{"team_id":%d}`, foreign.ID), http.StatusForbidden)
	e.expect(ownerTok, http.MethodPost, teamsPath, `{"team_id":999999}`, http.StatusForbidden)
	// A non-participating global admin cannot see or mutate the collection.
	e.expect(adminTok, http.MethodPost, teamsPath, fmt.Sprintf(`{"team_id":%d}`, foreign.ID), http.StatusNotFound)
	e.expect(adminTok, http.MethodGet, teamsPath, "", http.StatusNotFound)

	// Before sharing, the team member cannot see the collection.
	e.expect(memberTok, http.MethodGet, base, "", http.StatusNotFound)

	shareBody := fmt.Sprintf(`{"team_id":%d}`, shared.ID)
	e.expect(ownerTok, http.MethodPost, teamsPath, shareBody, http.StatusCreated)
	e.expect(ownerTok, http.MethodPost, teamsPath, shareBody, http.StatusCreated) // idempotent

	// Owner roster shows the team; counts agree across list and detail.
	var teams []models.CollectionTeam
	if err := json.Unmarshal(e.expect(ownerTok, http.MethodGet, teamsPath, "", http.StatusOK), &teams); err != nil {
		t.Fatalf("decode teams: %v", err)
	}
	if len(teams) != 1 || teams[0].TeamID != shared.ID || teams[0].TeamName != shared.Name {
		t.Fatalf("teams roster = %+v, want only %s", teams, shared.Name)
	}
	var detail models.Collection
	if err := json.Unmarshal(e.expect(memberTok, http.MethodGet, base, "", http.StatusOK), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.CallerRole != models.CollectionRoleMember || detail.MemberCount != 1 || detail.TeamCount != 1 || detail.ItemCount != 1 {
		t.Errorf("member detail = %+v, want member role, 1 member, 1 team, 1 item", detail)
	}
	listed := 0
	for _, c := range e.listedCollections(memberTok) {
		if c.ID == coll.ID {
			listed++
			if c.MemberCount != detail.MemberCount || c.TeamCount != detail.TeamCount || c.ItemCount != detail.ItemCount {
				t.Errorf("list counts %+v disagree with detail %+v", c, detail)
			}
		}
	}
	if listed != 1 {
		t.Errorf("collection listed %d times for team member, want 1", listed)
	}

	// Team-derived members get no rosters and no share management.
	e.expect(memberTok, http.MethodGet, teamsPath, "", http.StatusForbidden)
	e.expect(memberTok, http.MethodGet, base+"/members", "", http.StatusForbidden)
	e.expect(memberTok, http.MethodDelete, fmt.Sprintf("%s/%d", teamsPath, shared.ID), "", http.StatusForbidden)

	// The item is visible but locked, and the server still denies the source.
	var items []models.CollectionItem
	if err := json.Unmarshal(e.expect(memberTok, http.MethodGet, base+"/items", "", http.StatusOK), &items); err != nil {
		t.Fatalf("decode items: %v", err)
	}
	if len(items) != 1 || items[0].Runnable {
		t.Errorf("member items = %+v, want one locked item", items)
	}
	// Saved-query read, resolve, edit, and delete keep the source gate (404
	// hides the query), and none of them falls through to a second response.
	sqPath := fmt.Sprintf("/api/v1/saved-queries/%d", sq.ID)
	e.expect(memberTok, http.MethodGet, sqPath, "", http.StatusNotFound)
	e.expect(memberTok, http.MethodGet, sqPath+"/resolve", "", http.StatusNotFound)
	e.expect(memberTok, http.MethodPut, sqPath, `{"name":"hijack"}`, http.StatusNotFound)
	e.expect(memberTok, http.MethodDelete, sqPath, "", http.StatusNotFound)
	if got, err := db.GetSavedQuery(ctx, sq.ID); err != nil || got.Name != "infra errors" {
		t.Errorf("saved query after denied edit/delete = %+v / %v, want unchanged", got, err)
	}
	queryBody := `{"raw_sql":"SELECT 1","limit":10}`
	e.expect(memberTok, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs/query", infra.ID, infraSrc.ID), queryBody, http.StatusForbidden)
	e.expect(memberTok, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs/query", shared.ID, infraSrc.ID), queryBody, http.StatusForbidden)

	// A user outside the shared team cannot read it, even with source access.
	e.expect(outsiderTok, http.MethodGet, base, "", http.StatusNotFound)
	e.expect(outsiderTok, http.MethodGet, base+"/items", "", http.StatusNotFound)

	// Leaving the team revokes access on the next request; rejoining restores it.
	if err := db.RemoveTeamMember(ctx, shared.ID, member.ID); err != nil {
		t.Fatalf("RemoveTeamMember: %v", err)
	}
	e.expect(memberTok, http.MethodGet, base, "", http.StatusNotFound)
	if err := db.AddTeamMember(ctx, shared.ID, member.ID, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	e.expect(memberTok, http.MethodGet, base, "", http.StatusOK)

	// The owner can revoke the share after leaving the team.
	if err := db.RemoveTeamMember(ctx, shared.ID, owner.ID); err != nil {
		t.Fatalf("RemoveTeamMember(owner): %v", err)
	}
	e.expect(ownerTok, http.MethodDelete, fmt.Sprintf("%s/%d", teamsPath, shared.ID), "", http.StatusOK)
	e.expect(memberTok, http.MethodGet, base, "", http.StatusNotFound)

	// Existing member routes keep their client errors: the last owner cannot leave.
	e.expect(ownerTok, http.MethodDelete, fmt.Sprintf("%s/members/%d", base, owner.ID), "", http.StatusConflict)

	// Personal collections reject team shares.
	personal, err := core.EnsurePersonalCollection(ctx, db, e.s.log, owner)
	if err != nil {
		t.Fatalf("EnsurePersonalCollection: %v", err)
	}
	e.expect(ownerTok, http.MethodPost, fmt.Sprintf("/api/v1/collections/%d/teams", personal.ID), fmt.Sprintf(`{"team_id":%d}`, infra.ID), http.StatusBadRequest)

	// A global admin who owns a collection may pick any team, and gets 404 for
	// a missing one.
	adminColl, err := core.CreateCollection(ctx, db, e.s.log, "Admin owned", "", admin.ID)
	if err != nil {
		t.Fatalf("CreateCollection(admin): %v", err)
	}
	adminTeams := fmt.Sprintf("/api/v1/collections/%d/teams", adminColl.ID)
	e.expect(adminTok, http.MethodPost, adminTeams, fmt.Sprintf(`{"team_id":%d}`, foreign.ID), http.StatusCreated)
	e.expect(adminTok, http.MethodPost, adminTeams, `{"team_id":999999}`, http.StatusNotFound)
}
