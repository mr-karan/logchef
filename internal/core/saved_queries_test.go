package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// seedSavedQueryOnSource creates a saved query owned by owner on src, so
// visibility tests only need to vary team/source wiring.
func seedSavedQueryOnSource(t *testing.T, db *sqlite.DB, src *models.Source, owner *models.User) *models.SavedQuery {
	t.Helper()
	sq, err := db.CreateSavedQuery(context.Background(), src.ID, nil, "q", "",
		models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, "{}", &owner.ID)
	if err != nil {
		t.Fatalf("CreateSavedQuery: %v", err)
	}
	return sq
}

// TestListSavedQueriesForUserCrossTeamVisibility pins the contract behind
// ListSavedQueriesForUser: a saved query is visible to a user if ANY team
// they belong to has access to the query's source — not just the team it was
// created from. This is the cross-team visibility rule called out as a
// priority in issue #63.
func TestListSavedQueriesForUserCrossTeamVisibility(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	creator := newTestUser(t, db, "sq-creator@example.com", "Creator")
	src := newTestSource(t, db, "shared-src")
	sq := seedSavedQueryOnSource(t, db, src, creator)

	// Team A is linked to the source and the creator is a member of it.
	teamA, err := CreateTeam(ctx, db, log, "team-a", "")
	if err != nil {
		t.Fatalf("CreateTeam(team-a): %v", err)
	}
	if err := AddTeamMember(ctx, db, log, teamA.ID, creator.ID, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember(creator, team-a): %v", err)
	}
	if err := AddTeamSource(ctx, db, log, teamA.ID, src.ID); err != nil {
		t.Fatalf("AddTeamSource(team-a, src): %v", err)
	}

	// Team B is also linked to the source; a different user is only a member
	// of team B, never team A — cross-team visibility must still show them
	// the query.
	crossTeamUser := newTestUser(t, db, "cross-team@example.com", "Cross Team")
	teamB, err := CreateTeam(ctx, db, log, "team-b", "")
	if err != nil {
		t.Fatalf("CreateTeam(team-b): %v", err)
	}
	if err := AddTeamMember(ctx, db, log, teamB.ID, crossTeamUser.ID, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember(crossTeamUser, team-b): %v", err)
	}
	if err := AddTeamSource(ctx, db, log, teamB.ID, src.ID); err != nil {
		t.Fatalf("AddTeamSource(team-b, src): %v", err)
	}

	// An outsider with no team linked to the source must not see it.
	outsider := newTestUser(t, db, "outsider@example.com", "Outsider")

	for _, tc := range []struct {
		name    string
		userID  models.UserID
		visible bool
	}{
		{"creator via team-a", creator.ID, true},
		{"cross-team user via team-b sees it too", crossTeamUser.ID, true},
		{"outsider with no team access does not", outsider.ID, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queries, err := ListSavedQueriesForUser(ctx, db, log, tc.userID)
			if err != nil {
				t.Fatalf("ListSavedQueriesForUser: %v", err)
			}
			found := false
			for _, q := range queries {
				if q.ID == sq.ID {
					found = true
				}
			}
			if found != tc.visible {
				t.Errorf("ListSavedQueriesForUser(%d) contains query %d = %v, want %v", tc.userID, sq.ID, found, tc.visible)
			}
		})
	}
}

// TestUserCanEditSavedQueryDelegation pins the delegated-edit contract: an
// owner/editor of a shared (non-personal) collection that contains the query
// may edit it even though they didn't create it; a plain member of that same
// collection may not (curating != editing); membership in a *personal*
// collection never grants delegated edit (personal collections are
// single-user by construction, but the gate is explicit in the store query).
func TestUserCanEditSavedQueryDelegation(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	creator := newTestUser(t, db, "delegate-creator@example.com", "Creator")
	src := newTestSource(t, db, "delegate-src")
	sq := seedSavedQueryOnSource(t, db, src, creator)

	collOwner := newTestUser(t, db, "delegate-coll-owner@example.com", "Coll Owner")
	editor := newTestUser(t, db, "delegate-editor@example.com", "Editor")
	member := newTestUser(t, db, "delegate-member@example.com", "Member")
	stranger := newTestUser(t, db, "delegate-stranger@example.com", "Stranger")

	coll, err := CreateCollection(ctx, db, log, "Shared", "", collOwner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := db.AddCollectionItem(ctx, coll.ID, sq.ID, 0, &collOwner.ID); err != nil {
		t.Fatalf("AddCollectionItem: %v", err)
	}
	if err := AddCollectionMember(ctx, db, log, coll.ID, collOwner.ID, editor.ID, models.CollectionRoleEditor); err != nil {
		t.Fatalf("AddCollectionMember(editor): %v", err)
	}
	if err := AddCollectionMember(ctx, db, log, coll.ID, collOwner.ID, member.ID, models.CollectionRoleMember); err != nil {
		t.Fatalf("AddCollectionMember(member): %v", err)
	}

	for _, tc := range []struct {
		name string
		user *models.User
		want bool
	}{
		{"collection owner", &models.User{ID: collOwner.ID, Role: models.UserRoleMember}, true},
		{"collection editor", &models.User{ID: editor.ID, Role: models.UserRoleMember}, true},
		{"collection member (participation only)", &models.User{ID: member.ID, Role: models.UserRoleMember}, false},
		{"stranger with no collection membership", &models.User{ID: stranger.ID, Role: models.UserRoleMember}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := UserCanEditSavedQuery(ctx, db, sq, tc.user)
			if err != nil {
				t.Fatalf("UserCanEditSavedQuery: %v", err)
			}
			if got != tc.want {
				t.Errorf("UserCanEditSavedQuery(user=%d) = %v, want %v", tc.user.ID, got, tc.want)
			}
		})
	}

	// The creator's own base authority (not delegation) is unaffected: they
	// can always edit their own query regardless of collection membership.
	creatorUser := &models.User{ID: creator.ID, Role: models.UserRoleMember}
	if got, err := UserCanEditSavedQuery(ctx, db, sq, creatorUser); err != nil || !got {
		t.Errorf("creator UserCanEditSavedQuery = %v / %v, want true/nil", got, err)
	}
}

// TestMarkSavedQueriesRunnable pins the batched runnable-flag computation: a
// query is runnable iff the user has access to its source, computed from a
// single accessible-sources fetch rather than a per-row check.
func TestMarkSavedQueriesRunnable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	user := newTestUser(t, db, "runnable-user@example.com", "User")
	accessibleSrc := newTestSource(t, db, "runnable-accessible")
	lockedSrc := newTestSource(t, db, "runnable-locked")

	team, err := CreateTeam(ctx, db, log, "runnable-team", "")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := AddTeamMember(ctx, db, log, team.ID, user.ID, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	if err := AddTeamSource(ctx, db, log, team.ID, accessibleSrc.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}

	accessibleQuery := seedSavedQueryOnSource(t, db, accessibleSrc, user)
	lockedQuery := seedSavedQueryOnSource(t, db, lockedSrc, user)

	queries := []*models.SavedQuery{accessibleQuery, lockedQuery, nil}
	if err := MarkSavedQueriesRunnable(ctx, db, user.ID, queries); err != nil {
		t.Fatalf("MarkSavedQueriesRunnable: %v", err)
	}
	if accessibleQuery.Runnable == nil || !*accessibleQuery.Runnable {
		t.Errorf("accessible query Runnable = %+v, want true", accessibleQuery.Runnable)
	}
	if lockedQuery.Runnable == nil || *lockedQuery.Runnable {
		t.Errorf("locked query Runnable = %+v, want false", lockedQuery.Runnable)
	}
}

// TestValidateSavedQueryContent pins the pure content-validation rules used
// by both create and update.
func TestValidateSavedQueryContent(t *testing.T) {
	t.Parallel()

	valid := `{"version":1,"sourceId":1,"timeRange":{"relative":"15m"},"limit":100,"content":"SELECT 1"}`

	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"empty content is allowed (no-op)", "", false},
		{"valid relative time range", valid, false},
		{"invalid JSON", "{not json", true},
		{"non-positive version", `{"version":0,"limit":100,"content":"x"}`, true},
		{"empty query content", `{"version":1,"limit":100,"content":""}`, true},
		{"non-positive limit", `{"version":1,"limit":0,"content":"x"}`, true},
		{"both relative and absolute time set", `{"version":1,"limit":10,"content":"x","timeRange":{"relative":"15m","absolute":{"start":1,"end":2}}}`, true},
		{"invalid relative time format", `{"version":1,"limit":10,"content":"x","timeRange":{"relative":"15"}}`, true},
		{"absolute start non-positive", `{"version":1,"limit":10,"content":"x","timeRange":{"absolute":{"start":0,"end":5}}}`, true},
		{"absolute end non-positive", `{"version":1,"limit":10,"content":"x","timeRange":{"absolute":{"start":5,"end":0}}}`, true},
		{"absolute end before start", `{"version":1,"limit":10,"content":"x","timeRange":{"absolute":{"start":10,"end":5}}}`, true},
		{"absolute end after start is valid", `{"version":1,"limit":10,"content":"x","timeRange":{"absolute":{"start":5,"end":10}}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSavedQueryContent(tc.content)
			if tc.wantErr && !errors.Is(err, ErrInvalidQueryContent) {
				t.Errorf("ValidateSavedQueryContent(%s) err = %v, want ErrInvalidQueryContent", tc.content, err)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateSavedQueryContent(%s) unexpected err: %v", tc.content, err)
			}
		})
	}
}

// TestValidateContentMatchesLanguage pins the durable defense against
// persisting a (language, content) mismatch (root cause of prod query #119).
// The must-PASS (no-false-reject) cases matter as much as the must-REJECT ones:
// a false reject would break legitimate no-filter and templated saved queries.
func TestValidateContentMatchesLanguage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		content  string
		language models.QueryLanguage
		wantErr  bool
	}{
		// --- Must PASS: empty content is valid for every language ---
		{"empty logchefql", "", models.QueryLanguageLogchefQL, false},
		{"empty clickhouse-sql", "", models.QueryLanguageClickHouseSQL, false},
		{"empty logsql", "", models.QueryLanguageLogsQL, false},
		{"whitespace-only clickhouse-sql", "   \t\n ", models.QueryLanguageClickHouseSQL, false},

		// --- Must PASS: valid content in its declared language ---
		{"valid logchefql", `level="error" and svc="api"`, models.QueryLanguageLogchefQL, false},
		{"valid clickhouse-sql", `SELECT * FROM x WHERE a=1`, models.QueryLanguageClickHouseSQL, false},
		{"escaped-quote clickhouse-sql", `SELECT * FROM x WHERE s = 'it''s'`, models.QueryLanguageClickHouseSQL, false},
		{"non-empty logsql", `service:="payments" AND level:="error"`, models.QueryLanguageLogsQL, false},

		// --- Must PASS: templated content accepted via the "{{" marker path ---
		{"templated clickhouse-sql", `SELECT * FROM x WHERE a = {{val}}`, models.QueryLanguageClickHouseSQL, false},
		{"templated logchefql", `svc={{service}}`, models.QueryLanguageLogchefQL, false},

		// --- Accepted under design (B): clickhouse-sql / logsql are NOT
		// strict-parsed, to avoid false-rejecting valid-but-exotic SQL. The #119
		// mislabel (LogchefQL content declared clickhouse-sql) is prevented at its
		// frontend root cause, not re-litigated with a lossy third-party parser. ---
		{"logchefql-shaped content under clickhouse-sql is accepted", `smtp_id="hedwig-mailer" and status="delivered"`, models.QueryLanguageClickHouseSQL, false},
		{"broken sql under clickhouse-sql is accepted (not parsed)", `SELECT FROM WHERE`, models.QueryLanguageClickHouseSQL, false},

		// --- Must REJECT: only the LogchefQL direction is enforced (our parser) ---
		{"malformed logchefql", `level="error" and and`, models.QueryLanguageLogchefQL, true},
		{"sql content under logchefql", `SELECT * FROM x WHERE a=1`, models.QueryLanguageLogchefQL, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateContentMatchesLanguage(tc.content, tc.language)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidQueryContent) {
					t.Errorf("ValidateContentMatchesLanguage(%q, %s) err = %v, want ErrInvalidQueryContent", tc.content, tc.language, err)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateContentMatchesLanguage(%q, %s) unexpected err: %v", tc.content, tc.language, err)
			}
		})
	}
}

// TestCreateSavedQueryRejectsLanguageMismatch exercises the full create path:
// a mismatched (language, content) pair is rejected with ErrInvalidQueryContent
// (which both saved-query handlers map to HTTP 400), while a matching pair is
// persisted. ds is nil, which skips the source-support check but keeps the rest
// of the shared create/update path — the same path UpdateSavedQuery uses.
func TestCreateSavedQueryRejectsLanguageMismatch(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	user := newTestUser(t, db, "eve@example.com", "Eve")
	src := newTestSource(t, db, "sql-source")

	// Enforced direction: SQL content declared as logchefql — our own parser
	// rejects it, so the create path returns the 400 sentinel.
	mismatch := `{"version":1,"limit":100,"content":"SELECT * FROM x WHERE a=1"}`
	_, err := CreateSavedQuery(context.Background(), db, nil, log, src.ID, nil, "bad", "",
		mismatch, models.QueryLanguageLogchefQL, models.SavedQueryEditorModeBuilder, user.ID)
	if !errors.Is(err, ErrInvalidQueryContent) {
		t.Fatalf("CreateSavedQuery(mismatch) err = %v, want ErrInvalidQueryContent (HTTP 400)", err)
	}

	// Matching pair: valid LogchefQL declared as logchefql.
	match := `{"version":1,"limit":100,"content":"level=\"error\""}`
	created, err := CreateSavedQuery(context.Background(), db, nil, log, src.ID, nil, "good", "",
		match, models.QueryLanguageLogchefQL, models.SavedQueryEditorModeBuilder, user.ID)
	if err != nil {
		t.Fatalf("CreateSavedQuery(match) unexpected err: %v", err)
	}
	if created == nil {
		t.Fatal("CreateSavedQuery(match) returned nil query")
	}
}

type savedQueryGetterFunc func(context.Context, int) (*models.SavedQuery, error)

func (f savedQueryGetterFunc) GetSavedQuery(ctx context.Context, queryID int) (*models.SavedQuery, error) {
	return f(ctx, queryID)
}

func TestGetSavedQueryTreatsNilStoreResultAsNotFound(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := GetSavedQuery(context.Background(), savedQueryGetterFunc(func(context.Context, int) (*models.SavedQuery, error) {
		return nil, nil
	}), log, 123)

	if !errors.Is(err, ErrQueryNotFound) {
		t.Fatalf("GetSavedQuery nil result error = %v, want %v", err, ErrQueryNotFound)
	}
}

// TestUserCanDeleteSavedQuery covers the base creator-or-admin authority shared
// by delete (and the non-delegated part of edit). Delegated collection-editor
// edit access requires a DB and is exercised by the integration/browser smoke test.
func TestUserCanDeleteSavedQuery(t *testing.T) {
	t.Parallel()

	creator := models.UserID(42)
	other := models.UserID(99)

	cases := []struct {
		name  string
		query *models.SavedQuery
		user  *models.User
		want  bool
	}{
		{
			name:  "nil query",
			query: nil,
			user:  &models.User{ID: creator},
			want:  false,
		},
		{
			name:  "nil user",
			query: &models.SavedQuery{CreatedBy: &creator},
			user:  nil,
			want:  false,
		},
		{
			name:  "global admin always allowed",
			query: &models.SavedQuery{CreatedBy: &other},
			user:  &models.User{ID: creator, Role: models.UserRoleAdmin},
			want:  true,
		},
		{
			name:  "global admin allowed on legacy NULL-creator query",
			query: &models.SavedQuery{CreatedBy: nil},
			user:  &models.User{ID: creator, Role: models.UserRoleAdmin},
			want:  true,
		},
		{
			name:  "creator allowed",
			query: &models.SavedQuery{CreatedBy: &creator},
			user:  &models.User{ID: creator, Role: models.UserRoleMember},
			want:  true,
		},
		{
			name:  "non-creator member denied",
			query: &models.SavedQuery{CreatedBy: &creator},
			user:  &models.User{ID: other, Role: models.UserRoleMember},
			want:  false,
		},
		{
			name:  "legacy NULL-creator query denied for non-admin",
			query: &models.SavedQuery{CreatedBy: nil},
			user:  &models.User{ID: creator, Role: models.UserRoleMember},
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := UserCanDeleteSavedQuery(tc.query, tc.user); got != tc.want {
				t.Errorf("UserCanDeleteSavedQuery(%+v, %+v) = %v, want %v", tc.query, tc.user, got, tc.want)
			}
		})
	}
}
