package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateSavedView inserts view with its IsDefault and Managed flags as given
// and fills it from the stored row.
func (db *DB) CreateSavedView(ctx context.Context, view *models.SavedView) error {
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
		IsDefault:   boolToInt(view.IsDefault),
		Managed:     boolToInt(view.Managed),
	}
	if view.UserID != nil {
		params.UserID = sql.NullInt64{Int64: int64(*view.UserID), Valid: true}
	}
	if view.TeamID != nil {
		params.TeamID = sql.NullInt64{Int64: int64(*view.TeamID), Valid: true}
	}
	if view.CreatedBy != nil {
		params.CreatedBy = sql.NullInt64{Int64: int64(*view.CreatedBy), Valid: true}
	}
	row, err := db.writeQueries.CreateSavedView(ctx, params)
	if err != nil {
		if isUniqueConstraintSQLiteError(err, "", "") {
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
func (db *DB) GetSavedView(ctx context.Context, id models.SavedViewID) (*models.SavedView, error) {
	row, err := db.readQueries.GetSavedView(ctx, int64(id))
	if err != nil {
		return nil, fmt.Errorf("getting saved view %d: %w", id, translateNotFound(err))
	}
	return savedViewFromRow(row)
}

// ListSavedViews returns userID's personal views, teamID's team views and the
// source views of sourceID in one query.
func (db *DB) ListSavedViews(ctx context.Context, sourceID models.SourceID, userID models.UserID, teamID models.TeamID) ([]*models.SavedView, error) {
	rows, err := db.readQueries.ListSavedViews(ctx, sqlc.ListSavedViewsParams{
		SourceID: int64(sourceID),
		UserID:   sql.NullInt64{Int64: int64(userID), Valid: true},
		TeamID:   sql.NullInt64{Int64: int64(teamID), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("listing saved views: %w", err)
	}
	return savedViewsFromRows(rows)
}

// UpdateSavedView writes name, description and definition when the stored
// revision still equals view.Revision, and fills view from the stored row.
func (db *DB) UpdateSavedView(ctx context.Context, view *models.SavedView) error {
	definition, err := json.Marshal(view.Definition)
	if err != nil {
		return fmt.Errorf("encoding saved view definition: %w", err)
	}
	row, err := db.writeQueries.UpdateSavedView(ctx, sqlc.UpdateSavedViewParams{
		Name:        view.Name,
		Description: view.Description,
		Definition:  string(definition),
		ID:          int64(view.ID),
		Revision:    int64(view.Revision),
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, getErr := db.GetSavedView(ctx, view.ID); getErr != nil {
			return fmt.Errorf("updating saved view: %w", getErr)
		}
		return fmt.Errorf("updating saved view %d at revision %d: %w", view.ID, view.Revision, models.ErrConflict)
	case isUniqueConstraintSQLiteError(err, "", ""):
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
func (db *DB) DeleteSavedView(ctx context.Context, id models.SavedViewID) error {
	if _, err := db.writeQueries.DeleteSavedView(ctx, int64(id)); err != nil {
		return fmt.Errorf("deleting saved view %d: %w", id, translateNotFound(err))
	}
	return nil
}

// SetSavedViewDefault moves the default flag of the view's scope to the view
// in one transaction and returns the view that lost it.
func (db *DB) SetSavedViewDefault(ctx context.Context, id models.SavedViewID) (*models.ClearedSavedViewDefault, error) {
	var cleared *models.ClearedSavedViewDefault
	err := db.inWriteTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.GetSavedView(ctx, int64(id))
		if err != nil {
			return fmt.Errorf("getting saved view %d: %w", id, translateNotFound(err))
		}
		rows, err := q.ClearSavedViewScopeDefault(ctx, sqlc.ClearSavedViewScopeDefaultParams{
			SourceID: row.SourceID,
			Scope:    row.Scope,
			UserID:   sql.NullInt64{Int64: row.UserID.Int64, Valid: true},
			TeamID:   sql.NullInt64{Int64: row.TeamID.Int64, Valid: true},
			KeepID:   row.ID,
		})
		if err != nil {
			return fmt.Errorf("clearing saved view default: %w", err)
		}
		if len(rows) > 0 {
			cleared = &models.ClearedSavedViewDefault{ID: models.SavedViewID(rows[0].ID), Managed: rows[0].Managed != 0}
		}
		if _, err := q.SetSavedViewDefaultFlag(ctx, sqlc.SetSavedViewDefaultFlagParams{IsDefault: 1, ID: row.ID}); err != nil {
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
func (db *DB) ClearSavedViewDefault(ctx context.Context, id models.SavedViewID) error {
	if _, err := db.writeQueries.SetSavedViewDefaultFlag(ctx, sqlc.SetSavedViewDefaultFlagParams{IsDefault: 0, ID: int64(id)}); err != nil {
		return fmt.Errorf("clearing saved view %d default: %w", id, translateNotFound(err))
	}
	return nil
}

// CountSavedViewsInScope counts the views of one scope of one source.
func (db *DB) CountSavedViewsInScope(ctx context.Context, key models.SavedViewScopeKey) (int, error) {
	params := sqlc.CountSavedViewsInScopeParams{
		SourceID: int64(key.SourceID),
		Scope:    string(key.Scope),
		UserID:   sql.NullInt64{Valid: true},
		TeamID:   sql.NullInt64{Valid: true},
	}
	if key.UserID != nil {
		params.UserID.Int64 = int64(*key.UserID)
	}
	if key.TeamID != nil {
		params.TeamID.Int64 = int64(*key.TeamID)
	}
	n, err := db.readQueries.CountSavedViewsInScope(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("counting saved views: %w", err)
	}
	return int(n), nil
}

// MarkSavedViewManaged hands a view to provisioning, or returns
// models.ErrNotFound.
func (db *DB) MarkSavedViewManaged(ctx context.Context, id models.SavedViewID) error {
	if _, err := db.writeQueries.MarkSavedViewManaged(ctx, int64(id)); err != nil {
		return fmt.Errorf("marking saved view %d managed: %w", id, translateNotFound(err))
	}
	return nil
}

// ListManagedSavedViews returns every managed view.
func (db *DB) ListManagedSavedViews(ctx context.Context) ([]*models.SavedView, error) {
	rows, err := db.readQueries.ListManagedSavedViews(ctx)
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
		Name:        row.Name,
		Description: row.Description,
		IsDefault:   row.IsDefault != 0,
		Managed:     row.Managed != 0,
		Revision:    int(row.Revision),
		Timestamps:  models.Timestamps{CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt},
	}
	if err := json.Unmarshal([]byte(row.Definition), &v.Definition); err != nil {
		return nil, fmt.Errorf("decoding saved view %d definition: %w", row.ID, err)
	}
	if row.UserID.Valid {
		uid := models.UserID(row.UserID.Int64)
		v.UserID = &uid
	}
	if row.TeamID.Valid {
		tid := models.TeamID(row.TeamID.Int64)
		v.TeamID = &tid
	}
	if row.CreatedBy.Valid {
		uid := models.UserID(row.CreatedBy.Int64)
		v.CreatedBy = &uid
	}
	return v, nil
}
