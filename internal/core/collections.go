package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mr-karan/logchef/internal/sqlite"
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
)

const personalCollectionName = "My Collection"

// EnsurePersonalCollection returns the user's personal collection, creating it
// (and the owner-membership row) on first call.
func EnsurePersonalCollection(ctx context.Context, db *sqlite.DB, log *slog.Logger, userID models.UserID) (*models.Collection, error) {
	personal, err := db.GetPersonalCollection(ctx, userID)
	if err == nil {
		return personal, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("error fetching personal collection: %w", err)
	}

	created, err := db.CreateCollection(ctx, personalCollectionName, "", true, userID)
	if err != nil {
		return nil, err
	}
	if err := db.AddCollectionMember(ctx, created.ID, userID, models.CollectionRoleOwner, &userID); err != nil {
		log.Error("failed to add owner to personal collection", "error", err, "user_id", userID, "collection_id", created.ID)
		// Best-effort cleanup so we don't leak an orphaned collection row.
		_ = db.DeleteCollection(ctx, created.ID)
		return nil, fmt.Errorf("error initializing personal collection: %w", err)
	}
	created.CallerRole = models.CollectionRoleOwner
	created.MemberCount = 1
	return created, nil
}

// ListCollectionsForUser returns the user's collections (auto-creating personal).
func ListCollectionsForUser(ctx context.Context, db *sqlite.DB, log *slog.Logger, userID models.UserID) ([]*models.Collection, error) {
	if _, err := EnsurePersonalCollection(ctx, db, log, userID); err != nil {
		return nil, err
	}
	collections, err := db.ListCollectionsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return collections, nil
}

// CreateCollection creates a shared collection owned by the caller.
func CreateCollection(ctx context.Context, db *sqlite.DB, log *slog.Logger, name, description string, createdBy models.UserID) (*models.Collection, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	collection, err := db.CreateCollection(ctx, name, description, false, createdBy)
	if err != nil {
		return nil, err
	}
	if err := db.AddCollectionMember(ctx, collection.ID, createdBy, models.CollectionRoleOwner, &createdBy); err != nil {
		_ = db.DeleteCollection(ctx, collection.ID)
		return nil, fmt.Errorf("failed to add owner: %w", err)
	}
	collection.CallerRole = models.CollectionRoleOwner
	collection.MemberCount = 1
	log.Info("collection created", "collection_id", collection.ID, "created_by", createdBy)
	return collection, nil
}

// GetCollectionForUser fetches a collection if the user is a member.
// Returns ErrCollectionNotFound when the user has no membership row.
func GetCollectionForUser(ctx context.Context, db *sqlite.DB, log *slog.Logger, collectionID int, userID models.UserID, isAdmin bool) (*models.Collection, models.CollectionRole, error) {
	collection, err := db.GetCollection(ctx, collectionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || sqlite.IsNotFoundError(err) {
			return nil, "", ErrCollectionNotFound
		}
		log.Error("failed to load collection", "error", err, "collection_id", collectionID)
		return nil, "", err
	}

	member, memberErr := db.GetCollectionMember(ctx, collectionID, userID)
	if memberErr != nil && !errors.Is(memberErr, sql.ErrNoRows) {
		log.Error("failed to load collection membership", "error", memberErr, "collection_id", collectionID, "user_id", userID)
		return nil, "", memberErr
	}

	if member == nil || errors.Is(memberErr, sql.ErrNoRows) {
		if !isAdmin {
			return nil, "", ErrCollectionNotFound
		}
		// Global admins can read any collection but don't get an implicit role.
		return collection, "", nil
	}
	collection.CallerRole = member.Role
	return collection, member.Role, nil
}

// UpdateCollection renames/redescribes a collection. Owner-only.
func UpdateCollection(ctx context.Context, db *sqlite.DB, log *slog.Logger, collectionID int, userID models.UserID, isAdmin bool, name, description string) (*models.Collection, error) {
	collection, role, err := GetCollectionForUser(ctx, db, log, collectionID, userID, isAdmin)
	if err != nil {
		return nil, err
	}
	if !isAdmin && role != models.CollectionRoleOwner {
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
func DeleteCollection(ctx context.Context, db *sqlite.DB, log *slog.Logger, collectionID int, userID models.UserID, isAdmin bool) error {
	collection, role, err := GetCollectionForUser(ctx, db, log, collectionID, userID, isAdmin)
	if err != nil {
		return err
	}
	if !isAdmin && role != models.CollectionRoleOwner {
		return ErrCollectionForbidden
	}
	if collection.IsPersonal {
		return ErrPersonalCollectionImmutable
	}
	if err := db.DeleteCollection(ctx, collectionID); err != nil {
		return err
	}
	log.Info("collection deleted", "collection_id", collectionID, "user_id", userID)
	return nil
}

// AddCollectionMember invites a user. Owner-only.
func AddCollectionMember(ctx context.Context, db *sqlite.DB, log *slog.Logger, collectionID int, callerID models.UserID, isAdmin bool, targetUserID models.UserID, role models.CollectionRole) error {
	if role != models.CollectionRoleOwner && role != models.CollectionRoleMember {
		return ErrInvalidCollectionRole
	}
	collection, callerRole, err := GetCollectionForUser(ctx, db, log, collectionID, callerID, isAdmin)
	if err != nil {
		return err
	}
	if !isAdmin && callerRole != models.CollectionRoleOwner {
		return ErrCollectionForbidden
	}
	if collection.IsPersonal {
		return ErrPersonalCollectionImmutable
	}
	if _, err := db.GetUser(ctx, targetUserID); err != nil {
		return fmt.Errorf("user %d not found", targetUserID)
	}
	added := callerID
	return db.AddCollectionMember(ctx, collectionID, targetUserID, role, &added)
}

// RemoveCollectionMember drops a member. Owners can remove anyone (except the
// last owner); members can self-leave.
func RemoveCollectionMember(ctx context.Context, db *sqlite.DB, log *slog.Logger, collectionID int, callerID models.UserID, isAdmin bool, targetUserID models.UserID) error {
	collection, callerRole, err := GetCollectionForUser(ctx, db, log, collectionID, callerID, isAdmin)
	if err != nil {
		return err
	}
	if collection.IsPersonal {
		return ErrPersonalCollectionImmutable
	}

	selfRemoval := callerID == targetUserID
	if !isAdmin && callerRole != models.CollectionRoleOwner && !selfRemoval {
		return ErrCollectionForbidden
	}

	// Don't let the last owner leave; they should delete the collection instead.
	if !selfRemoval || callerRole == models.CollectionRoleOwner {
		members, err := db.ListCollectionMembers(ctx, collectionID)
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
			return fmt.Errorf("cannot remove the last owner — delete the collection instead")
		}
	}

	return db.RemoveCollectionMember(ctx, collectionID, targetUserID)
}

// ListCollectionMembers returns members visible to anyone in the collection.
func ListCollectionMembers(ctx context.Context, db *sqlite.DB, log *slog.Logger, collectionID int, callerID models.UserID, isAdmin bool) ([]*models.CollectionMember, error) {
	if _, _, err := GetCollectionForUser(ctx, db, log, collectionID, callerID, isAdmin); err != nil {
		return nil, err
	}
	return db.ListCollectionMembers(ctx, collectionID)
}

// AddCollectionItem references a saved query in a collection. Owner-only.
// The caller must also have visibility to the saved query (source access via
// any team) so they can't shove queries that they themselves can't see.
func AddCollectionItem(ctx context.Context, db *sqlite.DB, log *slog.Logger, collectionID int, callerID models.UserID, isAdmin bool, savedQueryID, sortOrder int) error {
	_, callerRole, err := GetCollectionForUser(ctx, db, log, collectionID, callerID, isAdmin)
	if err != nil {
		return err
	}
	if !isAdmin && callerRole != models.CollectionRoleOwner {
		return ErrCollectionForbidden
	}

	query, err := db.GetSavedQuery(ctx, savedQueryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || sqlite.IsNotFoundError(err) {
			return ErrQueryNotFound
		}
		return err
	}
	if !isAdmin {
		hasAccess, err := db.UserHasSourceAccess(ctx, callerID, query.SourceID)
		if err != nil {
			return err
		}
		if !hasAccess {
			return fmt.Errorf("you do not have access to source %d", query.SourceID)
		}
	}
	added := callerID
	return db.AddCollectionItem(ctx, collectionID, savedQueryID, sortOrder, &added)
}

// RemoveCollectionItem unlinks a saved query. Owner-only.
func RemoveCollectionItem(ctx context.Context, db *sqlite.DB, log *slog.Logger, collectionID int, callerID models.UserID, isAdmin bool, savedQueryID int) error {
	_, callerRole, err := GetCollectionForUser(ctx, db, log, collectionID, callerID, isAdmin)
	if err != nil {
		return err
	}
	if !isAdmin && callerRole != models.CollectionRoleOwner {
		return ErrCollectionForbidden
	}
	return db.RemoveCollectionItem(ctx, collectionID, savedQueryID)
}

// ListCollectionItems returns items with the runnable flag computed for the caller.
func ListCollectionItems(ctx context.Context, db *sqlite.DB, log *slog.Logger, collectionID int, callerID models.UserID, isAdmin bool) ([]*models.CollectionItem, error) {
	if _, _, err := GetCollectionForUser(ctx, db, log, collectionID, callerID, isAdmin); err != nil {
		return nil, err
	}
	items, err := db.ListCollectionItems(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	if isAdmin {
		for i := range items {
			items[i].Runnable = true
		}
		return items, nil
	}
	// Compute runnable per source by caching the access check.
	access := make(map[models.SourceID]bool)
	for i := range items {
		sid := items[i].Query.SourceID
		runnable, ok := access[sid]
		if !ok {
			has, err := db.UserHasSourceAccess(ctx, callerID, sid)
			if err != nil {
				return nil, err
			}
			runnable = has
			access[sid] = has
		}
		items[i].Runnable = runnable
	}
	return items, nil
}
