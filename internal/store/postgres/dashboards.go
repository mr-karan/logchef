package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateDashboard inserts a new dashboard and repopulates the model with the
// persisted row (id and timestamps).
func (s *Store) CreateDashboard(ctx context.Context, dashboard *models.Dashboard) error {
	if dashboard == nil {
		return fmt.Errorf("dashboard payload is required")
	}
	params := sqlc.CreateDashboardParams{
		Name:        dashboard.Name,
		Description: text(dashboard.Description),
		PanelsJson:  string(dashboard.PanelsJSON),
	}
	if dashboard.CreatedBy != nil {
		params.CreatedBy = int8Val(int64(*dashboard.CreatedBy))
	}

	id, err := s.q.CreateDashboard(ctx, params)
	if err != nil {
		s.log.Error("failed to create dashboard", "error", err)
		return fmt.Errorf("error creating dashboard: %w", err)
	}

	created, err := s.GetDashboard(ctx, int(id))
	if err != nil {
		return err
	}
	*dashboard = *created
	return nil
}

// GetDashboard returns a dashboard by id, or models.ErrNotFound if missing.
func (s *Store) GetDashboard(ctx context.Context, id int) (*models.Dashboard, error) {
	row, err := s.q.GetDashboard(ctx, int64(id))
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("getting dashboard id %d: %w", id, err)
	}
	return &models.Dashboard{
		ID:          int(row.ID),
		Name:        row.Name,
		Description: textStr(row.Description),
		PanelsJSON:  json.RawMessage(row.PanelsJson),
		CreatedBy:   userIDPtr(row.CreatedBy),
		Timestamps: models.Timestamps{
			CreatedAt: row.CreatedAt.Time,
			UpdatedAt: row.UpdatedAt.Time,
		},
		CreatedByEmail: textStr(row.CreatedByEmail),
		CreatedByName:  textStr(row.CreatedByName),
	}, nil
}

// ListDashboards returns every dashboard, newest-updated first, with creator info.
func (s *Store) ListDashboards(ctx context.Context) ([]*models.Dashboard, error) {
	rows, err := s.q.ListDashboards(ctx)
	if err != nil {
		s.log.Error("failed to list dashboards", "error", err)
		return nil, fmt.Errorf("error listing dashboards: %w", err)
	}

	dashboards := make([]*models.Dashboard, 0, len(rows))
	for i := range rows {
		r := rows[i]
		dashboards = append(dashboards, &models.Dashboard{
			ID:          int(r.ID),
			Name:        r.Name,
			Description: textStr(r.Description),
			PanelsJSON:  json.RawMessage(r.PanelsJson),
			CreatedBy:   userIDPtr(r.CreatedBy),
			Timestamps: models.Timestamps{
				CreatedAt: r.CreatedAt.Time,
				UpdatedAt: r.UpdatedAt.Time,
			},
			CreatedByEmail: textStr(r.CreatedByEmail),
			CreatedByName:  textStr(r.CreatedByName),
		})
	}
	return dashboards, nil
}

// UpdateDashboard overwrites a dashboard's mutable fields. Returns
// models.ErrNotFound when the id does not exist.
func (s *Store) UpdateDashboard(ctx context.Context, dashboard *models.Dashboard) error {
	if dashboard == nil {
		return fmt.Errorf("dashboard payload is required")
	}
	_, err := s.q.UpdateDashboard(ctx, sqlc.UpdateDashboardParams{
		Name:        dashboard.Name,
		Description: text(dashboard.Description),
		PanelsJson:  string(dashboard.PanelsJSON),
		ID:          int64(dashboard.ID),
	})
	if err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		s.log.Error("failed to update dashboard", "error", err, "dashboard_id", dashboard.ID)
		return fmt.Errorf("error updating dashboard: %w", err)
	}
	return nil
}

// DeleteDashboard removes a dashboard. Returns models.ErrNotFound when the id
// does not exist.
func (s *Store) DeleteDashboard(ctx context.Context, id int) error {
	if _, err := s.q.DeleteDashboard(ctx, int64(id)); err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		s.log.Error("failed to delete dashboard", "error", err, "dashboard_id", id)
		return fmt.Errorf("error deleting dashboard: %w", err)
	}
	return nil
}
