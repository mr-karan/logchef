package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// linkEnv is a shared collection owned by creator with editor and member as
// direct members, a query creator wrote on a source every participant can
// reach, and a query on a source only creator can reach.
type linkEnv struct {
	db                                       *sqlite.DB
	log                                      *slog.Logger
	creator, editor, member, admin, outsider *models.User
	coll, memberPersonal                     *models.Collection
	query, lockedQuery                       *models.SavedQuery
}

func newLinkEnv(t *testing.T) *linkEnv {
	t.Helper()
	e := &linkEnv{db: newTestDB(t), log: discardLogger()}
	e.creator = newTestUser(t, e.db, "lk-creator@example.com", "Creator")
	e.editor = newTestUser(t, e.db, "lk-editor@example.com", "Editor")
	e.member = newTestUser(t, e.db, "lk-member@example.com", "Member")
	e.outsider = newTestUser(t, e.db, "lk-outsider@example.com", "Outsider")
	e.admin = newTestUser(t, e.db, "lk-admin@example.com", "Admin")
	e.admin.Role = models.UserRoleAdmin
	if err := e.db.UpdateUser(context.Background(), e.admin); err != nil {
		t.Fatalf("UpdateUser(admin): %v", err)
	}

	everyone := newTestTeam(t, e.db, "lk-everyone", models.TeamRoleMember, e.creator, e.editor, e.member, e.admin, e.outsider)
	src := newTestSource(t, e.db, "lk_logs")
	if err := e.db.AddTeamSource(context.Background(), everyone.ID, src.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	e.query = seedSavedQueryOnSource(t, e.db, src, e.creator)

	private := newTestTeam(t, e.db, "lk-private", models.TeamRoleMember, e.creator)
	lockedSrc := newTestSource(t, e.db, "lk_locked")
	if err := e.db.AddTeamSource(context.Background(), private.ID, lockedSrc.ID); err != nil {
		t.Fatalf("AddTeamSource(locked): %v", err)
	}
	e.lockedQuery = seedSavedQueryOnSource(t, e.db, lockedSrc, e.creator)

	var err error
	e.coll, err = CreateCollection(context.Background(), e.db, e.log, "Shared", "", e.creator.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	for _, m := range []struct {
		user *models.User
		role models.CollectionRole
	}{{e.editor, models.CollectionRoleEditor}, {e.member, models.CollectionRoleMember}} {
		if err := AddCollectionMember(context.Background(), e.db, e.log, e.coll.ID, e.creator.ID, m.user.ID, m.role); err != nil {
			t.Fatalf("AddCollectionMember(%s): %v", m.user.Email, err)
		}
	}
	e.memberPersonal, err = EnsurePersonalCollection(context.Background(), e.db, e.log, e.member)
	if err != nil {
		t.Fatalf("EnsurePersonalCollection: %v", err)
	}
	return e
}

func (e *linkEnv) add(caller *models.User, collectionID, queryID int, kind models.CollectionLinkKind) (*models.CollectionItemLink, bool, error) {
	return AddCollectionItem(context.Background(), e.db, e.log, collectionID, caller, queryID, 0, kind)
}

func (e *linkEnv) convert(caller *models.User, kind models.CollectionLinkKind) (*models.CollectionItemLink, error) {
	return UpdateCollectionItem(context.Background(), e.db, e.log, e.coll.ID, caller, e.query.ID, kind)
}

func (e *linkEnv) remove(caller *models.User) error {
	return RemoveCollectionItem(context.Background(), e.db, e.log, e.coll.ID, caller, e.query.ID)
}

func (e *linkEnv) storedKind(t *testing.T) models.CollectionLinkKind {
	t.Helper()
	link, err := e.db.GetCollectionItemLink(context.Background(), e.coll.ID, e.query.ID)
	if models.IsNotFound(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("GetCollectionItemLink: %v", err)
	}
	return link.Kind
}

func (e *linkEnv) setKind(t *testing.T, kind models.CollectionLinkKind) {
	t.Helper()
	if _, err := e.db.RemoveCollectionItem(context.Background(), e.coll.ID, e.query.ID); err != nil {
		t.Fatalf("reset link: %v", err)
	}
	if kind == "" {
		return
	}
	if _, err := e.db.AddCollectionItem(context.Background(), e.coll.ID, e.query.ID, 0, kind, &e.creator.ID); err != nil {
		t.Fatalf("seed %s link: %v", kind, err)
	}
}

func (e *linkEnv) canEdit(t *testing.T, user *models.User) bool {
	t.Helper()
	ok, err := UserCanEditSavedQuery(context.Background(), e.db, e.query, user)
	if err != nil {
		t.Fatalf("UserCanEditSavedQuery(%s): %v", user.Email, err)
	}
	return ok
}

// The exploit from RFC 0003: a user with view access creates a collection,
// pins the query and tries to edit it. The pin grants nothing, and the user
// cannot create a managed link.
func TestLinkAuthorityExploit(t *testing.T) {
	t.Parallel()
	e := newLinkEnv(t)
	own, err := CreateCollection(context.Background(), e.db, e.log, "Mine", "", e.member.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	link, created, err := e.add(e.member, own.ID, e.query.ID, models.CollectionLinkPin)
	if err != nil || !created || link.Kind != models.CollectionLinkPin {
		t.Fatalf("pin into own collection = %+v / %v / %v, want a new pin", link, created, err)
	}
	if e.canEdit(t, e.member) {
		t.Fatal("a pin in the member's own collection granted edit")
	}
	if _, _, err := e.add(e.member, own.ID, e.query.ID, models.CollectionLinkManaged); !errors.Is(err, ErrCollectionLinkAuthority) {
		t.Fatalf("managed link by a non-creator = %v, want ErrCollectionLinkAuthority", err)
	}
	if _, err := UpdateCollectionItem(context.Background(), e.db, e.log, own.ID, e.member, e.query.ID, models.CollectionLinkManaged); !errors.Is(err, ErrCollectionLinkAuthority) {
		t.Fatalf("conversion by a non-creator = %v, want ErrCollectionLinkAuthority", err)
	}
	if e.canEdit(t, e.member) {
		t.Fatal("the member gained edit on the query")
	}
}

// Every answer of a POST, in precedence order.
func TestAddCollectionItemAnswers(t *testing.T) {
	t.Parallel()
	e := newLinkEnv(t)
	cases := []struct {
		name   string
		caller *models.User
		coll   int
		query  int
		kind   models.CollectionLinkKind
		want   error
	}{
		{"missing collection wins over an invalid kind", e.member, 999999, e.query.ID, "bogus", ErrCollectionNotFound},
		{"no participation wins over an invalid kind", e.outsider, e.coll.ID, e.query.ID, "bogus", ErrCollectionNotParticipant},
		{"a non-participating admin is refused", e.admin, e.coll.ID, e.query.ID, models.CollectionLinkPin, ErrCollectionNotParticipant},
		{"invalid kind", e.member, e.coll.ID, e.query.ID, "bookmark", ErrInvalidCollectionLinkKind},
		{"managed in a personal collection", e.member, e.memberPersonal.ID, e.query.ID, models.CollectionLinkManaged, ErrManagedLinkInPersonalCollection},
		{"missing query wins over managed authority", e.member, e.coll.ID, 999999, models.CollectionLinkManaged, ErrQueryNotFound},
		{"managed by a non-creator wins over source access", e.member, e.coll.ID, e.lockedQuery.ID, models.CollectionLinkManaged, ErrCollectionLinkAuthority},
		{"pin without source access", e.member, e.coll.ID, e.lockedQuery.ID, models.CollectionLinkPin, ErrSourceAccessDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := e.add(tc.caller, tc.coll, tc.query, tc.kind); !errors.Is(err, tc.want) {
				t.Errorf("AddCollectionItem = %v, want %v", err, tc.want)
			}
		})
	}
	if kind := e.storedKind(t); kind != "" {
		t.Fatalf("a refused POST wrote a %s link", kind)
	}

	link, created, err := e.add(e.member, e.coll.ID, e.query.ID, models.CollectionLinkPin)
	if err != nil || !created || link.Kind != models.CollectionLinkPin || link.AddedBy == nil || *link.AddedBy != e.member.ID {
		t.Fatalf("member pin = %+v / %v / %v, want a new pin by the member", link, created, err)
	}
	replay, created, err := e.add(e.creator, e.coll.ID, e.query.ID, models.CollectionLinkPin)
	if err != nil || created || *replay.AddedBy != e.member.ID || !replay.CreatedAt.Equal(link.CreatedAt) {
		t.Fatalf("same-kind replay = %+v / %v / %v, want the stored pin unchanged", replay, created, err)
	}
	if _, _, err := e.add(e.creator, e.coll.ID, e.query.ID, models.CollectionLinkManaged); !errors.Is(err, ErrCollectionLinkKindConflict) {
		t.Fatalf("managed over a pin = %v, want ErrCollectionLinkKindConflict", err)
	}
	if e.storedKind(t) != models.CollectionLinkPin {
		t.Fatal("a conflicting POST converted the link")
	}

	e.setKind(t, "")
	link, created, err = e.add(e.creator, e.coll.ID, e.query.ID, models.CollectionLinkManaged)
	if err != nil || !created || link.Kind != models.CollectionLinkManaged {
		t.Fatalf("creator managed = %+v / %v / %v, want a new managed link", link, created, err)
	}
	if !e.canEdit(t, e.editor) || e.canEdit(t, e.member) {
		t.Fatal("a managed link should delegate edit to the editor and not to the member")
	}
	if _, _, err := e.add(e.member, e.coll.ID, e.query.ID, models.CollectionLinkPin); !errors.Is(err, ErrCollectionLinkKindConflict) {
		t.Fatalf("pin over a managed link = %v, want ErrCollectionLinkKindConflict", err)
	}
}

// Every answer of a PUT.
func TestUpdateCollectionItemAnswers(t *testing.T) {
	t.Parallel()
	e := newLinkEnv(t)
	e.setKind(t, models.CollectionLinkPin)
	creatorPersonal, err := EnsurePersonalCollection(context.Background(), e.db, e.log, e.creator)
	if err != nil {
		t.Fatalf("EnsurePersonalCollection: %v", err)
	}

	if _, err := UpdateCollectionItem(context.Background(), e.db, e.log, 999999, e.creator, e.query.ID, models.CollectionLinkManaged); !errors.Is(err, ErrCollectionNotFound) {
		t.Errorf("missing collection = %v, want ErrCollectionNotFound", err)
	}
	if _, err := e.convert(e.outsider, models.CollectionLinkManaged); !errors.Is(err, ErrCollectionNotParticipant) {
		t.Errorf("outsider = %v, want ErrCollectionNotParticipant", err)
	}
	if _, err := e.convert(e.member, "bogus"); !errors.Is(err, ErrInvalidCollectionLinkKind) {
		t.Errorf("invalid kind = %v, want ErrInvalidCollectionLinkKind", err)
	}
	if _, err := e.convert(e.member, ""); !errors.Is(err, ErrInvalidCollectionLinkKind) {
		t.Errorf("missing kind = %v, want ErrInvalidCollectionLinkKind", err)
	}
	if _, err := UpdateCollectionItem(context.Background(), e.db, e.log, creatorPersonal.ID, e.creator, e.query.ID, models.CollectionLinkManaged); !errors.Is(err, ErrManagedLinkInPersonalCollection) {
		t.Errorf("managed in personal = %v, want ErrManagedLinkInPersonalCollection", err)
	}
	for _, kind := range []models.CollectionLinkKind{models.CollectionLinkManaged, models.CollectionLinkPin} {
		if _, err := e.convert(e.editor, kind); !errors.Is(err, ErrCollectionLinkAuthority) {
			t.Errorf("editor converting to %s = %v, want ErrCollectionLinkAuthority", kind, err)
		}
	}
	if _, err := UpdateCollectionItem(context.Background(), e.db, e.log, e.coll.ID, e.creator, e.lockedQuery.ID, models.CollectionLinkManaged); !errors.Is(err, ErrCollectionLinkNotFound) {
		t.Errorf("no link = %v, want ErrCollectionLinkNotFound", err)
	}
	if e.storedKind(t) != models.CollectionLinkPin {
		t.Fatal("a refused PUT changed the link")
	}

	link, err := e.convert(e.creator, models.CollectionLinkManaged)
	if err != nil || link.Kind != models.CollectionLinkManaged {
		t.Fatalf("creator to managed = %+v / %v", link, err)
	}
	if !e.canEdit(t, e.editor) {
		t.Error("conversion to managed did not delegate edit")
	}
	if link, err = e.convert(e.creator, models.CollectionLinkManaged); err != nil || link.Kind != models.CollectionLinkManaged {
		t.Fatalf("same-kind replay = %+v / %v", link, err)
	}
	if link, err = e.convert(e.creator, models.CollectionLinkPin); err != nil || link.Kind != models.CollectionLinkPin {
		t.Fatalf("creator to pin = %+v / %v", link, err)
	}
	if e.canEdit(t, e.editor) {
		t.Error("conversion to pin kept the delegation")
	}
	// The creator loses conversion authority outside the collection.
	if err := RemoveCollectionMember(context.Background(), e.db, e.log, e.coll.ID, e.creator.ID, e.creator.ID); !errors.Is(err, ErrLastOwnerRemoval) {
		t.Fatalf("self-removal of the last owner = %v, want ErrLastOwnerRemoval", err)
	}
}

// A global admin converts without taking part in the collection, also when
// the query's creator is gone, and every change writes a log line.
func TestUpdateCollectionItemAdminBypass(t *testing.T) {
	t.Parallel()
	e := newLinkEnv(t)
	e.setKind(t, models.CollectionLinkPin)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))

	if _, _, err := e.add(e.admin, e.coll.ID, e.query.ID, models.CollectionLinkManaged); !errors.Is(err, ErrCollectionNotParticipant) {
		t.Fatalf("non-participating admin POST = %v, want ErrCollectionNotParticipant", err)
	}
	link, err := UpdateCollectionItem(context.Background(), e.db, log, e.coll.ID, e.admin, e.query.ID, models.CollectionLinkManaged)
	if err != nil || link.Kind != models.CollectionLinkManaged {
		t.Fatalf("admin conversion = %+v / %v", link, err)
	}
	for _, want := range []string{"collection link kind changed", "old_kind=pin", "new_kind=managed", "user_role=admin"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log %q lacks %q", logs.String(), want)
		}
	}
	logs.Reset()
	if _, err := UpdateCollectionItem(context.Background(), e.db, log, e.coll.ID, e.admin, e.query.ID, models.CollectionLinkManaged); err != nil {
		t.Fatalf("admin replay: %v", err)
	}
	if logs.Len() != 0 {
		t.Errorf("same-kind replay logged: %s", logs.String())
	}
	if err := RemoveCollectionItem(context.Background(), e.db, log, e.coll.ID, e.admin, e.query.ID); !errors.Is(err, ErrCollectionNotParticipant) {
		t.Errorf("non-participating admin DELETE = %v, want ErrCollectionNotParticipant", err)
	}

	// The creator's account is gone, so created_by is null.
	if err := e.db.DeleteUser(context.Background(), e.creator.ID); err != nil {
		t.Fatalf("DeleteUser(creator): %v", err)
	}
	if link, err = UpdateCollectionItem(context.Background(), e.db, log, e.coll.ID, e.admin, e.query.ID, models.CollectionLinkPin); err != nil || link.Kind != models.CollectionLinkPin {
		t.Fatalf("admin conversion of an orphaned query = %+v / %v", link, err)
	}
	if _, err := e.convert(e.editor, models.CollectionLinkManaged); !errors.Is(err, ErrCollectionLinkAuthority) {
		t.Errorf("editor converting an orphaned query = %v, want ErrCollectionLinkAuthority", err)
	}
}

// Every answer of a DELETE.
func TestRemoveCollectionItemAnswers(t *testing.T) {
	t.Parallel()
	e := newLinkEnv(t)
	if err := RemoveCollectionItem(context.Background(), e.db, e.log, 999999, e.member, e.query.ID); !errors.Is(err, ErrCollectionNotFound) {
		t.Errorf("missing collection = %v, want ErrCollectionNotFound", err)
	}
	if err := e.remove(e.outsider); !errors.Is(err, ErrCollectionNotParticipant) {
		t.Errorf("outsider = %v, want ErrCollectionNotParticipant", err)
	}
	if err := e.remove(e.member); !errors.Is(err, ErrCollectionLinkNotFound) {
		t.Errorf("no link = %v, want ErrCollectionLinkNotFound", err)
	}
	if err := RemoveCollectionItem(context.Background(), e.db, e.log, e.coll.ID, e.member, 999999); !errors.Is(err, ErrCollectionLinkNotFound) {
		t.Errorf("missing query = %v, want ErrCollectionLinkNotFound", err)
	}

	e.setKind(t, models.CollectionLinkManaged)
	if err := e.remove(e.editor); !errors.Is(err, ErrCollectionLinkAuthority) {
		t.Errorf("editor removing a managed link = %v, want ErrCollectionLinkAuthority", err)
	}
	if e.storedKind(t) != models.CollectionLinkManaged {
		t.Fatal("a refused DELETE removed the managed link")
	}
	if err := e.remove(e.creator); err != nil {
		t.Errorf("creator removing a managed link: %v", err)
	}
	e.setKind(t, models.CollectionLinkPin)
	if err := e.remove(e.member); err != nil {
		t.Errorf("member removing a pin: %v", err)
	}
	if e.storedKind(t) != "" {
		t.Fatal("the pin was not removed")
	}
}

// The four races, each order run one after the other. The answers follow
// from the row counts the storetest races pin.
func TestCollectionLinkRacesSequential(t *testing.T) {
	t.Parallel()
	e := newLinkEnv(t)
	pin, managed := models.CollectionLinkPin, models.CollectionLinkManaged

	// Remove against convert to managed.
	e.setKind(t, pin)
	if err := e.remove(e.member); err != nil {
		t.Fatalf("remove pin: %v", err)
	}
	if _, err := e.convert(e.creator, managed); !errors.Is(err, ErrCollectionLinkNotFound) {
		t.Errorf("convert after removal = %v, want ErrCollectionLinkNotFound", err)
	}
	e.setKind(t, pin)
	if _, err := e.convert(e.creator, managed); err != nil {
		t.Fatalf("convert to managed: %v", err)
	}
	if err := e.remove(e.member); !errors.Is(err, ErrCollectionLinkAuthority) || e.storedKind(t) != managed {
		t.Errorf("remove after conversion = %v (kind %s), want ErrCollectionLinkAuthority and a managed link", err, e.storedKind(t))
	}

	// Remove against convert to pin.
	e.setKind(t, managed)
	if err := e.remove(e.member); !errors.Is(err, ErrCollectionLinkAuthority) || e.storedKind(t) != managed {
		t.Errorf("remove managed = %v, want ErrCollectionLinkAuthority", err)
	}
	if _, err := e.convert(e.creator, pin); err != nil || e.storedKind(t) != pin {
		t.Errorf("convert to pin after refused removal = %v", err)
	}
	e.setKind(t, managed)
	if _, err := e.convert(e.creator, pin); err != nil {
		t.Fatalf("convert to pin: %v", err)
	}
	if err := e.remove(e.member); err != nil || e.storedKind(t) != "" {
		t.Errorf("remove after conversion to pin = %v, want removed", err)
	}

	// Two creates of different kinds.
	e.setKind(t, "")
	if _, created, err := e.add(e.member, e.coll.ID, e.query.ID, pin); err != nil || !created {
		t.Fatalf("create pin = %v / %v", created, err)
	}
	if _, _, err := e.add(e.creator, e.coll.ID, e.query.ID, managed); !errors.Is(err, ErrCollectionLinkKindConflict) || e.storedKind(t) != pin {
		t.Errorf("managed after pin = %v, want ErrCollectionLinkKindConflict and a pin", err)
	}
	e.setKind(t, "")
	if _, created, err := e.add(e.creator, e.coll.ID, e.query.ID, managed); err != nil || !created {
		t.Fatalf("create managed = %v / %v", created, err)
	}
	if _, _, err := e.add(e.member, e.coll.ID, e.query.ID, pin); !errors.Is(err, ErrCollectionLinkKindConflict) || e.storedKind(t) != managed {
		t.Errorf("pin after managed = %v, want ErrCollectionLinkKindConflict and a managed link", err)
	}

	// Two creates of the same kind.
	e.setKind(t, "")
	first, created, err := e.add(e.member, e.coll.ID, e.query.ID, pin)
	if err != nil || !created {
		t.Fatalf("first pin = %v / %v", created, err)
	}
	second, created, err := e.add(e.editor, e.coll.ID, e.query.ID, pin)
	if err != nil || created || *second.AddedBy != e.member.ID || !second.CreatedAt.Equal(first.CreatedAt) || second.SortOrder != first.SortOrder {
		t.Errorf("second pin = %+v / %v / %v, want the first link unchanged", second, created, err)
	}
}

// The report lists the pins in shared collections with the owners and
// editors a conversion would delegate to.
func TestListDemotedCollectionLinks(t *testing.T) {
	t.Parallel()
	e := newLinkEnv(t)
	e.setKind(t, models.CollectionLinkPin)
	if _, _, err := e.add(e.member, e.memberPersonal.ID, e.query.ID, models.CollectionLinkPin); err != nil {
		t.Fatalf("personal pin: %v", err)
	}
	if _, _, err := e.add(e.creator, e.coll.ID, e.lockedQuery.ID, models.CollectionLinkManaged); err != nil {
		t.Fatalf("managed link: %v", err)
	}

	rows, err := ListDemotedCollectionLinks(context.Background(), e.db, e.log)
	if err != nil {
		t.Fatalf("ListDemotedCollectionLinks: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("report = %+v, want exactly the shared pin", rows)
	}
	row := rows[0]
	if row.CollectionID != e.coll.ID || row.SavedQueryID != e.query.ID || row.QueryCreatedBy == nil || *row.QueryCreatedBy != e.creator.ID || row.AddedBy == nil || *row.AddedBy != e.creator.ID {
		t.Errorf("report row = %+v", row)
	}
	editors := map[models.UserID]models.CollectionRole{}
	for _, m := range row.Editors {
		editors[m.UserID] = m.Role
	}
	if len(editors) != 2 || editors[e.creator.ID] != models.CollectionRoleOwner || editors[e.editor.ID] != models.CollectionRoleEditor {
		t.Errorf("report editors = %+v, want the owner and the editor only", row.Editors)
	}
}

type reportBatchStore struct {
	store.StoreOps
	calls int
	ids   []int
}

func (s *reportBatchStore) ListCollectionMembers(context.Context, int) ([]*models.CollectionMember, error) {
	panic("report must batch editors")
}
func (s *reportBatchStore) ListCollectionEditors(ctx context.Context, ids []int) ([]*models.CollectionMember, error) {
	s.calls++
	s.ids = ids
	return s.StoreOps.ListCollectionEditors(ctx, ids)
}
func TestCollectionLinkReportBatchesEditors(t *testing.T) {
	e := newLinkEnv(t)
	e.setKind(t, models.CollectionLinkPin)
	ctx := context.Background()
	second, err := CreateCollection(ctx, e.db, e.log, "Second", "", e.creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []*models.SavedQuery{e.query, e.lockedQuery} {
		if _, _, err := e.add(e.creator, second.ID, q.ID, models.CollectionLinkPin); err != nil {
			t.Fatal(err)
		}
	}
	db := &reportBatchStore{StoreOps: e.db}
	rows, err := ListDemotedCollectionLinks(ctx, db, e.log)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || db.calls != 1 || len(db.ids) != 2 {
		t.Fatalf("rows=%d batch calls=%d ids=%v", len(rows), db.calls, db.ids)
	}
	for _, row := range rows {
		want := 1
		if row.CollectionID == e.coll.ID {
			want = 2
		}
		if len(row.Editors) != want {
			t.Errorf("collection %d editors=%+v", row.CollectionID, row.Editors)
		}
	}
}
