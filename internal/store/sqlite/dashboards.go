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

// mapDashboardRow converts a generated sqlc.Dashboard into the domain model.
func mapDashboardRow(row sqlc.Dashboard) *models.Dashboard {
	d := &models.Dashboard{
		ID:          int(row.ID),
		Name:        row.Name,
		Description: row.Description.String,
		PanelsJSON:  json.RawMessage(row.PanelsJson),
		Timestamps: models.Timestamps{
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
	}
	if row.CreatedBy.Valid {
		uid := models.UserID(row.CreatedBy.Int64)
		d.CreatedBy = &uid
	}
	return d
}

// CreateDashboard inserts a new dashboard and repopulates the model with the
// persisted row (id and timestamps).
func (db *DB) CreateDashboard(ctx context.Context, dashboard *models.Dashboard) error {
	if dashboard == nil {
		return fmt.Errorf("dashboard payload is required")
	}
	params := sqlc.CreateDashboardParams{
		Name:        dashboard.Name,
		Description: nullString(dashboard.Description),
		PanelsJson:  string(dashboard.PanelsJSON),
	}
	if dashboard.CreatedBy != nil {
		params.CreatedBy = sql.NullInt64{Int64: int64(*dashboard.CreatedBy), Valid: true}
	}

	id, err := db.writeQueries.CreateDashboard(ctx, params)
	if err != nil {
		db.log.Error("failed to create dashboard", "error", err)
		return fmt.Errorf("error creating dashboard: %w", err)
	}

	created, err := db.GetDashboard(ctx, int(id))
	if err != nil {
		return err
	}
	*dashboard = *created
	return nil
}

// GetDashboard returns a dashboard by id, or models.ErrNotFound if missing.
func (db *DB) GetDashboard(ctx context.Context, id int) (*models.Dashboard, error) {
	row, err := db.readQueries.GetDashboard(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("getting dashboard id %d: %w", id, err)
	}
	d := &models.Dashboard{
		ID:          int(row.ID),
		Name:        row.Name,
		Description: row.Description.String,
		PanelsJSON:  json.RawMessage(row.PanelsJson),
		Timestamps: models.Timestamps{
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		CreatedByEmail: row.CreatedByEmail.String,
		CreatedByName:  row.CreatedByName.String,
	}
	if row.CreatedBy.Valid {
		uid := models.UserID(row.CreatedBy.Int64)
		d.CreatedBy = &uid
	}
	return d, nil
}

// ListDashboards returns every dashboard, newest-updated first, with creator info.
func (db *DB) ListDashboards(ctx context.Context) ([]*models.Dashboard, error) {
	rows, err := db.readQueries.ListDashboards(ctx)
	if err != nil {
		db.log.Error("failed to list dashboards", "error", err)
		return nil, fmt.Errorf("error listing dashboards: %w", err)
	}

	dashboards := make([]*models.Dashboard, 0, len(rows))
	for i := range rows {
		r := rows[i]
		d := &models.Dashboard{
			ID:          int(r.ID),
			Name:        r.Name,
			Description: r.Description.String,
			PanelsJSON:  json.RawMessage(r.PanelsJson),
			Timestamps: models.Timestamps{
				CreatedAt: r.CreatedAt,
				UpdatedAt: r.UpdatedAt,
			},
			CreatedByEmail: r.CreatedByEmail.String,
			CreatedByName:  r.CreatedByName.String,
		}
		if r.CreatedBy.Valid {
			uid := models.UserID(r.CreatedBy.Int64)
			d.CreatedBy = &uid
		}
		dashboards = append(dashboards, d)
	}
	return dashboards, nil
}

// UpdateDashboard overwrites a dashboard's mutable fields. Returns
// models.ErrNotFound when the id does not exist.
func (db *DB) UpdateDashboard(ctx context.Context, dashboard *models.Dashboard) error {
	if dashboard == nil {
		return fmt.Errorf("dashboard payload is required")
	}
	_, err := db.writeQueries.UpdateDashboard(ctx, sqlc.UpdateDashboardParams{
		Name:        dashboard.Name,
		Description: nullString(dashboard.Description),
		PanelsJson:  string(dashboard.PanelsJSON),
		ID:          int64(dashboard.ID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		db.log.Error("failed to update dashboard", "error", err, "dashboard_id", dashboard.ID)
		return fmt.Errorf("error updating dashboard: %w", err)
	}
	return nil
}

// DeleteDashboard removes a dashboard. Returns models.ErrNotFound when the id
// does not exist.
func (db *DB) DeleteDashboard(ctx context.Context, id int) error {
	if _, err := db.writeQueries.DeleteDashboard(ctx, int64(id)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		db.log.Error("failed to delete dashboard", "error", err, "dashboard_id", id)
		return fmt.Errorf("error deleting dashboard: %w", err)
	}
	return nil
}
