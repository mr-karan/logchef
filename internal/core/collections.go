package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrCollectionNotFound is returned when a collection cannot be located or
	// the caller has no membership relation to it.
	ErrCollectionNotFound = errors.New("collection not found")
	// ErrCollectionForbidden indicates the caller lacks the required role.
	ErrCollectionForbidden = errors.New("not allowed to perform this action on the collection")
	// ErrPersonalCollectionImmutable is returned when callers try to delete or
	// rename a personal collection.
	ErrPersonalCollectionImmutable = errors.New("personal collection cannot be deleted or renamed")
	// ErrInvalidCollectionRole is returned for unknown role strings.
	ErrInvalidCollectionRole = errors.New("invalid collection role")
	// ErrLastOwnerRemoval is returned when removing a member would leave
	// the collection ownerless. Surfaces to the API as a 409 Conflict.
	ErrLastOwnerRemoval = errors.New("cannot remove the last owner; delete the collection instead")
	// ErrCollectionTeamNotMember is returned when a collection owner who is not
	// a global admin tries to share with a team they do not belong to.
	ErrCollectionTeamNotMember = errors.New("you can only share a collection with teams you belong to")
	// ErrCollectionNotParticipant is returned on the link routes when the
	// collection exists but the caller takes no part in it. Surfaces as 403.
	ErrCollectionNotParticipant = errors.New("you do not take part in this collection")
	// ErrInvalidCollectionLinkKind is returned for a kind other than pin or
	// managed. Surfaces as 400.
	ErrInvalidCollectionLinkKind = errors.New("kind must be 'pin' or 'managed'")
	// ErrManagedLinkInPersonalCollection is returned for a managed link in a
	// personal collection, which takes pins only. Surfaces as 400.
	ErrManagedLinkInPersonalCollection = errors.New("a personal collection takes pins only")
	// ErrCollectionLinkAuthority is returned when the caller is neither the
	// saved query's creator nor a global admin and the write needs that
	// authority: creating, converting or removing a managed link. Surfaces as 403.
	ErrCollectionLinkAuthority = errors.New("only the query's creator or a global admin can manage this link")
	// ErrCollectionLinkNotFound is returned when no link exists between the
	// collection and the saved query. Surfaces as 404.
	ErrCollectionLinkNotFound = errors.New("the query is not in this collection")
	// ErrCollectionLinkKindConflict is returned by a POST when a link of the
	// other kind exists. A POST never converts a link. Surfaces as 409.
	ErrCollectionLinkKindConflict = errors.New("a link of a different kind exists")
)

// personalCollectionName is the default name for an auto-created personal
// collection. Personal collections are never shared, so a single hardcoded
// label is plenty — the owner can rename via the UI if they want.
const personalCollectionName = "My Collection"

// EnsurePersonalCollection returns the personal collection, creating it atomically with its owner.
func EnsurePersonalCollection(ctx context.Context, db store.Store, log *slog.Logger, user *models.User) (*models.Collection, error) {
	if user == nil {
		return nil, fmt.Errorf("user is required to ensure personal collection")
	}
	personal, err := db.GetPersonalCollection(ctx, user.ID)
	if err == nil {
		return personal, nil
	}
	if !models.IsNotFound(err) {
		return nil, fmt.Errorf("error fetching personal collection: %w", err)
	}

	var created *models.Collection
	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		var err error
		created, err = tx.CreateCollection(ctx, personalCollectionName, "", true, user.ID)
		if err != nil {
			return err
		}
		if err := tx.AddCollectionMember(ctx, created.ID, user.ID, models.CollectionRoleOwner, &user.ID); err != nil {
			return fmt.Errorf("error initializing personal collection: %w", err)
		}
		return nil
	})
	if err != nil {
		// Race: another goroutine wrote the row between our GetPersonalCollection
		// check and our INSERT. The unique partial index turns the second insert
		// into a unique-constraint failure; recover by reading the winner's row.
		if models.IsConflict(err) {
			personal, getErr := db.GetPersonalCollection(ctx, user.ID)
			if getErr == nil {
				return personal, nil
			}
			log.Error("personal collection unique-constraint race recovery failed",
				"insert_error", err, "fetch_error", getErr, "user_id", user.ID)
		}
		return nil, err
	}
	created.CallerRole = models.CollectionRoleOwner
	created.MemberCount = 1
	return created, nil
}

// ListCollectionsForUser returns the user's collections (auto-creating personal).
func ListCollectionsForUser(ctx context.Context, db store.Store, log *slog.Logger, user *models.User) ([]*models.Collection, error) {
	if _, err := EnsurePersonalCollection(ctx, db, log, user); err != nil {
		return nil, err
	}
	collections, err := db.ListCollectionsForUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return collections, nil
}

// CreateCollection creates a shared collection owned by the caller.
func CreateCollection(ctx context.Context, db store.Store, log *slog.Logger, name, description string, createdBy models.UserID) (*models.Collection, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	var collection *models.Collection
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		var err error
		collection, err = tx.CreateCollection(ctx, name, description, false, createdBy)
		if err != nil {
			return err
		}
		if err := tx.AddCollectionMember(ctx, collection.ID, createdBy, models.CollectionRoleOwner, &createdBy); err != nil {
			return fmt.Errorf("failed to add owner: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	collection.CallerRole = models.CollectionRoleOwner
	collection.MemberCount = 1
	log.Info("collection created", "collection_id", collection.ID, "created_by", createdBy)
	return collection, nil
}

// GetCollectionForUser fetches a collection if the user participates in it.
// A direct membership row wins and keeps its role (owner/editor/member).
// Without one, membership in any team the collection is shared with grants the
// member role. Returns ErrCollectionNotFound when neither applies. Admins do
// not get a free pass — they must participate like everyone else (matches the
// team-membership model for sources).
func GetCollectionForUser(ctx context.Context, db store.StoreOps, log *slog.Logger, collectionID int, userID models.UserID) (*models.Collection, models.CollectionRole, error) {
	collection, role, participates, err := collectionParticipation(ctx, db, log, collectionID, userID)
	if err != nil {
		return nil, "", err
	}
	if !participates {
		return nil, "", ErrCollectionNotFound
	}
	return collection, role, nil
}

// collectionParticipation loads the collection and the caller's role in it.
// participates is false when the caller has neither a membership row nor a
// team share. The link routes distinguish that case from a missing
// collection; every other route folds both into ErrCollectionNotFound.
func collectionParticipation(ctx context.Context, db store.StoreOps, log *slog.Logger, collectionID int, userID models.UserID) (collection *models.Collection, role models.CollectionRole, participates bool, err error) {
	collection, err = db.GetCollection(ctx, collectionID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, "", false, ErrCollectionNotFound
		}
		log.Error("failed to load collection", "error", err, "collection_id", collectionID)
		return nil, "", false, err
	}

	member, memberErr := db.GetCollectionMember(ctx, collectionID, userID)
	if memberErr != nil && !models.IsNotFound(memberErr) {
		log.Error("failed to load collection membership", "error", memberErr, "collection_id", collectionID, "user_id", userID)
		return nil, "", false, memberErr
	}
	if memberErr == nil {
		collection.CallerRole = member.Role
		return collection, member.Role, true, nil
	}

	viaTeam, err := db.UserHasCollectionTeamAccess(ctx, collectionID, userID)
	if err != nil {
		log.Error("failed to check collection team access", "error", err, "collection_id", collectionID, "user_id", userID)
		return nil, "", false, err
	}
	if !viaTeam {
		return collection, "", false, nil
	}
	collection.CallerRole = models.CollectionRoleMember
	return collection, models.CollectionRoleMember, true, nil
}

// lockCollection takes the collection lock inside a link, member or share
// write, so the authority it reads next cannot change before commit.
func lockCollection(ctx context.Context, tx store.StoreOps, collectionID int) error {
	if err := tx.LockCollection(ctx, collectionID); err != nil {
		if models.IsNotFound(err) {
			return ErrCollectionNotFound
		}
		return err
	}
	return nil
}

// UpdateCollection renames/redescribes a collection. Owner-only.
func UpdateCollection(ctx context.Context, db store.StoreOps, log *slog.Logger, collectionID int, userID models.UserID, name, description string) (*models.Collection, error) {
	collection, role, err := GetCollectionForUser(ctx, db, log, collectionID, userID)
	if err != nil {
		return nil, err
	}
	if role != models.CollectionRoleOwner {
		return nil, ErrCollectionForbidden
	}
	if collection.IsPersonal {
		return nil, ErrPersonalCollectionImmutable
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if err := db.UpdateCollection(ctx, collectionID, name, description); err != nil {
		return nil, err
	}
	updated, err := db.GetCollection(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	updated.CallerRole = role
	return updated, nil
}

// DeleteCollection removes a collection. Owner-only; personal collections are protected.
func DeleteCollection(ctx context.Context, db store.Store, log *slog.Logger, collectionID int, userID models.UserID) error {
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.LockMembershipWrites(ctx); err != nil {
			return err
		}
		collection, role, err := GetCollectionForUser(ctx, tx, log, collectionID, userID)
		if err != nil {
			return err
		}
		if role != models.CollectionRoleOwner {
			return ErrCollectionForbidden
		}
		if collection.IsPersonal {
			return ErrPersonalCollectionImmutable
		}
		if err := tx.DeleteCollection(ctx, collectionID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	log.Info("collection deleted", "collection_id", collectionID, "user_id", userID)
	return nil
}

// AddCollectionMember invites a user. Owner-only. The check and the write
// run under the collection lock, because membership is the authority that a
// managed link hands out.
func AddCollectionMember(ctx context.Context, db store.Store, log *slog.Logger, collectionID int, callerID, targetUserID models.UserID, role models.CollectionRole) error {
	switch role {
	case models.CollectionRoleOwner, models.CollectionRoleEditor, models.CollectionRoleMember:
		// valid
	default:
		return ErrInvalidCollectionRole
	}
	return db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := lockCollection(ctx, tx, collectionID); err != nil {
			return err
		}
		collection, callerRole, err := GetCollectionForUser(ctx, tx, log, collectionID, callerID)
		if err != nil {
			return err
		}
		if callerRole != models.CollectionRoleOwner {
			return ErrCollectionForbidden
		}
		if collection.IsPersonal {
			return ErrPersonalCollectionImmutable
		}
		if _, err := tx.GetUser(ctx, targetUserID); err != nil {
			return fmt.Errorf("user %d not found", targetUserID)
		}
		added := callerID
		return tx.AddCollectionMember(ctx, collectionID, targetUserID, role, &added)
	})
}

// RemoveCollectionMember drops a direct membership row. Owners can remove
// anyone (except the last owner); members can self-leave. Access derived from a
// team share is not affected: it ends only when the share or the team
// membership is removed.
func RemoveCollectionMember(ctx context.Context, db store.Store, log *slog.Logger, collectionID int, callerID, targetUserID models.UserID) error {
	return db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := lockCollection(ctx, tx, collectionID); err != nil {
			return err
		}
		collection, callerRole, err := GetCollectionForUser(ctx, tx, log, collectionID, callerID)
		if err != nil {
			return err
		}
		if collection.IsPersonal {
			return ErrPersonalCollectionImmutable
		}

		selfRemoval := callerID == targetUserID
		if callerRole != models.CollectionRoleOwner && !selfRemoval {
			return ErrCollectionForbidden
		}

		// Don't let the last owner leave; they should delete the collection instead.
		if !selfRemoval || callerRole == models.CollectionRoleOwner {
			members, err := tx.ListCollectionMembers(ctx, collectionID)
			if err != nil {
				return err
			}
			ownerCount := 0
			var targetIsOwner bool
			for _, m := range members {
				if m.Role == models.CollectionRoleOwner {
					ownerCount++
					if m.UserID == targetUserID {
						targetIsOwner = true
					}
				}
			}
			if targetIsOwner && ownerCount <= 1 {
				return ErrLastOwnerRemoval
			}
		}

		return tx.RemoveCollectionMember(ctx, collectionID, targetUserID)
	})
}

// requireRosterAccess gates the member and team rosters: visible only to the
// collection owner or a participating global admin (not through OAuth), not to
// editors/members who merely participate.
func requireRosterAccess(ctx context.Context, db store.StoreOps, log *slog.Logger, collectionID int, p access.Principal) error {
	if p.User == nil {
		return ErrCollectionForbidden
	}
	_, callerRole, err := GetCollectionForUser(ctx, db, log, collectionID, p.User.ID)
	if err != nil {
		return err
	}
	if callerRole != models.CollectionRoleOwner && p.RequireGlobalAdmin() != nil {
		return ErrCollectionForbidden
	}
	return nil
}

func ListCollectionMembers(ctx context.Context, db store.StoreOps, log *slog.Logger, collectionID int, p access.Principal) ([]*models.CollectionMember, error) {
	if err := requireRosterAccess(ctx, db, log, collectionID, p); err != nil {
		return nil, err
	}
	return db.ListCollectionMembers(ctx, collectionID)
}

// ListCollectionTeams returns the teams a collection is shared with. Same
// visibility rule as the member roster.
func ListCollectionTeams(ctx context.Context, db store.StoreOps, log *slog.Logger, collectionID int, p access.Principal) ([]*models.CollectionTeam, error) {
	if err := requireRosterAccess(ctx, db, log, collectionID, p); err != nil {
		return nil, err
	}
	return db.ListCollectionTeams(ctx, collectionID)
}

// AddCollectionTeam shares a collection with a team, giving every current team
// member the collection Member role. The caller must be a direct owner of the
// collection; being a global admin does not replace ownership. Owners who are
// not global admins (or who call through OAuth) may only share with teams they
// belong to, so the endpoint cannot be used to probe or reach arbitrary teams.
// Sharing is idempotent.
func AddCollectionTeam(ctx context.Context, db store.Store, log *slog.Logger, collectionID int, p access.Principal, teamID models.TeamID) error {
	if p.User == nil {
		return ErrCollectionForbidden
	}
	callerID := p.User.ID
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := lockCollection(ctx, tx, collectionID); err != nil {
			return err
		}
		collection, callerRole, err := GetCollectionForUser(ctx, tx, log, collectionID, callerID)
		if err != nil {
			return err
		}
		if callerRole != models.CollectionRoleOwner {
			return ErrCollectionForbidden
		}
		if collection.IsPersonal {
			return ErrPersonalCollectionImmutable
		}
		if p.RequireGlobalAdmin() == nil {
			if _, err := tx.GetTeam(ctx, teamID); err != nil {
				if models.IsNotFound(err) {
					return ErrTeamNotFound
				}
				return err
			}
		} else {
			// Check membership before existence so non-admins cannot tell a missing
			// team from one they do not belong to.
			isMember, err := IsTeamMember(ctx, tx, teamID, callerID)
			if err != nil {
				return err
			}
			if !isMember {
				return ErrCollectionTeamNotMember
			}
		}
		added := callerID
		return tx.AddCollectionTeam(ctx, collectionID, teamID, &added)
	})
	if err != nil {
		return err
	}
	log.Info("collection shared with team", "collection_id", collectionID, "team_id", teamID, "user_id", callerID)
	return nil
}

// RemoveCollectionTeam removes a team share. Direct-owner only. The owner does
// not need to belong to the team, so a share stays revocable after the owner
// leaves it. Direct memberships and other team shares are untouched.
func RemoveCollectionTeam(ctx context.Context, db store.Store, log *slog.Logger, collectionID int, callerID models.UserID, teamID models.TeamID) error {
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := lockCollection(ctx, tx, collectionID); err != nil {
			return err
		}
		collection, callerRole, err := GetCollectionForUser(ctx, tx, log, collectionID, callerID)
		if err != nil {
			return err
		}
		if callerRole != models.CollectionRoleOwner {
			return ErrCollectionForbidden
		}
		if collection.IsPersonal {
			return ErrPersonalCollectionImmutable
		}
		return tx.RemoveCollectionTeam(ctx, collectionID, teamID)
	})
	if err != nil {
		return err
	}
	log.Info("collection team share removed", "collection_id", collectionID, "team_id", teamID, "user_id", callerID)
	return nil
}

// linkWrite is the state a link write reads under the collection lock before
// it touches the row: the collection, whether the caller takes part in it,
// and the saved query the link points at.
type linkWrite struct {
	caller       *models.User
	collection   *models.Collection
	participates bool
	query        *models.SavedQuery
}

// beginLinkWrite locks the collection and loads what every link write needs.
// A missing collection is ErrCollectionNotFound. A missing query is left to
// the caller, because POST and DELETE answer it differently.
func beginLinkWrite(ctx context.Context, tx store.StoreOps, log *slog.Logger, collectionID int, caller *models.User, savedQueryID int) (*linkWrite, error) {
	if err := lockCollection(ctx, tx, collectionID); err != nil {
		return nil, err
	}
	caller, err := tx.GetUserForUpdate(ctx, caller.ID)
	if models.IsNotFound(err) || (err == nil && caller.Status != models.UserStatusActive) {
		return nil, ErrCollectionNotParticipant
	}
	if err != nil {
		return nil, err
	}
	collection, _, participates, err := collectionParticipation(ctx, tx, log, collectionID, caller.ID)
	if err != nil {
		return nil, err
	}
	query, err := tx.GetSavedQuery(ctx, savedQueryID)
	if err != nil && !models.IsNotFound(err) {
		return nil, err
	}
	return &linkWrite{caller: caller, collection: collection, participates: participates, query: query}, nil
}

// validateLinkKind rejects a kind outside pin and managed, and a managed link
// in a personal collection, which takes pins only.
func validateLinkKind(collection *models.Collection, kind models.CollectionLinkKind) error {
	if !kind.Valid() {
		return ErrInvalidCollectionLinkKind
	}
	if kind == models.CollectionLinkManaged && collection.IsPersonal {
		return ErrManagedLinkInPersonalCollection
	}
	return nil
}

// AddCollectionItem links a saved query into a collection. Any participant
// with source access may pin. A managed link needs owner authority over the
// query: its creator or a global admin. The checks and the insert run in one
// transaction under the collection lock. created reports whether the link was
// inserted; a repeated POST of the same kind returns the stored link
// unchanged, and a POST over a link of the other kind fails with
// ErrCollectionLinkKindConflict. A POST never converts a link.
func AddCollectionItem(ctx context.Context, db store.Store, log *slog.Logger, collectionID int, caller *models.User, savedQueryID, sortOrder int, kind models.CollectionLinkKind) (link *models.CollectionItemLink, created bool, err error) {
	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		w, err := beginLinkWrite(ctx, tx, log, collectionID, caller, savedQueryID)
		if err != nil {
			return err
		}
		if !w.participates {
			return ErrCollectionNotParticipant
		}
		if err := validateLinkKind(w.collection, kind); err != nil {
			return err
		}
		if w.query == nil {
			return ErrQueryNotFound
		}
		if kind == models.CollectionLinkManaged && !userIsCreatorOrAdmin(w.query, w.caller) {
			return ErrCollectionLinkAuthority
		}
		hasAccess, err := tx.UserHasSourceAccess(ctx, caller.ID, w.query.SourceID)
		if err != nil {
			return err
		}
		if !hasAccess {
			return ErrSourceAccessDenied
		}
		created, err = tx.AddCollectionItem(ctx, collectionID, savedQueryID, sortOrder, kind, &caller.ID)
		if err != nil {
			return err
		}
		link, err = tx.GetCollectionItemLink(ctx, collectionID, savedQueryID)
		if err != nil {
			return err
		}
		if !created && link.Kind != kind {
			return fmt.Errorf("%w: a %s link exists; use PUT to change its kind", ErrCollectionLinkKindConflict, link.Kind)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return link, created, nil
}

// UpdateCollectionItem converts a link to the given kind. The query's creator
// must take part in the collection; a global admin needs no part in it, so a
// link whose creator is gone can still be converted. The conversion statement
// names the old kind in its condition, so one changed row means the link had
// the other kind. A link already of that kind is returned unchanged.
func UpdateCollectionItem(ctx context.Context, db store.Store, log *slog.Logger, collectionID int, caller *models.User, savedQueryID int, kind models.CollectionLinkKind) (*models.CollectionItemLink, error) {
	var link *models.CollectionItemLink
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		w, err := beginLinkWrite(ctx, tx, log, collectionID, caller, savedQueryID)
		if err != nil {
			return err
		}
		admin := w.caller.Role == models.UserRoleAdmin
		if !w.participates && !admin {
			return ErrCollectionNotParticipant
		}
		if err := validateLinkKind(w.collection, kind); err != nil {
			return err
		}
		if w.query == nil {
			return ErrCollectionLinkNotFound
		}
		if !userIsCreatorOrAdmin(w.query, w.caller) {
			return ErrCollectionLinkAuthority
		}
		changed, err := tx.SetCollectionItemKind(ctx, collectionID, savedQueryID, kind)
		if err != nil {
			return err
		}
		link, err = tx.GetCollectionItemLink(ctx, collectionID, savedQueryID)
		if err != nil {
			if models.IsNotFound(err) {
				return ErrCollectionLinkNotFound
			}
			return err
		}
		if changed {
			log.Info("collection link kind changed",
				"collection_id", collectionID, "saved_query_id", savedQueryID,
				"old_kind", kind.Other(), "new_kind", kind,
				"user_id", caller.ID, "user_role", w.caller.Role)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return link, nil
}

// RemoveCollectionItem unlinks a saved query. A participant may remove a pin.
// Removing a managed link ends a delegation, so it needs the authority that
// created it: the query's creator or a global admin, who must still take part
// in the collection. The delete statement names the kinds the caller may
// remove, so a pin that became managed after the caller looked at it stays.
func RemoveCollectionItem(ctx context.Context, db store.Store, log *slog.Logger, collectionID int, caller *models.User, savedQueryID int) error {
	return db.WithTx(ctx, func(tx store.StoreOps) error {
		w, err := beginLinkWrite(ctx, tx, log, collectionID, caller, savedQueryID)
		if err != nil {
			return err
		}
		if !w.participates {
			return ErrCollectionNotParticipant
		}
		if w.query == nil {
			return ErrCollectionLinkNotFound
		}
		var removed bool
		if userIsCreatorOrAdmin(w.query, w.caller) {
			removed, err = tx.RemoveCollectionItem(ctx, collectionID, savedQueryID)
		} else {
			removed, err = tx.RemoveCollectionPin(ctx, collectionID, savedQueryID)
		}
		if err != nil {
			return err
		}
		if removed {
			return nil
		}
		if _, err := tx.GetCollectionItemLink(ctx, collectionID, savedQueryID); err != nil {
			if models.IsNotFound(err) {
				return ErrCollectionLinkNotFound
			}
			return err
		}
		return ErrCollectionLinkAuthority
	})
}

// ListDemotedCollectionLinks lists every pin in a shared collection with the
// collection's owners and editors, for global admins. Right after the kind
// migration it is exactly the links that lost delegation; pins added later
// join it, and the creation time separates them.
func ListDemotedCollectionLinks(ctx context.Context, db store.StoreOps, log *slog.Logger) ([]*models.CollectionLinkReportRow, error) {
	rows, err := db.ListSharedCollectionPins(ctx)
	if err != nil {
		log.Error("failed to list shared collection pins", "error", err)
		return nil, err
	}
	collectionIDs := make([]int, 0, len(rows))
	seen := make(map[int]bool)
	for _, row := range rows {
		if !seen[row.CollectionID] {
			collectionIDs = append(collectionIDs, row.CollectionID)
			seen[row.CollectionID] = true
		}
	}
	editorsByCollection := make(map[int][]models.CollectionMember)
	if len(collectionIDs) > 0 {
		members, err := db.ListCollectionEditors(ctx, collectionIDs)
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			editorsByCollection[m.CollectionID] = append(editorsByCollection[m.CollectionID], *m)
		}
	}
	for _, row := range rows {
		row.Editors = editorsByCollection[row.CollectionID]
		if row.Editors == nil {
			row.Editors = []models.CollectionMember{}
		}
	}
	return rows, nil
}

// ListCollectionItems returns items with the runnable flag computed for the caller.
func ListCollectionItems(ctx context.Context, db store.StoreOps, log *slog.Logger, collectionID int, callerID models.UserID) ([]*models.CollectionItem, error) {
	if _, _, err := GetCollectionForUser(ctx, db, log, collectionID, callerID); err != nil {
		return nil, err
	}
	items, err := db.ListCollectionItems(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	// Compute runnable per source by caching the access check.
	runnableBySource := make(map[models.SourceID]bool)
	for i := range items {
		sid := items[i].Query.SourceID
		runnable, ok := runnableBySource[sid]
		if !ok {
			has, err := db.UserHasSourceAccess(ctx, callerID, sid)
			if err != nil {
				return nil, err
			}
			runnable = has
			runnableBySource[sid] = has
		}
		items[i].Runnable = runnable
	}
	return items, nil
}
