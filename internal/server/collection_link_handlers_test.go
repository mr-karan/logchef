package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/pkg/models"
)

// linkRouteEnv is a shared collection owned by creator with member as a
// member, a query creator wrote on a source both can reach, and a query on a
// source only creator can reach. admin and outsider take no part in it.
type linkRouteEnv struct {
	*collectionRouteEnv
	creator, member, admin, outsider *models.User
	coll, personal                   *models.Collection
	query, lockedQuery               *models.SavedQuery
	creatorTok, memberTok, adminTok  string
	outsiderTok                      string
	items                            string
}

func newLinkRouteEnv(t *testing.T) *linkRouteEnv {
	t.Helper()
	e := &linkRouteEnv{collectionRouteEnv: newCollectionRouteEnv(t)}
	ctx := context.Background()
	db := e.s.store
	e.creator = mkTestUser(t, db, "link-creator@test.dev", models.UserRoleMember)
	e.member = mkTestUser(t, db, "link-member@test.dev", models.UserRoleMember)
	e.admin = mkTestUser(t, db, "link-admin@test.dev", models.UserRoleAdmin)
	e.outsider = mkTestUser(t, db, "link-outsider@test.dev", models.UserRoleMember)
	_, src := mkTestTeam(t, db, "link-shared", e.creator, e.member, e.admin, e.outsider)
	_, lockedSrc := mkTestTeam(t, db, "link-private", e.creator)
	var err error
	e.query, err = db.CreateSavedQuery(ctx, src.ID, nil, "errors", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, `{"content":"SELECT 1"}`, &e.creator.ID)
	if err != nil {
		t.Fatalf("CreateSavedQuery: %v", err)
	}
	e.lockedQuery, err = db.CreateSavedQuery(ctx, lockedSrc.ID, nil, "locked", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, `{"content":"SELECT 1"}`, &e.creator.ID)
	if err != nil {
		t.Fatalf("CreateSavedQuery(locked): %v", err)
	}
	e.coll, err = core.CreateCollection(ctx, db, e.s.log, "Links", "", e.creator.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := core.AddCollectionMember(ctx, db, e.s.log, e.coll.ID, e.creator.ID, e.member.ID, models.CollectionRoleMember); err != nil {
		t.Fatalf("AddCollectionMember: %v", err)
	}
	e.personal, err = core.EnsurePersonalCollection(ctx, db, e.s.log, e.creator)
	if err != nil {
		t.Fatalf("EnsurePersonalCollection: %v", err)
	}
	e.creatorTok = e.token(e.creator, collectionsRW...)
	e.memberTok = e.token(e.member, collectionsRW...)
	e.adminTok = e.token(e.admin, collectionsRW...)
	e.outsiderTok = e.token(e.outsider, collectionsRW...)
	e.items = fmt.Sprintf("/api/v1/collections/%d/items", e.coll.ID)
	return e
}

func (e *linkRouteEnv) item(queryID int) string {
	return fmt.Sprintf("%s/%d", e.items, queryID)
}

func (e *linkRouteEnv) link(t *testing.T, data json.RawMessage) models.CollectionItemLink {
	t.Helper()
	var link models.CollectionItemLink
	if err := json.Unmarshal(data, &link); err != nil {
		t.Fatalf("decode link %s: %v", data, err)
	}
	return link
}

func (e *linkRouteEnv) storedKind(t *testing.T) models.CollectionLinkKind {
	t.Helper()
	link, err := e.s.store.GetCollectionItemLink(context.Background(), e.coll.ID, e.query.ID)
	if models.IsNotFound(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("GetCollectionItemLink: %v", err)
	}
	return link.Kind
}

func postBody(queryID int, kind string) string {
	if kind == "" {
		return fmt.Sprintf(`{"saved_query_id":%d}`, queryID)
	}
	return fmt.Sprintf(`{"saved_query_id":%d,"kind":%q}`, queryID, kind)
}

// Every row of the POST table, in precedence order, then the create, replay
// and conflict answers with the link in the response.
func TestCollectionItemPost(t *testing.T) {
	e := newLinkRouteEnv(t)
	cases := []struct {
		name  string
		token string
		path  string
		body  string
		want  int
	}{
		{"missing collection wins over an invalid kind", e.memberTok, "/api/v1/collections/999999/items", postBody(e.query.ID, "bogus"), http.StatusNotFound},
		{"no participation wins over an invalid kind", e.outsiderTok, e.items, postBody(e.query.ID, "bogus"), http.StatusForbidden},
		{"a non-participating admin is refused", e.adminTok, e.items, postBody(e.query.ID, "pin"), http.StatusForbidden},
		{"empty kind", e.memberTok, e.items, fmt.Sprintf(`{"saved_query_id":%d,"kind":""}`, e.query.ID), http.StatusBadRequest},
		{"empty kind without participation", e.outsiderTok, e.items, `{"kind":""}`, http.StatusForbidden},
		{"invalid kind", e.memberTok, e.items, postBody(e.query.ID, "bookmark"), http.StatusBadRequest},
		{"managed in a personal collection", e.creatorTok, fmt.Sprintf("/api/v1/collections/%d/items", e.personal.ID), postBody(e.query.ID, "managed"), http.StatusBadRequest},
		{"missing query wins over managed authority", e.memberTok, e.items, postBody(999999, "managed"), http.StatusNotFound},
		{"managed by a non-creator wins over source access", e.memberTok, e.items, postBody(e.lockedQuery.ID, "managed"), http.StatusForbidden},
		{"pin without source access", e.memberTok, e.items, postBody(e.lockedQuery.ID, "pin"), http.StatusForbidden},
		{"malformed body", e.memberTok, e.items, `{not json`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.expect(tc.token, http.MethodPost, tc.path, tc.body, tc.want)
		})
	}
	if kind := e.storedKind(t); kind != "" {
		t.Fatalf("a refused POST wrote a %s link", kind)
	}

	created := e.link(t, e.expect(e.memberTok, http.MethodPost, e.items, postBody(e.query.ID, ""), http.StatusCreated))
	if created.Kind != models.CollectionLinkPin || created.CollectionID != e.coll.ID || created.SavedQueryID != e.query.ID || created.AddedBy == nil || *created.AddedBy != e.member.ID || created.CreatedAt.IsZero() {
		t.Fatalf("omitted kind created %+v, want a pin by the member", created)
	}
	replay := e.link(t, e.expect(e.creatorTok, http.MethodPost, e.items, postBody(e.query.ID, "pin"), http.StatusOK))
	if *replay.AddedBy != e.member.ID || !replay.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("replay = %+v, want the stored pin unchanged", replay)
	}
	status, data := e.do(e.creatorTok, http.MethodPost, e.items, postBody(e.query.ID, "managed"))
	if status != http.StatusConflict {
		t.Errorf("managed over a pin = %d, want 409 (%s)", status, data)
	}
	if e.storedKind(t) != models.CollectionLinkPin {
		t.Error("a conflicting POST converted the link")
	}

	e.expect(e.creatorTok, http.MethodDelete, e.item(e.query.ID), "", http.StatusOK)
	managed := e.link(t, e.expect(e.creatorTok, http.MethodPost, e.items, postBody(e.query.ID, "managed"), http.StatusCreated))
	if managed.Kind != models.CollectionLinkManaged {
		t.Errorf("creator managed = %+v", managed)
	}
	e.expect(e.memberTok, http.MethodPost, e.items, postBody(e.query.ID, "pin"), http.StatusConflict)
	e.expect(e.creatorTok, http.MethodPost, e.items, postBody(e.query.ID, "managed"), http.StatusOK)
}

// Every row of the PUT table.
func TestCollectionItemPut(t *testing.T) {
	e := newLinkRouteEnv(t)
	e.expect(e.memberTok, http.MethodPost, e.items, postBody(e.query.ID, "pin"), http.StatusCreated)
	readOnly := e.token(e.creator, models.TokenScopeCollectionsRead)
	cases := []struct {
		name  string
		token string
		path  string
		body  string
		want  int
	}{
		{"read-only token", readOnly, e.item(e.query.ID), `{"kind":"managed"}`, http.StatusForbidden},
		{"missing collection", e.creatorTok, "/api/v1/collections/999999/items/1", `{"kind":"managed"}`, http.StatusNotFound},
		{"outsider", e.outsiderTok, e.item(e.query.ID), `{"kind":"managed"}`, http.StatusForbidden},
		{"missing kind", e.creatorTok, e.item(e.query.ID), `{}`, http.StatusBadRequest},
		{"invalid kind", e.creatorTok, e.item(e.query.ID), `{"kind":"bookmark"}`, http.StatusBadRequest},
		{"managed in a personal collection", e.creatorTok, fmt.Sprintf("/api/v1/collections/%d/items/%d", e.personal.ID, e.query.ID), `{"kind":"managed"}`, http.StatusBadRequest},
		{"member converting to managed", e.memberTok, e.item(e.query.ID), `{"kind":"managed"}`, http.StatusForbidden},
		{"member converting to pin", e.memberTok, e.item(e.query.ID), `{"kind":"pin"}`, http.StatusForbidden},
		{"no link", e.creatorTok, e.item(e.lockedQuery.ID), `{"kind":"managed"}`, http.StatusNotFound},
		{"invalid query id", e.creatorTok, e.items + "/abc", `{"kind":"managed"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.expect(tc.token, http.MethodPut, tc.path, tc.body, tc.want)
		})
	}
	if e.storedKind(t) != models.CollectionLinkPin {
		t.Fatal("a refused PUT changed the link")
	}

	link := e.link(t, e.expect(e.creatorTok, http.MethodPut, e.item(e.query.ID), `{"kind":"managed"}`, http.StatusOK))
	if link.Kind != models.CollectionLinkManaged {
		t.Errorf("creator conversion = %+v", link)
	}
	e.expect(e.creatorTok, http.MethodPut, e.item(e.query.ID), `{"kind":"managed"}`, http.StatusOK)
	// A global admin converts without taking part in the collection.
	link = e.link(t, e.expect(e.adminTok, http.MethodPut, e.item(e.query.ID), `{"kind":"pin"}`, http.StatusOK))
	if link.Kind != models.CollectionLinkPin {
		t.Errorf("admin conversion = %+v", link)
	}
}

// Every row of the DELETE table.
func TestCollectionItemDelete(t *testing.T) {
	e := newLinkRouteEnv(t)
	e.expect(e.memberTok, http.MethodDelete, e.item(e.query.ID), "", http.StatusNotFound)
	e.expect(e.creatorTok, http.MethodPost, e.items, postBody(e.query.ID, "managed"), http.StatusCreated)
	cases := []struct {
		name  string
		token string
		path  string
		want  int
	}{
		{"missing collection", e.memberTok, "/api/v1/collections/999999/items/1", http.StatusNotFound},
		{"outsider", e.outsiderTok, e.item(e.query.ID), http.StatusForbidden},
		{"a non-participating admin cannot remove", e.adminTok, e.item(e.query.ID), http.StatusForbidden},
		{"member removing a managed link", e.memberTok, e.item(e.query.ID), http.StatusForbidden},
		{"no link", e.memberTok, e.item(e.lockedQuery.ID), http.StatusNotFound},
		{"invalid query id", e.memberTok, e.items + "/0", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.expect(tc.token, http.MethodDelete, tc.path, "", tc.want)
		})
	}
	if e.storedKind(t) != models.CollectionLinkManaged {
		t.Fatal("a refused DELETE removed the managed link")
	}
	e.expect(e.creatorTok, http.MethodDelete, e.item(e.query.ID), "", http.StatusOK)
	e.expect(e.creatorTok, http.MethodDelete, e.item(e.query.ID), "", http.StatusNotFound)
	e.expect(e.memberTok, http.MethodPost, e.items, postBody(e.query.ID, "pin"), http.StatusCreated)
	e.expect(e.memberTok, http.MethodDelete, e.item(e.query.ID), "", http.StatusOK)
	if e.storedKind(t) != "" {
		t.Fatal("the member's pin was not removed")
	}
}

// The admin report lists the pins in shared collections with the editors a
// conversion would delegate to, and only global admins can read it.
func TestCollectionLinkReportRoute(t *testing.T) {
	e := newLinkRouteEnv(t)
	e.expect(e.memberTok, http.MethodPost, e.items, postBody(e.query.ID, "pin"), http.StatusCreated)
	e.expect(e.creatorTok, http.MethodPost, fmt.Sprintf("/api/v1/collections/%d/items", e.personal.ID), postBody(e.lockedQuery.ID, "pin"), http.StatusCreated)
	const report = "/api/v1/admin/collection-links/report"
	e.expect(e.memberTok, http.MethodGet, report, "", http.StatusForbidden)
	e.expect(e.token(e.admin, models.TokenScopeLogsRead), http.MethodGet, report, "", http.StatusForbidden)

	var rows []models.CollectionLinkReportRow
	if err := json.Unmarshal(e.expect(e.adminTok, http.MethodGet, report, "", http.StatusOK), &rows); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if len(rows) != 1 || rows[0].CollectionID != e.coll.ID || rows[0].SavedQueryID != e.query.ID || rows[0].AddedBy == nil || *rows[0].AddedBy != e.member.ID || rows[0].AddedByEmail != e.member.Email {
		t.Fatalf("report = %+v, want the member's pin in the shared collection", rows)
	}
	if len(rows[0].Editors) != 1 || rows[0].Editors[0].UserID != e.creator.ID || rows[0].Editors[0].Role != models.CollectionRoleOwner {
		t.Errorf("report editors = %+v, want the owner only", rows[0].Editors)
	}
	// Converting the pin from the report removes it from the report.
	e.expect(e.adminTok, http.MethodPut, e.item(e.query.ID), `{"kind":"managed"}`, http.StatusOK)
	if err := json.Unmarshal(e.expect(e.adminTok, http.MethodGet, report, "", http.StatusOK), &rows); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("report after conversion = %+v, want empty", rows)
	}
}

// busyStore answers every transaction with the driver's contention error, as
// SQLite does after its busy timeout and Postgres does on a serialization
// failure or deadlock.
type busyStore struct {
	store.Store
}

func (busyStore) WithTx(context.Context, func(store.StoreOps) error) error {
	return fmt.Errorf("begin transaction: %w", models.ErrDatabaseBusy)
}

// A transaction the database could not serialize answers 503 with
// Retry-After: 1 on every link route, and nothing is written.
func TestCollectionItemRoutesBusy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sqlite.New(context.Background(), sqlite.Options{Logger: logger, Config: config.SQLiteConfig{Path: sqlitetest.Path(t)}})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := &config.Config{Auth: config.AuthConfig{APITokenSecret: strings.Repeat("t", 32)}}
	s := New(ServerOptions{
		Config: cfg,
		SQLite: busyStore{Store: db},
		FS:     http.FS(fstest.MapFS{"index.html": {Data: []byte(`<base href="/" />`)}}),
		Logger: logger,
	})
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	user := mkTestUser(t, db, "busy@test.dev", models.UserRoleMember)
	tok, err := core.CreateAPIToken(context.Background(), db, logger, &cfg.Auth, user.ID, "busy", nil, collectionsRW)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	for _, rt := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/collections/1/items", `{"saved_query_id":1}`},
		{http.MethodPut, "/api/v1/collections/1/items/1", `{"kind":"pin"}`},
		{http.MethodDelete, "/api/v1/collections/1/items/1", ""},
		{http.MethodPost, "/api/v1/collections/1/members", `{"user_id":1,"role":"member"}`},
	} {
		var body io.Reader = http.NoBody
		if rt.body != "" {
			body = strings.NewReader(rt.body)
		}
		req := httptest.NewRequest(rt.method, rt.path, body)
		req.Header.Set("Authorization", "Bearer "+tok.Token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.app.Test(req)
		if err != nil {
			t.Fatalf("%s %s: %v", rt.method, rt.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") != "1" {
			t.Errorf("%s %s = %d Retry-After %q, want 503 and 1", rt.method, rt.path, resp.StatusCode, resp.Header.Get("Retry-After"))
		}
	}
}

type demotingLinkStore struct {
	store.Store
	user *models.User
}

func (s demotingLinkStore) WithTx(ctx context.Context, fn func(store.StoreOps) error) error {
	user := *s.user
	user.Role = models.UserRoleMember
	if err := s.UpdateUser(ctx, &user); err != nil {
		return err
	}
	return s.Store.WithTx(ctx, fn)
}

func TestCollectionLinkDemotedAdmin(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			e := newLinkRouteEnv(t)
			ctx := context.Background()
			if err := core.AddCollectionMember(ctx, e.s.store, e.s.log, e.coll.ID, e.creator.ID, e.admin.ID, models.CollectionRoleMember); err != nil {
				t.Fatal(err)
			}
			wantKind := models.CollectionLinkPin
			if method == http.MethodDelete {
				wantKind = models.CollectionLinkManaged
			}
			if method != http.MethodPost {
				e.expect(e.creatorTok, http.MethodPost, e.items, postBody(e.query.ID, string(wantKind)), http.StatusCreated)
			} else {
				wantKind = ""
			}
			e.s.store = demotingLinkStore{Store: e.s.store, user: e.admin}
			path, body := e.item(e.query.ID), `{"kind":"managed"}`
			if method == http.MethodPost {
				path, body = e.items, postBody(e.query.ID, "managed")
			}
			if method == http.MethodDelete {
				body = ""
			}
			e.expect(e.adminTok, method, path, body, http.StatusForbidden)
			if got := e.storedKind(t); got != wantKind {
				t.Fatalf("stored kind = %q, want %q", got, wantKind)
			}
			t.Logf("%s demoted admin returned 403; stored kind %q", method, wantKind)
		})
	}
}
