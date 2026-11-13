package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateDashboard inserts a new dashboard and the derived list columns of its
// blob, then repopulates the model with the persisted row (id and timestamps).
func (s *Store) CreateDashboard(ctx context.Context, dashboard *models.Dashboard) error {
	if dashboard == nil {
		return fmt.Errorf("dashboard payload is required")
	}
	summary := models.SummarizeDashboardPanels(dashboard.PanelsJSON)
	params := sqlc.CreateDashboardParams{
		Name:           dashboard.Name,
		Description:    text(dashboard.Description),
		PanelsJson:     string(dashboard.PanelsJSON),
		PanelCount:     int32(summary.PanelCount), //nolint:gosec // G115: at most 24 panels
		TabCount:       int32(summary.TabCount),   //nolint:gosec // G115: at most 10 tabs
		PanelTypes:     summary.PanelTypesJSON(),
		PanelTeamIds:   summary.PanelTeamIDsJSON(),
		PanelsCorrupt:  summary.Corrupt,
		SummaryVersion: models.DashboardSummaryVersion,
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
		Revision:    int(row.Revision),
		Timestamps: models.Timestamps{
			CreatedAt: row.CreatedAt.Time,
			UpdatedAt: row.UpdatedAt.Time,
		},
		CreatedByEmail: textStr(row.CreatedByEmail),
		CreatedByName:  textStr(row.CreatedByName),
	}, nil
}

// ListDashboardSummaries returns every dashboard's summary from the derived
// columns, newest-updated first, with creator info.
func (s *Store) ListDashboardSummaries(ctx context.Context) ([]*models.DashboardSummary, error) {
	rows, err := s.q.ListDashboardSummaries(ctx)
	if err != nil {
		s.log.Error("failed to list dashboards", "error", err)
		return nil, fmt.Errorf("error listing dashboards: %w", err)
	}

	summaries := make([]*models.DashboardSummary, 0, len(rows))
	for i := range rows {
		r := rows[i]
		types, teams, err := models.ParseDashboardSummaryColumns(r.PanelTypes, r.PanelTeamIds)
		if err != nil {
			return nil, fmt.Errorf("error listing dashboards: row %d: %w", r.ID, err)
		}
		summaries = append(summaries, &models.DashboardSummary{
			ID:              int(r.ID),
			Name:            r.Name,
			Description:     r.Description,
			PanelCount:      int(r.PanelCount),
			PanelTypes:      types,
			TabCount:        int(r.TabCount),
			CreatedByEmail:  textStr(r.CreatedByEmail),
			CreatedByName:   textStr(r.CreatedByName),
			Revision:        int(r.Revision),
			Corrupt:         r.PanelsCorrupt,
			Timestamps:      models.Timestamps{CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time},
			CreatedBy:       userIDPtr(r.CreatedBy),
			PanelTeamIDs:    teams,
			SummaryRevision: int(r.SummaryRevision),
			SummaryVersion:  int(r.SummaryVersion),
		})
	}
	return summaries, nil
}

// ListStaleDashboardSummaryInputs returns stale rows above afterID in id order.
func (s *Store) ListStaleDashboardSummaryInputs(ctx context.Context, afterID, limit int) ([]models.DashboardSummaryInput, error) {
	rows, err := s.q.ListStaleDashboardSummaryInputs(ctx, sqlc.ListStaleDashboardSummaryInputsParams{
		ID:             int64(afterID),
		SummaryVersion: models.DashboardSummaryVersion,
		Limit:          int32(limit), //nolint:gosec // G115: a page size of 50
	})
	if err != nil {
		return nil, fmt.Errorf("error listing stale dashboard summaries: %w", err)
	}
	inputs := make([]models.DashboardSummaryInput, 0, len(rows))
	for _, r := range rows {
		inputs = append(inputs, models.DashboardSummaryInput{ID: int(r.ID), Revision: int(r.Revision), PanelsJSON: json.RawMessage(r.PanelsJson)})
	}
	return inputs, nil
}

// RepairDashboardSummary writes summary while the row still holds
// expectedRevision and is still stale, and returns the affected-row count.
func (s *Store) RepairDashboardSummary(ctx context.Context, id, expectedRevision int, summary models.DashboardPanelsSummary) (int64, error) {
	n, err := s.q.RepairDashboardSummary(ctx, sqlc.RepairDashboardSummaryParams{
		PanelCount:       int32(summary.PanelCount), //nolint:gosec // G115: at most 24 panels
		TabCount:         int32(summary.TabCount),   //nolint:gosec // G115: at most 10 tabs
		PanelTypes:       summary.PanelTypesJSON(),
		PanelTeamIds:     summary.PanelTeamIDsJSON(),
		PanelsCorrupt:    summary.Corrupt,
		SummaryRevision:  int32(expectedRevision), //nolint:gosec // G115: the column is INTEGER, as revision is
		SummaryVersion:   models.DashboardSummaryVersion,
		ID:               int64(id),
		Revision:         int64(expectedRevision),
		SummaryVersion_2: models.DashboardSummaryVersion,
	})
	if err != nil {
		return 0, fmt.Errorf("error repairing dashboard %d summary: %w", id, err)
	}
	return n, nil
}

// UpdateDashboard overwrites a dashboard's mutable fields and the derived
// list columns of its blob when the stored revision still equals
// dashboard.Revision, and advances
// dashboard.Revision. Returns models.ErrConflict when another save already
// moved the revision and models.ErrNotFound when the id does not exist.
func (s *Store) UpdateDashboard(ctx context.Context, dashboard *models.Dashboard) error {
	if dashboard == nil {
		return fmt.Errorf("dashboard payload is required")
	}
	summary := models.SummarizeDashboardPanels(dashboard.PanelsJSON)
	revision, err := s.q.UpdateDashboard(ctx, sqlc.UpdateDashboardParams{
		Name:           dashboard.Name,
		Description:    text(dashboard.Description),
		PanelsJson:     string(dashboard.PanelsJSON),
		PanelCount:     int32(summary.PanelCount), //nolint:gosec // G115: at most 24 panels
		TabCount:       int32(summary.TabCount),   //nolint:gosec // G115: at most 10 tabs
		PanelTypes:     summary.PanelTypesJSON(),
		PanelTeamIds:   summary.PanelTeamIDsJSON(),
		PanelsCorrupt:  summary.Corrupt,
		SummaryVersion: models.DashboardSummaryVersion,
		ID:             int64(dashboard.ID),
		Revision:       int64(dashboard.Revision),
	})
	if notFound(err) {
		if _, getErr := s.GetDashboard(ctx, dashboard.ID); getErr != nil {
			return getErr
		}
		return fmt.Errorf("error updating dashboard %d at revision %d: %w", dashboard.ID, dashboard.Revision, models.ErrConflict)
	}
	if err != nil {
		s.log.Error("failed to update dashboard", "error", err, "dashboard_id", dashboard.ID)
		return fmt.Errorf("error updating dashboard: %w", err)
	}
	dashboard.Revision = int(revision)
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
