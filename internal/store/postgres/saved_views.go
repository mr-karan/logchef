package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateSavedView inserts view with its IsDefault and Managed flags as given
// and fills it from the stored row.
func (s *Store) CreateSavedView(ctx context.Context, view *models.SavedView) error {
	definition, err := json.Marshal(view.Definition)
	if err != nil {
		return fmt.Errorf("encoding saved view definition: %w", err)
	}
	params := sqlc.CreateSavedViewParams{
		SourceID:    int64(view.SourceID),
		Scope:       string(view.Scope),
		Name:        view.Name,
		Description: view.Description,
		Definition:  string(definition),
		IsDefault:   view.IsDefault,
		Managed:     view.Managed,
	}
	if view.UserID != nil {
		params.UserID = int8Val(int64(*view.UserID))
	}
	if view.TeamID != nil {
		params.TeamID = int8Val(int64(*view.TeamID))
	}
	if view.CreatedBy != nil {
		params.CreatedBy = int8Val(int64(*view.CreatedBy))
	}
	row, err := s.q.CreateSavedView(ctx, params)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("creating saved view %q: %w", view.Name, models.ErrConflict)
		}
		return fmt.Errorf("creating saved view: %w", err)
	}
	created, err := savedViewFromRow(row)
	if err != nil {
		return err
	}
	*view = *created
	return nil
}

// GetSavedView returns a view by id, or models.ErrNotFound.
func (s *Store) GetSavedView(ctx context.Context, id models.SavedViewID) (*models.SavedView, error) {
	row, err := s.q.GetSavedView(ctx, int64(id))
	if err != nil {
		if notFound(err) {
			return nil, fmt.Errorf("getting saved view %d: %w", id, models.ErrNotFound)
		}
		return nil, fmt.Errorf("getting saved view %d: %w", id, err)
	}
	return savedViewFromRow(row)
}

// ListSavedViews returns userID's personal views, teamID's team views and the
// source views of sourceID in one query.
func (s *Store) ListSavedViews(ctx context.Context, sourceID models.SourceID, userID models.UserID, teamID models.TeamID) ([]*models.SavedView, error) {
	rows, err := s.q.ListSavedViews(ctx, sqlc.ListSavedViewsParams{
		SourceID: int64(sourceID),
		UserID:   int8Val(int64(userID)),
		TeamID:   int8Val(int64(teamID)),
	})
	if err != nil {
		return nil, fmt.Errorf("listing saved views: %w", err)
	}
	return savedViewsFromRows(rows)
}

// UpdateSavedView writes name, description and definition when the stored
// revision still equals view.Revision, and fills view from the stored row.
func (s *Store) UpdateSavedView(ctx context.Context, view *models.SavedView) error {
	definition, err := json.Marshal(view.Definition)
	if err != nil {
		return fmt.Errorf("encoding saved view definition: %w", err)
	}
	row, err := s.q.UpdateSavedView(ctx, sqlc.UpdateSavedViewParams{
		Name:        view.Name,
		Description: view.Description,
		Definition:  string(definition),
		ID:          int64(view.ID),
		Revision:    int64(view.Revision),
	})
	switch {
	case notFound(err):
		if _, getErr := s.GetSavedView(ctx, view.ID); getErr != nil {
			return fmt.Errorf("updating saved view: %w", getErr)
		}
		return fmt.Errorf("updating saved view %d at revision %d: %w", view.ID, view.Revision, models.ErrConflict)
	case isUniqueViolation(err):
		return fmt.Errorf("updating saved view %d to name %q: %w", view.ID, view.Name, models.ErrConflict)
	case err != nil:
		return fmt.Errorf("updating saved view: %w", err)
	}
	updated, err := savedViewFromRow(row)
	if err != nil {
		return err
	}
	*view = *updated
	return nil
}

// DeleteSavedView removes a view, or returns models.ErrNotFound.
func (s *Store) DeleteSavedView(ctx context.Context, id models.SavedViewID) error {
	if _, err := s.q.DeleteSavedView(ctx, int64(id)); err != nil {
		if notFound(err) {
			return fmt.Errorf("deleting saved view %d: %w", id, models.ErrNotFound)
		}
		return fmt.Errorf("deleting saved view %d: %w", id, err)
	}
	return nil
}

// SetSavedViewDefault moves the default flag of the view's scope to the view
// in one transaction and returns the view that lost it. The clear locks the
// old default row, so a concurrent swap in the same scope waits for this one
// and then fails the partial unique index with models.ErrConflict.
func (s *Store) SetSavedViewDefault(ctx context.Context, id models.SavedViewID) (*models.ClearedSavedViewDefault, error) {
	var cleared *models.ClearedSavedViewDefault
	err := s.inTx(ctx, func(q sqlc.Querier) error {
		row, err := q.GetSavedView(ctx, int64(id))
		if err != nil {
			if notFound(err) {
				return fmt.Errorf("getting saved view %d: %w", id, models.ErrNotFound)
			}
			return fmt.Errorf("getting saved view %d: %w", id, err)
		}
		rows, err := q.ClearSavedViewScopeDefault(ctx, sqlc.ClearSavedViewScopeDefaultParams{
			SourceID: row.SourceID,
			Scope:    row.Scope,
			UserID:   row.UserID.Int64,
			TeamID:   row.TeamID.Int64,
			KeepID:   row.ID,
		})
		if err != nil {
			return fmt.Errorf("clearing saved view default: %w", err)
		}
		if len(rows) > 0 {
			cleared = &models.ClearedSavedViewDefault{ID: models.SavedViewID(rows[0].ID), Managed: rows[0].Managed}
		}
		if _, err := q.SetSavedViewDefaultFlag(ctx, sqlc.SetSavedViewDefaultFlagParams{IsDefault: true, ID: row.ID}); err != nil {
			if notFound(err) {
				return fmt.Errorf("setting saved view %d as default: %w", id, models.ErrNotFound)
			}
			if isUniqueViolation(err) {
				return fmt.Errorf("setting saved view %d as default: %w", id, models.ErrConflict)
			}
			return fmt.Errorf("setting saved view %d as default: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cleared, nil
}

// ClearSavedViewDefault clears the view's default flag, or returns
// models.ErrNotFound.
func (s *Store) ClearSavedViewDefault(ctx context.Context, id models.SavedViewID) error {
	if _, err := s.q.SetSavedViewDefaultFlag(ctx, sqlc.SetSavedViewDefaultFlagParams{IsDefault: false, ID: int64(id)}); err != nil {
		if notFound(err) {
			return fmt.Errorf("clearing saved view %d default: %w", id, models.ErrNotFound)
		}
		return fmt.Errorf("clearing saved view %d default: %w", id, err)
	}
	return nil
}

// CountSavedViewsInScope counts the views of one scope of one source.
func (s *Store) CountSavedViewsInScope(ctx context.Context, key models.SavedViewScopeKey) (int, error) {
	params := sqlc.CountSavedViewsInScopeParams{SourceID: int64(key.SourceID), Scope: string(key.Scope)}
	if key.UserID != nil {
		params.UserID = int64(*key.UserID)
	}
	if key.TeamID != nil {
		params.TeamID = int64(*key.TeamID)
	}
	n, err := s.q.CountSavedViewsInScope(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("counting saved views: %w", err)
	}
	return int(n), nil
}

// MarkSavedViewManaged hands a view to provisioning, or returns
// models.ErrNotFound.
func (s *Store) MarkSavedViewManaged(ctx context.Context, id models.SavedViewID) error {
	if _, err := s.q.MarkSavedViewManaged(ctx, int64(id)); err != nil {
		if notFound(err) {
			return fmt.Errorf("marking saved view %d managed: %w", id, models.ErrNotFound)
		}
		return fmt.Errorf("marking saved view %d managed: %w", id, err)
	}
	return nil
}

// ListManagedSavedViews returns every managed view.
func (s *Store) ListManagedSavedViews(ctx context.Context) ([]*models.SavedView, error) {
	rows, err := s.q.ListManagedSavedViews(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing managed saved views: %w", err)
	}
	return savedViewsFromRows(rows)
}

func savedViewsFromRows(rows []sqlc.SavedView) ([]*models.SavedView, error) {
	views := make([]*models.SavedView, 0, len(rows))
	for i := range rows {
		v, err := savedViewFromRow(rows[i])
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, nil
}

func savedViewFromRow(row sqlc.SavedView) (*models.SavedView, error) {
	v := &models.SavedView{
		ID:          models.SavedViewID(row.ID),
		SourceID:    models.SourceID(row.SourceID),
		Scope:       models.SavedViewScope(row.Scope),
		UserID:      userIDPtr(row.UserID),
		TeamID:      teamIDPtr(row.TeamID),
		Name:        row.Name,
		Description: row.Description,
		IsDefault:   row.IsDefault,
		Managed:     row.Managed,
		Revision:    int(row.Revision),
		CreatedBy:   userIDPtr(row.CreatedBy),
		Timestamps:  models.Timestamps{CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time},
	}
	if err := json.Unmarshal([]byte(row.Definition), &v.Definition); err != nil {
		return nil, fmt.Errorf("decoding saved view %d definition: %w", row.ID, err)
	}
	return v, nil
}
