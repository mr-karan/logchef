package storetest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// linkFixture is a shared collection owned by creator, with other as an
// editor, and one saved query that creator wrote.
type linkFixture struct {
	creator, other *models.User
	coll, personal *models.Collection
	query          *models.SavedQuery
}

func seedLinks(t *testing.T, ctx context.Context, s store.Store, prefix string) linkFixture {
	t.Helper()
	f := linkFixture{
		creator: mkUser(t, ctx, s, prefix+"-creator@test.dev"),
		other:   mkUser(t, ctx, s, prefix+"-other@test.dev"),
	}
	src := mkSource(t, ctx, s, prefix)
	var err error
	f.query, err = s.CreateSavedQuery(ctx, src.ID, nil, prefix, "", models.QueryLanguageLogchefQL, models.SavedQueryEditorModeBuilder, `{"content":"status>=500"}`, &f.creator.ID)
	if err != nil {
		t.Fatalf("CreateSavedQuery: %v", err)
	}
	f.coll, err = s.CreateCollection(ctx, prefix+" shared", "", false, f.creator.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := s.AddCollectionMember(ctx, f.coll.ID, f.creator.ID, models.CollectionRoleOwner, &f.creator.ID); err != nil {
		t.Fatalf("AddCollectionMember(owner): %v", err)
	}
	if err := s.AddCollectionMember(ctx, f.coll.ID, f.other.ID, models.CollectionRoleEditor, &f.creator.ID); err != nil {
		t.Fatalf("AddCollectionMember(editor): %v", err)
	}
	f.personal, err = s.CreateCollection(ctx, "My Collection", "", true, f.other.ID)
	if err != nil {
		t.Fatalf("CreateCollection(personal): %v", err)
	}
	return f
}

func assertLink(t *testing.T, ctx context.Context, s store.StoreOps, f linkFixture, want *models.CollectionItemLink) *models.CollectionItemLink {
	t.Helper()
	got, err := s.GetCollectionItemLink(ctx, f.coll.ID, f.query.ID)
	if want == nil {
		if !errors.Is(err, models.ErrNotFound) {
			t.Fatalf("GetCollectionItemLink = %+v / %v, want ErrNotFound", got, err)
		}
		return nil
	}
	if err != nil {
		t.Fatalf("GetCollectionItemLink: %v", err)
	}
	if got.Kind != want.Kind || got.SortOrder != want.SortOrder || got.CollectionID != f.coll.ID || got.SavedQueryID != f.query.ID {
		t.Fatalf("link = %+v, want kind %s sort %d", got, want.Kind, want.SortOrder)
	}
	if (got.AddedBy == nil) != (want.AddedBy == nil) || (got.AddedBy != nil && *got.AddedBy != *want.AddedBy) {
		t.Fatalf("link added_by = %v, want %v", got.AddedBy, want.AddedBy)
	}
	if got.CreatedAt.IsZero() {
		t.Fatalf("link created_at is zero")
	}
	return got
}

// testCollectionLinks covers the link kinds on one store: the insert that
// never converts, the kind-conditioned conversion and deletes, the edit
// check that counts managed links only, and the report of pins.
func testCollectionLinks(t *testing.T, ctx context.Context, s store.Store) {
	f := seedLinks(t, ctx, s, "links")
	verifyCollectionLock(t, ctx, s, f)
	pin := verifyLinkInsert(t, ctx, s, f)
	verifyLinkConversion(t, ctx, s, f, pin)
	verifyLinkReport(t, ctx, s, f)
	verifyLinkRemoval(t, ctx, s, f)
}

func verifyCollectionLock(t *testing.T, ctx context.Context, s store.Store, f linkFixture) {
	t.Helper()
	if err := s.WithTx(ctx, func(tx store.StoreOps) error { return tx.LockCollection(ctx, f.coll.ID) }); err != nil {
		t.Fatalf("LockCollection inside WithTx: %v", err)
	}
	if err := s.WithTx(ctx, func(tx store.StoreOps) error { return tx.LockCollection(ctx, -1) }); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("LockCollection(missing) = %v, want ErrNotFound", err)
	}
}

// verifyLinkInsert inserts a pin and checks that a managed insert over it
// changes nothing: the stored link keeps its adder, order and creation time.
func verifyLinkInsert(t *testing.T, ctx context.Context, s store.Store, f linkFixture) *models.CollectionItemLink {
	t.Helper()
	inserted, err := s.AddCollectionItem(ctx, f.coll.ID, f.query.ID, 3, models.CollectionLinkPin, &f.other.ID)
	if err != nil || !inserted {
		t.Fatalf("AddCollectionItem(pin) = %v / %v, want inserted", inserted, err)
	}
	pin := assertLink(t, ctx, s, f, &models.CollectionItemLink{Kind: models.CollectionLinkPin, SortOrder: 3, AddedBy: &f.other.ID})
	inserted, err = s.AddCollectionItem(ctx, f.coll.ID, f.query.ID, 9, models.CollectionLinkManaged, &f.creator.ID)
	if err != nil || inserted {
		t.Fatalf("AddCollectionItem(managed over pin) = %v / %v, want no insert", inserted, err)
	}
	if got := assertLink(t, ctx, s, f, pin); !got.CreatedAt.Equal(pin.CreatedAt) {
		t.Errorf("replay changed created_at: %v -> %v", pin.CreatedAt, got.CreatedAt)
	}
	return pin
}

func assertEditAccess(t *testing.T, ctx context.Context, s store.Store, f linkFixture, want bool) {
	t.Helper()
	if ok, err := s.UserCanEditSavedQueryViaSharedCollection(ctx, f.other.ID, f.query.ID); err != nil || ok != want {
		t.Errorf("UserCanEditSavedQueryViaSharedCollection = %v / %v, want %v", ok, err, want)
	}
}

// verifyLinkConversion converts the pin to managed and checks that only a
// managed link delegates edit, and that a pin-only delete leaves it alone.
func verifyLinkConversion(t *testing.T, ctx context.Context, s store.Store, f linkFixture, pin *models.CollectionItemLink) {
	t.Helper()
	assertEditAccess(t, ctx, s, f, false)
	if changed, err := s.SetCollectionItemKind(ctx, f.coll.ID, f.query.ID, models.CollectionLinkManaged); err != nil || !changed {
		t.Fatalf("SetCollectionItemKind(managed) = %v / %v, want changed", changed, err)
	}
	if changed, err := s.SetCollectionItemKind(ctx, f.coll.ID, f.query.ID, models.CollectionLinkManaged); err != nil || changed {
		t.Fatalf("SetCollectionItemKind(managed again) = %v / %v, want unchanged", changed, err)
	}
	assertEditAccess(t, ctx, s, f, true)
	items, err := s.ListCollectionItems(ctx, f.coll.ID)
	if err != nil || len(items) != 1 || items[0].Kind != models.CollectionLinkManaged {
		t.Fatalf("ListCollectionItems = %+v / %v, want one managed item", items, err)
	}
	if removed, err := s.RemoveCollectionPin(ctx, f.coll.ID, f.query.ID); err != nil || removed {
		t.Fatalf("RemoveCollectionPin(managed) = %v / %v, want nothing removed", removed, err)
	}
	managed := *pin
	managed.Kind = models.CollectionLinkManaged
	assertLink(t, ctx, s, f, &managed)
}

// verifyLinkReport checks that the report lists pins in shared collections
// only, with the creator and the adder, and that it follows conversions.
func verifyLinkReport(t *testing.T, ctx context.Context, s store.Store, f linkFixture) {
	t.Helper()
	second := seedLinks(t, ctx, s, "batch-editors")
	if err := s.AddCollectionMember(ctx, second.coll.ID, f.other.ID, models.CollectionRoleMember, &second.creator.ID); err != nil {
		t.Fatal(err)
	}
	members, err := s.ListCollectionEditors(ctx, []int{f.coll.ID, second.coll.ID})
	if err != nil {
		t.Fatal(err)
	}
	want := map[[2]int]models.CollectionRole{
		{f.coll.ID, int(f.creator.ID)}:           models.CollectionRoleOwner,
		{f.coll.ID, int(f.other.ID)}:             models.CollectionRoleEditor,
		{second.coll.ID, int(second.creator.ID)}: models.CollectionRoleOwner,
		{second.coll.ID, int(second.other.ID)}:   models.CollectionRoleEditor,
	}
	if len(members) != len(want) {
		t.Fatalf("batch editors = %+v, want four owners/editors", members)
	}
	for _, m := range members {
		key := [2]int{m.CollectionID, int(m.UserID)}
		if want[key] != m.Role || m.Email == "" || m.CreatedAt.IsZero() {
			t.Errorf("unexpected batch editor %+v", m)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Errorf("missing batch editors %v", want)
	}
	members, err = s.ListCollectionEditors(ctx, nil)
	if err != nil || len(members) != 0 {
		t.Fatalf("empty batch = %+v / %v", members, err)
	}

	if inserted, err := s.AddCollectionItem(ctx, f.personal.ID, f.query.ID, 0, models.CollectionLinkPin, &f.other.ID); err != nil || !inserted {
		t.Fatalf("AddCollectionItem(personal pin) = %v / %v", inserted, err)
	}
	assertReport(t, ctx, s, f, false)
	if changed, err := s.SetCollectionItemKind(ctx, f.coll.ID, f.query.ID, models.CollectionLinkPin); err != nil || !changed {
		t.Fatalf("SetCollectionItemKind(pin) = %v / %v, want changed", changed, err)
	}
	assertReport(t, ctx, s, f, true)
	assertEditAccess(t, ctx, s, f, false)
}

func assertReport(t *testing.T, ctx context.Context, s store.Store, f linkFixture, wantListed bool) {
	t.Helper()
	rows, err := s.ListSharedCollectionPins(ctx)
	if err != nil {
		t.Fatalf("ListSharedCollectionPins: %v", err)
	}
	var found *models.CollectionLinkReportRow
	for _, r := range rows {
		if r.CollectionID == f.personal.ID {
			t.Errorf("report lists a personal collection pin: %+v", r)
		}
		if r.CollectionID == f.coll.ID && r.SavedQueryID == f.query.ID {
			found = r
		}
	}
	if (found != nil) != wantListed {
		t.Fatalf("report lists link = %v, want %v (%+v)", found != nil, wantListed, rows)
	}
	if found == nil {
		return
	}
	if found.CollectionName != f.coll.Name || found.SavedQueryName != f.query.Name ||
		found.QueryCreatedBy == nil || *found.QueryCreatedBy != f.creator.ID || found.QueryCreatorEmail != f.creator.Email ||
		found.AddedBy == nil || *found.AddedBy != f.other.ID || found.AddedByEmail != f.other.Email || found.CreatedAt.IsZero() {
		t.Errorf("report row = %+v, want names, creator %s and adder %s", found, f.creator.Email, f.other.Email)
	}
}

// verifyLinkRemoval removes the link and checks that later writes find no row.
func verifyLinkRemoval(t *testing.T, ctx context.Context, s store.Store, f linkFixture) {
	t.Helper()
	if removed, err := s.RemoveCollectionItem(ctx, f.coll.ID, f.query.ID); err != nil || !removed {
		t.Fatalf("RemoveCollectionItem = %v / %v, want removed", removed, err)
	}
	if removed, err := s.RemoveCollectionItem(ctx, f.coll.ID, f.query.ID); err != nil || removed {
		t.Fatalf("RemoveCollectionItem(again) = %v / %v, want nothing removed", removed, err)
	}
	if changed, err := s.SetCollectionItemKind(ctx, f.coll.ID, f.query.ID, models.CollectionLinkManaged); err != nil || changed {
		t.Fatalf("SetCollectionItemKind(missing) = %v / %v, want unchanged", changed, err)
	}
	assertLink(t, ctx, s, f, nil)
}

// linkWrite is one side of a race: a statement run inside a locked
// transaction, reporting whether it changed a row.
type linkWrite func(ctx context.Context, tx store.StoreOps, f linkFixture) (bool, error)

type linkRace struct {
	name          string
	seed          models.CollectionLinkKind // "" seeds no link
	first, second linkWrite
	wantFirst     bool
	wantSecond    bool
	wantKind      models.CollectionLinkKind // "" means no link afterwards
	// wantFirstRow keeps the link exactly as the first write left it: the
	// same-kind replay must not touch added_by, sort_order or created_at.
	wantFirstRow bool
}

// RunCollectionLinkRaces runs the four link races from RFC 0003 on two store
// instances, one per writer, in both lock acquisition orders. The second
// writer must block until the first commits; the assertions then pin the row
// counts and the stored link, which are what the HTTP answers derive from.
func RunCollectionLinkRaces(t *testing.T, open Opener) {
	ctx := context.Background()
	a, b := open(t), open(t)
	removePin := func(ctx context.Context, tx store.StoreOps, f linkFixture) (bool, error) {
		return tx.RemoveCollectionPin(ctx, f.coll.ID, f.query.ID)
	}
	convertTo := func(kind models.CollectionLinkKind) linkWrite {
		return func(ctx context.Context, tx store.StoreOps, f linkFixture) (bool, error) {
			return tx.SetCollectionItemKind(ctx, f.coll.ID, f.query.ID, kind)
		}
	}
	createAs := func(kind models.CollectionLinkKind, byCreator bool, sortOrder int) linkWrite {
		return func(ctx context.Context, tx store.StoreOps, f linkFixture) (bool, error) {
			adder := f.other.ID
			if byCreator {
				adder = f.creator.ID
			}
			return tx.AddCollectionItem(ctx, f.coll.ID, f.query.ID, sortOrder, kind, &adder)
		}
	}
	pin, managed := models.CollectionLinkPin, models.CollectionLinkManaged
	races := []linkRace{
		{name: "remove pin, then convert to managed", seed: pin, first: removePin, second: convertTo(managed), wantFirst: true, wantSecond: false, wantKind: ""},
		{name: "convert to managed, then remove pin", seed: pin, first: convertTo(managed), second: removePin, wantFirst: true, wantSecond: false, wantKind: managed},
		{name: "remove pin on managed, then convert to pin", seed: managed, first: removePin, second: convertTo(pin), wantFirst: false, wantSecond: true, wantKind: pin},
		{name: "convert to pin, then remove pin", seed: managed, first: convertTo(pin), second: removePin, wantFirst: true, wantSecond: true, wantKind: ""},
		{name: "create pin, then create managed", first: createAs(pin, false, 1), second: createAs(managed, true, 2), wantFirst: true, wantSecond: false, wantKind: pin, wantFirstRow: true},
		{name: "create managed, then create pin", first: createAs(managed, true, 1), second: createAs(pin, false, 2), wantFirst: true, wantSecond: false, wantKind: managed, wantFirstRow: true},
		{name: "create pin twice", first: createAs(pin, false, 1), second: createAs(pin, true, 2), wantFirst: true, wantSecond: false, wantKind: pin, wantFirstRow: true},
		{name: "create managed twice", first: createAs(managed, true, 1), second: createAs(managed, false, 2), wantFirst: true, wantSecond: false, wantKind: managed, wantFirstRow: true},
	}
	for i, race := range races {
		t.Run(race.name, func(t *testing.T) {
			f := seedLinks(t, ctx, a, fmt.Sprintf("race-%d", i))
			if race.seed != "" {
				if _, err := a.AddCollectionItem(ctx, f.coll.ID, f.query.ID, 0, race.seed, &f.creator.ID); err != nil {
					t.Fatalf("seed link: %v", err)
				}
			}
			gotFirst, gotSecond, afterFirst := runLinkRace(t, ctx, a, b, f, race.first, race.second)
			if gotFirst != race.wantFirst || gotSecond != race.wantSecond {
				t.Errorf("changed rows = first %v, second %v; want first %v, second %v", gotFirst, gotSecond, race.wantFirst, race.wantSecond)
			}
			link, err := a.GetCollectionItemLink(ctx, f.coll.ID, f.query.ID)
			switch {
			case race.wantKind == "":
				if !errors.Is(err, models.ErrNotFound) {
					t.Errorf("link after race = %+v / %v, want none", link, err)
				}
			case err != nil:
				t.Errorf("GetCollectionItemLink: %v", err)
			case link.Kind != race.wantKind:
				t.Errorf("link kind after race = %s, want %s", link.Kind, race.wantKind)
			case race.wantFirstRow && (afterFirst == nil || *link != *afterFirst) && !sameLink(link, afterFirst):
				t.Errorf("second write changed the stored link: %+v -> %+v", afterFirst, link)
			}
		})
	}
}

func sameLink(a, b *models.CollectionItemLink) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Kind == b.Kind && a.SortOrder == b.SortOrder && a.CreatedAt.Equal(b.CreatedAt) &&
		(a.AddedBy == nil) == (b.AddedBy == nil) && (a.AddedBy == nil || *a.AddedBy == *b.AddedBy)
}

type raceResult struct {
	changed bool
	err     error
}

// runLinkRace runs first inside a locked transaction on store a, starts
// second on store b while that lock is held, and only then lets the first
// commit. It reports each write's changed flag and the link as the first
// transaction saw it after its write.
func runLinkRace(t *testing.T, ctx context.Context, a, b store.Store, f linkFixture, first, second linkWrite) (gotFirst, gotSecond bool, afterFirst *models.CollectionItemLink) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	secondDone := make(chan raceResult, 1)
	err := a.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.LockCollection(ctx, f.coll.ID); err != nil {
			return err
		}
		go func() {
			var changed bool
			err := b.WithTx(ctx, func(tx2 store.StoreOps) error {
				if err := tx2.LockCollection(ctx, f.coll.ID); err != nil {
					return err
				}
				var err error
				changed, err = second(ctx, tx2, f)
				return err
			})
			secondDone <- raceResult{changed: changed, err: err}
		}()
		select {
		case r := <-secondDone:
			t.Errorf("second write finished while the first held the lock: %+v", r)
			secondDone <- r
		case <-time.After(150 * time.Millisecond):
		}
		var err error
		gotFirst, err = first(ctx, tx, f)
		if err != nil {
			return err
		}
		afterFirst, err = tx.GetCollectionItemLink(ctx, f.coll.ID, f.query.ID)
		if errors.Is(err, models.ErrNotFound) {
			afterFirst = nil
			return nil
		}
		return err
	})
	if err != nil {
		t.Fatalf("first transaction: %v", err)
	}
	select {
	case r := <-secondDone:
		if r.err != nil {
			t.Fatalf("second transaction: %v", r.err)
		}
		gotSecond = r.changed
	case <-ctx.Done():
		t.Fatalf("second transaction did not finish after the first committed")
	}
	return gotFirst, gotSecond, afterFirst
}
