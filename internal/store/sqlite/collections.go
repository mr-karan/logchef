package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateCollection inserts a new collection. The caller is automatically
// added as the owner via AddCollectionMember in core.
func (db *DB) CreateCollection(ctx context.Context, name, description string, isPersonal bool, createdBy models.UserID) (*models.Collection, error) {
	row, err := db.writeQueries.CreateCollection(ctx, sqlc.CreateCollectionParams{
		Name:        name,
		Description: nullString(description),
		IsPersonal:  boolToInt(isPersonal),
		CreatedBy:   sql.NullInt64{Int64: int64(createdBy), Valid: true},
	})
	if err != nil {
		if IsUniqueConstraintError(err) {
			// Hit the one-personal-collection-per-user partial unique index.
			return nil, fmt.Errorf("%w: personal collection already exists for user %d", ErrUniqueConstraint, createdBy)
		}
		db.log.Error("failed to create collection", "error", err, "name", name, "is_personal", isPersonal)
		return nil, fmt.Errorf("error creating collection: %w", err)
	}

	return collectionToModel(sqlc.Collection{
		ID:          row.ID,
		Name:        name,
		Description: nullString(description),
		IsPersonal:  boolToInt(isPersonal),
		CreatedBy:   sql.NullInt64{Int64: int64(createdBy), Valid: true},
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}), nil
}

// GetCollection returns a collection by id, or ErrNotFound if missing.
func (db *DB) GetCollection(ctx context.Context, collectionID int) (*models.Collection, error) {
	row, err := db.readQueries.GetCollection(ctx, int64(collectionID))
	if err != nil {
		return nil, fmt.Errorf("getting collection id %d: %w", collectionID, translateNotFound(err))
	}
	c := collectionToModel(sqlc.Collection{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		IsPersonal:  row.IsPersonal,
		CreatedBy:   row.CreatedBy,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	})
	c.MemberCount = int(row.MemberCount)
	c.TeamCount = int(row.TeamCount)
	c.ItemCount = int(row.ItemCount)
	return c, nil
}

// GetPersonalCollection returns the user's personal collection, or
// models.ErrNotFound if one has not been created yet.
func (db *DB) GetPersonalCollection(ctx context.Context, userID models.UserID) (*models.Collection, error) {
	row, err := db.readQueries.GetPersonalCollection(ctx, sql.NullInt64{Int64: int64(userID), Valid: true})
	if err != nil {
		return nil, fmt.Errorf("getting personal collection for user %d: %w", userID, translateNotFound(err))
	}
	return collectionToModel(row), nil
}

// UpdateCollection updates name and description.
func (db *DB) UpdateCollection(ctx context.Context, collectionID int, name, description string) error {
	if err := db.writeQueries.UpdateCollection(ctx, sqlc.UpdateCollectionParams{
		Name:        name,
		Description: nullString(description),
		ID:          int64(collectionID),
	}); err != nil {
		db.log.Error("failed to update collection", "error", err, "collection_id", collectionID)
		return fmt.Errorf("error updating collection: %w", err)
	}
	return nil
}

// DeleteCollection removes a collection.
func (db *DB) DeleteCollection(ctx context.Context, collectionID int) error {
	if err := db.writeQueries.DeleteCollection(ctx, int64(collectionID)); err != nil {
		db.log.Error("failed to delete collection", "error", err, "collection_id", collectionID)
		return fmt.Errorf("error deleting collection: %w", err)
	}
	return nil
}

// ListCollectionsForUser returns every collection the user can see through a
// direct membership or a team share, once each.
func (db *DB) ListCollectionsForUser(ctx context.Context, userID models.UserID) ([]*models.Collection, error) {
	rows, err := db.readQueries.ListCollectionsForUser(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list collections for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing collections: %w", err)
	}
	out := make([]*models.Collection, 0, len(rows))
	for i := range rows {
		r := rows[i]
		c := collectionToModel(sqlc.Collection{
			ID:          r.ID,
			Name:        r.Name,
			Description: r.Description,
			IsPersonal:  r.IsPersonal,
			CreatedBy:   r.CreatedBy,
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
		})
		c.CallerRole = models.CollectionRole(r.CallerRole)
		c.MemberCount, c.TeamCount, c.ItemCount = int(r.MemberCount), int(r.TeamCount), int(r.ItemCount)
		out = append(out, c)
	}
	return out, nil
}

// AddCollectionMember adds a user to a collection (idempotent).
func (db *DB) AddCollectionMember(ctx context.Context, collectionID int, userID models.UserID, role models.CollectionRole, addedBy *models.UserID) error {
	params := sqlc.AddCollectionMemberParams{
		CollectionID: int64(collectionID),
		UserID:       int64(userID),
		Role:         string(role),
	}
	if addedBy != nil {
		params.AddedBy = sql.NullInt64{Int64: int64(*addedBy), Valid: true}
	}
	if err := db.writeQueries.AddCollectionMember(ctx, params); err != nil {
		// The SQL itself uses ON CONFLICT DO NOTHING, so re-adding an existing
		// member is a no-op at the DB level — any error here is unexpected.
		db.log.Error("failed to add collection member", "error", err, "collection_id", collectionID, "user_id", userID)
		return fmt.Errorf("error adding collection member: %w", err)
	}
	return nil
}

// GetCollectionMember returns a single membership row, or models.ErrNotFound if absent.
func (db *DB) GetCollectionMember(ctx context.Context, collectionID int, userID models.UserID) (*models.CollectionMember, error) {
	row, err := db.readQueries.GetCollectionMember(ctx, sqlc.GetCollectionMemberParams{
		CollectionID: int64(collectionID),
		UserID:       int64(userID),
	})
	if err != nil {
		return nil, fmt.Errorf("getting collection %d member %d: %w", collectionID, userID, translateNotFound(err))
	}
	member := &models.CollectionMember{
		CollectionID: int(row.CollectionID),
		UserID:       models.UserID(row.UserID),
		Role:         models.CollectionRole(row.Role),
		CreatedAt:    row.CreatedAt,
	}
	if row.AddedBy.Valid {
		uid := models.UserID(row.AddedBy.Int64)
		member.AddedBy = &uid
	}
	return member, nil
}

// ListCollectionMembers returns members of a collection with user details.
func (db *DB) ListCollectionMembers(ctx context.Context, collectionID int) ([]*models.CollectionMember, error) {
	rows, err := db.readQueries.ListCollectionMembers(ctx, int64(collectionID))
	if err != nil {
		return nil, fmt.Errorf("error listing collection members: %w", err)
	}
	out := make([]*models.CollectionMember, 0, len(rows))
	for _, r := range rows {
		m := &models.CollectionMember{
			CollectionID: int(r.CollectionID),
			UserID:       models.UserID(r.UserID),
			Role:         models.CollectionRole(r.Role),
			CreatedAt:    r.CreatedAt,
			Email:        r.Email,
			FullName:     r.FullName,
		}
		if r.AddedBy.Valid {
			uid := models.UserID(r.AddedBy.Int64)
			m.AddedBy = &uid
		}
		out = append(out, m)
	}
	return out, nil
}

func (db *DB) ListCollectionEditors(ctx context.Context, collectionIDs []int) ([]*models.CollectionMember, error) {
	ids := make([]int64, len(collectionIDs))
	for i, id := range collectionIDs {
		ids[i] = int64(id)
	}

	rows, err := db.readQueries.ListCollectionEditors(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("error listing collection members: %w", err)
	}
	out := make([]*models.CollectionMember, 0, len(rows))
	for _, r := range rows {
		m := &models.CollectionMember{
			CollectionID: int(r.CollectionID),
			UserID:       models.UserID(r.UserID),
			Role:         models.CollectionRole(r.Role),
			CreatedAt:    r.CreatedAt,
			Email:        r.Email,
			FullName:     r.FullName,
		}
		if r.AddedBy.Valid {
			uid := models.UserID(r.AddedBy.Int64)
			m.AddedBy = &uid
		}
		out = append(out, m)
	}
	return out, nil
}

// RemoveCollectionMember drops a member from a collection.
func (db *DB) RemoveCollectionMember(ctx context.Context, collectionID int, userID models.UserID) error {
	if err := db.writeQueries.RemoveCollectionMember(ctx, sqlc.RemoveCollectionMemberParams{
		CollectionID: int64(collectionID),
		UserID:       int64(userID),
	}); err != nil {
		return fmt.Errorf("error removing collection member: %w", err)
	}
	return nil
}

// AddCollectionTeam shares a collection with a team (idempotent).
func (db *DB) AddCollectionTeam(ctx context.Context, collectionID int, teamID models.TeamID, addedBy *models.UserID) error {
	params := sqlc.AddCollectionTeamParams{
		CollectionID: int64(collectionID),
		TeamID:       int64(teamID),
	}
	if addedBy != nil {
		params.AddedBy = sql.NullInt64{Int64: int64(*addedBy), Valid: true}
	}
	if err := db.writeQueries.AddCollectionTeam(ctx, params); err != nil {
		db.log.Error("failed to add collection team", "error", err, "collection_id", collectionID, "team_id", teamID)
		return fmt.Errorf("error adding collection team: %w", err)
	}
	return nil
}

// RemoveCollectionTeam removes a team share.
func (db *DB) RemoveCollectionTeam(ctx context.Context, collectionID int, teamID models.TeamID) error {
	if err := db.writeQueries.RemoveCollectionTeam(ctx, sqlc.RemoveCollectionTeamParams{
		CollectionID: int64(collectionID),
		TeamID:       int64(teamID),
	}); err != nil {
		return fmt.Errorf("error removing collection team: %w", err)
	}
	return nil
}

// ListCollectionTeams returns the teams a collection is shared with.
func (db *DB) ListCollectionTeams(ctx context.Context, collectionID int) ([]*models.CollectionTeam, error) {
	rows, err := db.readQueries.ListCollectionTeams(ctx, int64(collectionID))
	if err != nil {
		return nil, fmt.Errorf("error listing collection teams: %w", err)
	}
	out := make([]*models.CollectionTeam, 0, len(rows))
	for _, r := range rows {
		t := &models.CollectionTeam{
			CollectionID: int(r.CollectionID),
			TeamID:       models.TeamID(r.TeamID),
			TeamName:     r.TeamName,
			CreatedAt:    r.CreatedAt,
		}
		if r.AddedBy.Valid {
			uid := models.UserID(r.AddedBy.Int64)
			t.AddedBy = &uid
		}
		out = append(out, t)
	}
	return out, nil
}

// UserHasCollectionTeamAccess reports whether the user belongs to any team the
// collection is shared with.
func (db *DB) UserHasCollectionTeamAccess(ctx context.Context, collectionID int, userID models.UserID) (bool, error) {
	n, err := db.readQueries.CountCollectionTeamAccess(ctx, sqlc.CountCollectionTeamAccessParams{
		CollectionID: int64(collectionID),
		UserID:       int64(userID),
	})
	if err != nil {
		return false, fmt.Errorf("error checking collection team access: %w", err)
	}
	return n > 0, nil
}

// LockCollection checks that the collection exists. The write transaction
// that WithTx begins with BEGIN IMMEDIATE already excludes other writers, so
// no row lock is needed.
func (db *DB) LockCollection(ctx context.Context, collectionID int) error {
	if _, err := db.writeQueries.LockCollection(ctx, int64(collectionID)); err != nil {
		return fmt.Errorf("locking collection %d: %w", collectionID, translateNotFound(err))
	}
	return nil
}

// AddCollectionItem links a saved query to a collection and reports whether a
// row was inserted. An existing link of any kind is left as it is.
func (db *DB) AddCollectionItem(ctx context.Context, collectionID, savedQueryID, sortOrder int, kind models.CollectionLinkKind, addedBy *models.UserID) (bool, error) {
	params := sqlc.AddCollectionItemParams{
		CollectionID: int64(collectionID),
		SavedQueryID: int64(savedQueryID),
		SortOrder:    int64(sortOrder),
		Kind:         string(kind),
	}
	if addedBy != nil {
		params.AddedBy = sql.NullInt64{Int64: int64(*addedBy), Valid: true}
	}
	n, err := db.writeQueries.AddCollectionItem(ctx, params)
	if err != nil {
		return false, fmt.Errorf("error adding collection item: %w", err)
	}
	return n > 0, nil
}

// GetCollectionItemLink returns one link row, or models.ErrNotFound.
func (db *DB) GetCollectionItemLink(ctx context.Context, collectionID, savedQueryID int) (*models.CollectionItemLink, error) {
	row, err := db.readQueries.GetCollectionItemLink(ctx, sqlc.GetCollectionItemLinkParams{
		CollectionID: int64(collectionID),
		SavedQueryID: int64(savedQueryID),
	})
	if err != nil {
		return nil, fmt.Errorf("getting collection %d link %d: %w", collectionID, savedQueryID, translateNotFound(err))
	}
	link := &models.CollectionItemLink{
		CollectionID: int(row.CollectionID),
		SavedQueryID: int(row.SavedQueryID),
		Kind:         models.CollectionLinkKind(row.Kind),
		SortOrder:    int(row.SortOrder),
		CreatedAt:    row.CreatedAt,
	}
	if row.AddedBy.Valid {
		uid := models.UserID(row.AddedBy.Int64)
		link.AddedBy = &uid
	}
	return link, nil
}

// SetCollectionItemKind converts a link and reports whether a row changed.
func (db *DB) SetCollectionItemKind(ctx context.Context, collectionID, savedQueryID int, kind models.CollectionLinkKind) (bool, error) {
	n, err := db.writeQueries.SetCollectionItemKind(ctx, sqlc.SetCollectionItemKindParams{
		Kind:         string(kind),
		CollectionID: int64(collectionID),
		SavedQueryID: int64(savedQueryID),
	})
	if err != nil {
		return false, fmt.Errorf("error converting collection item: %w", err)
	}
	return n > 0, nil
}

// RemoveCollectionItem unlinks a saved query of either kind and reports
// whether a row was deleted.
func (db *DB) RemoveCollectionItem(ctx context.Context, collectionID, savedQueryID int) (bool, error) {
	n, err := db.writeQueries.RemoveCollectionItem(ctx, sqlc.RemoveCollectionItemParams{
		CollectionID: int64(collectionID),
		SavedQueryID: int64(savedQueryID),
	})
	if err != nil {
		return false, fmt.Errorf("error removing collection item: %w", err)
	}
	return n > 0, nil
}

// RemoveCollectionPin unlinks a saved query only when the link is a pin and
// reports whether a row was deleted.
func (db *DB) RemoveCollectionPin(ctx context.Context, collectionID, savedQueryID int) (bool, error) {
	n, err := db.writeQueries.RemoveCollectionPin(ctx, sqlc.RemoveCollectionPinParams{
		CollectionID: int64(collectionID),
		SavedQueryID: int64(savedQueryID),
	})
	if err != nil {
		return false, fmt.Errorf("error removing collection pin: %w", err)
	}
	return n > 0, nil
}

// ListSharedCollectionPins lists every pin in a shared collection, oldest
// first. Editors are left empty; core fills them per collection.
func (db *DB) ListSharedCollectionPins(ctx context.Context) ([]*models.CollectionLinkReportRow, error) {
	rows, err := db.readQueries.ListSharedCollectionPins(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing shared collection pins: %w", err)
	}
	out := make([]*models.CollectionLinkReportRow, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		row := &models.CollectionLinkReportRow{
			CollectionID:      int(r.CollectionID),
			CollectionName:    r.CollectionName,
			SavedQueryID:      int(r.SavedQueryID),
			SavedQueryName:    r.SavedQueryName,
			QueryCreatorEmail: r.QueryCreatorEmail.String,
			AddedByEmail:      r.AddedByEmail.String,
			CreatedAt:         r.CreatedAt,
			Editors:           []models.CollectionMember{},
		}
		if r.QueryCreatedBy.Valid {
			uid := models.UserID(r.QueryCreatedBy.Int64)
			row.QueryCreatedBy = &uid
		}
		if r.AddedBy.Valid {
			uid := models.UserID(r.AddedBy.Int64)
			row.AddedBy = &uid
		}
		out = append(out, row)
	}
	return out, nil
}

// ListCollectionItems returns items joined with their saved-query details.
// Runnable is left at false here — it must be set by the application layer
// based on the requesting user's source access.
func (db *DB) ListCollectionItems(ctx context.Context, collectionID int) ([]*models.CollectionItem, error) {
	rows, err := db.readQueries.ListCollectionItems(ctx, int64(collectionID))
	if err != nil {
		return nil, fmt.Errorf("error listing collection items: %w", err)
	}
	out := make([]*models.CollectionItem, 0, len(rows))
	for i := range rows {
		r := rows[i]
		query := models.SavedQuery{
			ID:            int(r.QueryID),
			SourceID:      models.SourceID(r.SourceID),
			Name:          r.QueryName,
			Description:   r.QueryDescription.String,
			QueryLanguage: models.QueryLanguage(r.QueryLanguage),
			EditorMode:    models.SavedQueryEditorMode(r.EditorMode),
			QueryContent:  r.QueryContent,
			CreatedAt:     r.QueryCreatedAt,
			UpdatedAt:     r.QueryUpdatedAt,
			SourceName:    r.SourceName,
		}
		if r.QueryCreatedBy.Valid {
			uid := models.UserID(r.QueryCreatedBy.Int64)
			query.CreatedBy = &uid
		}
		query.CreatedByName = r.QueryCreatedByName.String
		query.CreatedByEmail = r.QueryCreatedByEmail.String
		item := &models.CollectionItem{
			CollectionID: int(r.CollectionID),
			Kind:         models.CollectionLinkKind(r.Kind),
			SortOrder:    int(r.SortOrder),
			ItemAddedAt:  r.ItemAddedAt,
			Query:        query,
		}
		if r.AddedBy.Valid {
			uid := models.UserID(r.AddedBy.Int64)
			item.AddedBy = &uid
		}
		out = append(out, item)
	}
	return out, nil
}

func collectionToModel(row sqlc.Collection) *models.Collection {
	c := &models.Collection{
		ID:          int(row.ID),
		Name:        row.Name,
		Description: row.Description.String,
		IsPersonal:  row.IsPersonal == 1,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if row.CreatedBy.Valid {
		uid := models.UserID(row.CreatedBy.Int64)
		c.CreatedBy = &uid
	}
	return c
}

// UserCanEditSavedQueryViaSharedCollection reports whether the user is an owner
// or editor of any shared (non-personal) collection that manages the saved
// query. A pin grants nothing.
func (db *DB) UserCanEditSavedQueryViaSharedCollection(ctx context.Context, userID models.UserID, queryID int) (bool, error) {
	n, err := db.readQueries.CountSharedCollectionEditAccess(ctx, sqlc.CountSharedCollectionEditAccessParams{
		SavedQueryID: int64(queryID),
		UserID:       int64(userID),
	})
	if err != nil {
		return false, fmt.Errorf("error checking shared-collection edit access: %w", err)
	}
	return n > 0, nil
}
