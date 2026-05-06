package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateCollection inserts a new collection. The caller is automatically
// added as the owner via AddCollectionMember in core.
func (db *DB) CreateCollection(ctx context.Context, name, description string, isPersonal bool, createdBy models.UserID) (*models.Collection, error) {
	row, err := db.writeQueries.CreateCollection(ctx, sqlc.CreateCollectionParams{
		Name:        name,
		Description: nullString(description),
		IsPersonal:  boolToInt(isPersonal),
		CreatedBy:   int64(createdBy),
	})
	if err != nil {
		db.log.Error("failed to create collection", "error", err, "name", name, "is_personal", isPersonal)
		return nil, fmt.Errorf("error creating collection: %w", err)
	}

	return &models.Collection{
		ID:          int(row.ID),
		Name:        name,
		Description: description,
		IsPersonal:  isPersonal,
		CreatedBy:   createdBy,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

// GetCollection returns a collection by id, or ErrNotFound if missing.
func (db *DB) GetCollection(ctx context.Context, collectionID int) (*models.Collection, error) {
	row, err := db.readQueries.GetCollection(ctx, int64(collectionID))
	if err != nil {
		return nil, handleNotFoundError(err, fmt.Sprintf("getting collection id %d", collectionID))
	}
	return mapCollectionRow(row), nil
}

// GetPersonalCollection returns the user's personal collection, or sql.ErrNoRows
// if one has not been created yet.
func (db *DB) GetPersonalCollection(ctx context.Context, userID models.UserID) (*models.Collection, error) {
	row, err := db.readQueries.GetPersonalCollection(ctx, int64(userID))
	if err != nil {
		return nil, err
	}
	return mapCollectionRow(row), nil
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

// ListCollectionsForUser returns every collection the user owns or is a member of.
func (db *DB) ListCollectionsForUser(ctx context.Context, userID models.UserID) ([]*models.Collection, error) {
	rows, err := db.readQueries.ListCollectionsForUser(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list collections for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing collections: %w", err)
	}
	out := make([]*models.Collection, 0, len(rows))
	for _, r := range rows {
		out = append(out, &models.Collection{
			ID:          int(r.ID),
			Name:        r.Name,
			Description: r.Description.String,
			IsPersonal:  r.IsPersonal == 1,
			CreatedBy:   models.UserID(r.CreatedBy),
			CallerRole:  models.CollectionRole(r.CallerRole),
			MemberCount: int(r.MemberCount),
			ItemCount:   int(r.ItemCount),
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
		})
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
		if isUniqueConstraintSQLiteError(err, "", "") {
			// User is already a member — treat as a no-op for idempotency.
			return nil
		}
		db.log.Error("failed to add collection member", "error", err, "collection_id", collectionID, "user_id", userID)
		return fmt.Errorf("error adding collection member: %w", err)
	}
	return nil
}

// GetCollectionMember returns a single membership row, or sql.ErrNoRows if absent.
func (db *DB) GetCollectionMember(ctx context.Context, collectionID int, userID models.UserID) (*models.CollectionMember, error) {
	row, err := db.readQueries.GetCollectionMember(ctx, sqlc.GetCollectionMemberParams{
		CollectionID: int64(collectionID),
		UserID:       int64(userID),
	})
	if err != nil {
		return nil, err
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

// AddCollectionItem links a saved query to a collection (idempotent).
func (db *DB) AddCollectionItem(ctx context.Context, collectionID, savedQueryID, sortOrder int, addedBy *models.UserID) error {
	params := sqlc.AddCollectionItemParams{
		CollectionID: int64(collectionID),
		SavedQueryID: int64(savedQueryID),
		SortOrder:    int64(sortOrder),
	}
	if addedBy != nil {
		params.AddedBy = sql.NullInt64{Int64: int64(*addedBy), Valid: true}
	}
	if err := db.writeQueries.AddCollectionItem(ctx, params); err != nil {
		if isUniqueConstraintSQLiteError(err, "", "") {
			// Saved query is already in the collection — treat as a no-op.
			return nil
		}
		return fmt.Errorf("error adding collection item: %w", err)
	}
	return nil
}

// RemoveCollectionItem unlinks a saved query from a collection.
func (db *DB) RemoveCollectionItem(ctx context.Context, collectionID, savedQueryID int) error {
	if err := db.writeQueries.RemoveCollectionItem(ctx, sqlc.RemoveCollectionItemParams{
		CollectionID: int64(collectionID),
		SavedQueryID: int64(savedQueryID),
	}); err != nil {
		return fmt.Errorf("error removing collection item: %w", err)
	}
	return nil
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
	for _, r := range rows {
		query := models.SavedQuery{
			ID:           int(r.QueryID),
			SourceID:     models.SourceID(r.SourceID),
			Name:         r.QueryName,
			Description:  r.QueryDescription.String,
			QueryType:    models.SavedQueryType(r.QueryType),
			QueryContent: r.QueryContent,
			CreatedAt:    r.QueryCreatedAt,
			UpdatedAt:    r.QueryUpdatedAt,
			SourceName:   r.SourceName,
		}
		if r.QueryCreatedBy.Valid {
			uid := models.UserID(r.QueryCreatedBy.Int64)
			query.CreatedBy = &uid
		}
		item := &models.CollectionItem{
			CollectionID: int(r.CollectionID),
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

func mapCollectionRow(row sqlc.Collection) *models.Collection {
	return &models.Collection{
		ID:          int(row.ID),
		Name:        row.Name,
		Description: row.Description.String,
		IsPersonal:  row.IsPersonal == 1,
		CreatedBy:   models.UserID(row.CreatedBy),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}
