package postgres

import (
	"context"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func collectionToModel(r sqlc.Collection) *models.Collection {
	return &models.Collection{
		ID:          int(r.ID),
		Name:        r.Name,
		Description: textStr(r.Description),
		IsPersonal:  r.IsPersonal,
		CreatedBy:   userIDPtr(r.CreatedBy),
		CreatedAt:   r.CreatedAt.Time,
		UpdatedAt:   r.UpdatedAt.Time,
	}
}

// CreateCollection inserts a new collection (the caller is added as owner in core).
func (s *Store) CreateCollection(ctx context.Context, name, description string, isPersonal bool, createdBy models.UserID) (*models.Collection, error) {
	row, err := s.q.CreateCollection(ctx, sqlc.CreateCollectionParams{
		Name:        name,
		Description: text(description),
		IsPersonal:  isPersonal,
		CreatedBy:   int8Val(int64(createdBy)),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: personal collection already exists for user %d", models.ErrConflict, createdBy)
		}
		s.log.Error("failed to create collection", "error", err, "name", name, "is_personal", isPersonal)
		return nil, fmt.Errorf("error creating collection: %w", err)
	}
	owner := createdBy
	return &models.Collection{
		ID:          int(row.ID),
		Name:        name,
		Description: description,
		IsPersonal:  isPersonal,
		CreatedBy:   &owner,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}, nil
}

// GetCollection returns a collection by id, or models.ErrNotFound if missing.
func (s *Store) GetCollection(ctx context.Context, collectionID int) (*models.Collection, error) {
	row, err := s.q.GetCollection(ctx, int64(collectionID))
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("getting collection id %d: %w", collectionID, err)
	}
	return collectionToModel(row), nil
}

// GetPersonalCollection returns the user's personal collection, or models.ErrNotFound
// if one has not been created yet.
func (s *Store) GetPersonalCollection(ctx context.Context, userID models.UserID) (*models.Collection, error) {
	row, err := s.q.GetPersonalCollection(ctx, int8Val(int64(userID)))
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("getting personal collection for user %d: %w", userID, err)
	}
	return collectionToModel(row), nil
}

// UpdateCollection updates name and description.
func (s *Store) UpdateCollection(ctx context.Context, collectionID int, name, description string) error {
	if err := s.q.UpdateCollection(ctx, sqlc.UpdateCollectionParams{
		Name:        name,
		Description: text(description),
		ID:          int64(collectionID),
	}); err != nil {
		s.log.Error("failed to update collection", "error", err, "collection_id", collectionID)
		return fmt.Errorf("error updating collection: %w", err)
	}
	return nil
}

// DeleteCollection removes a collection.
func (s *Store) DeleteCollection(ctx context.Context, collectionID int) error {
	if err := s.q.DeleteCollection(ctx, int64(collectionID)); err != nil {
		s.log.Error("failed to delete collection", "error", err, "collection_id", collectionID)
		return fmt.Errorf("error deleting collection: %w", err)
	}
	return nil
}

// ListCollectionsForUser returns every collection the user owns or is a member of.
func (s *Store) ListCollectionsForUser(ctx context.Context, userID models.UserID) ([]*models.Collection, error) {
	rows, err := s.q.ListCollectionsForUser(ctx, int64(userID))
	if err != nil {
		s.log.Error("failed to list collections for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing collections: %w", err)
	}
	out := make([]*models.Collection, 0, len(rows))
	for i := range rows {
		r := rows[i]
		out = append(out, &models.Collection{
			ID:          int(r.ID),
			Name:        r.Name,
			Description: textStr(r.Description),
			IsPersonal:  r.IsPersonal,
			CreatedBy:   userIDPtr(r.CreatedBy),
			CallerRole:  models.CollectionRole(r.CallerRole),
			MemberCount: int(r.MemberCount),
			ItemCount:   int(r.ItemCount),
			CreatedAt:   r.CreatedAt.Time,
			UpdatedAt:   r.UpdatedAt.Time,
		})
	}
	return out, nil
}

// AddCollectionMember adds a user to a collection (idempotent via ON CONFLICT).
func (s *Store) AddCollectionMember(ctx context.Context, collectionID int, userID models.UserID, role models.CollectionRole, addedBy *models.UserID) error {
	params := sqlc.AddCollectionMemberParams{
		CollectionID: int64(collectionID),
		UserID:       int64(userID),
		Role:         string(role),
	}
	if addedBy != nil {
		params.AddedBy = int8Val(int64(*addedBy))
	}
	if err := s.q.AddCollectionMember(ctx, params); err != nil {
		s.log.Error("failed to add collection member", "error", err, "collection_id", collectionID, "user_id", userID)
		return fmt.Errorf("error adding collection member: %w", err)
	}
	return nil
}

// GetCollectionMember returns a single membership row, or models.ErrNotFound if absent.
func (s *Store) GetCollectionMember(ctx context.Context, collectionID int, userID models.UserID) (*models.CollectionMember, error) {
	row, err := s.q.GetCollectionMember(ctx, sqlc.GetCollectionMemberParams{
		CollectionID: int64(collectionID),
		UserID:       int64(userID),
	})
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("getting collection member: %w", err)
	}
	return &models.CollectionMember{
		CollectionID: int(row.CollectionID),
		UserID:       models.UserID(row.UserID),
		Role:         models.CollectionRole(row.Role),
		AddedBy:      userIDPtr(row.AddedBy),
		CreatedAt:    row.CreatedAt.Time,
	}, nil
}

// ListCollectionMembers returns members of a collection with user details.
func (s *Store) ListCollectionMembers(ctx context.Context, collectionID int) ([]*models.CollectionMember, error) {
	rows, err := s.q.ListCollectionMembers(ctx, int64(collectionID))
	if err != nil {
		return nil, fmt.Errorf("error listing collection members: %w", err)
	}
	out := make([]*models.CollectionMember, 0, len(rows))
	for _, r := range rows {
		out = append(out, &models.CollectionMember{
			CollectionID: int(r.CollectionID),
			UserID:       models.UserID(r.UserID),
			Role:         models.CollectionRole(r.Role),
			AddedBy:      userIDPtr(r.AddedBy),
			CreatedAt:    r.CreatedAt.Time,
			Email:        r.Email,
			FullName:     r.FullName,
		})
	}
	return out, nil
}

// RemoveCollectionMember drops a member from a collection.
func (s *Store) RemoveCollectionMember(ctx context.Context, collectionID int, userID models.UserID) error {
	if err := s.q.RemoveCollectionMember(ctx, sqlc.RemoveCollectionMemberParams{
		CollectionID: int64(collectionID),
		UserID:       int64(userID),
	}); err != nil {
		return fmt.Errorf("error removing collection member: %w", err)
	}
	return nil
}

// AddCollectionItem links a saved query to a collection (idempotent via ON CONFLICT).
func (s *Store) AddCollectionItem(ctx context.Context, collectionID, savedQueryID, sortOrder int, addedBy *models.UserID) error {
	params := sqlc.AddCollectionItemParams{
		CollectionID: int64(collectionID),
		SavedQueryID: int64(savedQueryID),
		SortOrder:    int64(sortOrder),
	}
	if addedBy != nil {
		params.AddedBy = int8Val(int64(*addedBy))
	}
	if err := s.q.AddCollectionItem(ctx, params); err != nil {
		return fmt.Errorf("error adding collection item: %w", err)
	}
	return nil
}

// RemoveCollectionItem unlinks a saved query from a collection.
func (s *Store) RemoveCollectionItem(ctx context.Context, collectionID, savedQueryID int) error {
	if err := s.q.RemoveCollectionItem(ctx, sqlc.RemoveCollectionItemParams{
		CollectionID: int64(collectionID),
		SavedQueryID: int64(savedQueryID),
	}); err != nil {
		return fmt.Errorf("error removing collection item: %w", err)
	}
	return nil
}

// ListCollectionItems returns items joined with their saved-query details.
// Runnable is left false; the application layer sets it per requesting user.
func (s *Store) ListCollectionItems(ctx context.Context, collectionID int) ([]*models.CollectionItem, error) {
	rows, err := s.q.ListCollectionItems(ctx, int64(collectionID))
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
			Description:   textStr(r.QueryDescription),
			QueryLanguage: models.QueryLanguage(r.QueryLanguage),
			EditorMode:    models.SavedQueryEditorMode(r.EditorMode),
			QueryContent:  r.QueryContent,
			CreatedBy:     userIDPtr(r.QueryCreatedBy),
			CreatedAt:     r.QueryCreatedAt.Time,
			UpdatedAt:     r.QueryUpdatedAt.Time,
			SourceName:    r.SourceName,
		}
		out = append(out, &models.CollectionItem{
			CollectionID: int(r.CollectionID),
			SortOrder:    int(r.SortOrder),
			AddedBy:      userIDPtr(r.AddedBy),
			ItemAddedAt:  r.ItemAddedAt.Time,
			Query:        query,
		})
	}
	return out, nil
}

// UserCanEditSavedQueryViaSharedCollection reports whether the user is an owner
// or editor of any shared collection containing the saved query.
func (s *Store) UserCanEditSavedQueryViaSharedCollection(ctx context.Context, userID models.UserID, queryID int) (bool, error) {
	n, err := s.q.CountSharedCollectionEditAccess(ctx, sqlc.CountSharedCollectionEditAccessParams{
		SavedQueryID: int64(queryID),
		UserID:       int64(userID),
	})
	if err != nil {
		return false, fmt.Errorf("error checking shared-collection edit access: %w", err)
	}
	return n > 0, nil
}
