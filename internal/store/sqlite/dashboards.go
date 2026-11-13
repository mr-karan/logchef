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

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

// CreateDashboard inserts a new dashboard and the derived list columns of its
// blob, then repopulates the model with the persisted row (id and timestamps).
func (db *DB) CreateDashboard(ctx context.Context, dashboard *models.Dashboard) error {
	if dashboard == nil {
		return fmt.Errorf("dashboard payload is required")
	}
	summary := models.SummarizeDashboardPanels(dashboard.PanelsJSON)
	params := sqlc.CreateDashboardParams{
		Name:           dashboard.Name,
		Description:    nullString(dashboard.Description),
		PanelsJson:     string(dashboard.PanelsJSON),
		PanelCount:     int64(summary.PanelCount),
		TabCount:       int64(summary.TabCount),
		PanelTypes:     summary.PanelTypesJSON(),
		PanelTeamIds:   summary.PanelTeamIDsJSON(),
		PanelsCorrupt:  boolInt(summary.Corrupt),
		SummaryVersion: models.DashboardSummaryVersion,
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
		return nil, fmt.Errorf("getting dashboard id %d: %w", id, translateNotFound(err))
	}
	d := &models.Dashboard{
		ID:          int(row.ID),
		Name:        row.Name,
		Description: row.Description.String,
		PanelsJSON:  json.RawMessage(row.PanelsJson),
		Revision:    int(row.Revision),
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

// ListDashboardSummaries returns every dashboard's summary from the derived
// columns, newest-updated first, with creator info.
func (db *DB) ListDashboardSummaries(ctx context.Context) ([]*models.DashboardSummary, error) {
	rows, err := db.readQueries.ListDashboardSummaries(ctx)
	if err != nil {
		db.log.Error("failed to list dashboards", "error", err)
		return nil, fmt.Errorf("error listing dashboards: %w", err)
	}

	summaries := make([]*models.DashboardSummary, 0, len(rows))
	for i := range rows {
		r := rows[i]
		types, teams, err := models.ParseDashboardSummaryColumns(r.PanelTypes, r.PanelTeamIds)
		if err != nil {
			return nil, fmt.Errorf("error listing dashboards: row %d: %w", r.ID, err)
		}
		s := &models.DashboardSummary{
			ID:              int(r.ID),
			Name:            r.Name,
			Description:     r.Description,
			PanelCount:      int(r.PanelCount),
			PanelTypes:      types,
			TabCount:        int(r.TabCount),
			CreatedByEmail:  r.CreatedByEmail.String,
			CreatedByName:   r.CreatedByName.String,
			Revision:        int(r.Revision),
			Corrupt:         r.PanelsCorrupt != 0,
			Timestamps:      models.Timestamps{CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt},
			PanelTeamIDs:    teams,
			SummaryRevision: int(r.SummaryRevision),
			SummaryVersion:  int(r.SummaryVersion),
		}
		if r.CreatedBy.Valid {
			uid := models.UserID(r.CreatedBy.Int64)
			s.CreatedBy = &uid
		}
		summaries = append(summaries, s)
	}
	return summaries, nil
}

// ListStaleDashboardSummaryInputs returns stale rows above afterID in id order.
func (db *DB) ListStaleDashboardSummaryInputs(ctx context.Context, afterID, limit int) ([]models.DashboardSummaryInput, error) {
	rows, err := db.readQueries.ListStaleDashboardSummaryInputs(ctx, sqlc.ListStaleDashboardSummaryInputsParams{
		ID:             int64(afterID),
		SummaryVersion: models.DashboardSummaryVersion,
		Limit:          int64(limit),
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
func (db *DB) RepairDashboardSummary(ctx context.Context, id, expectedRevision int, summary models.DashboardPanelsSummary) (int64, error) {
	n, err := db.writeQueries.RepairDashboardSummary(ctx, sqlc.RepairDashboardSummaryParams{
		PanelCount:       int64(summary.PanelCount),
		TabCount:         int64(summary.TabCount),
		PanelTypes:       summary.PanelTypesJSON(),
		PanelTeamIds:     summary.PanelTeamIDsJSON(),
		PanelsCorrupt:    boolInt(summary.Corrupt),
		SummaryRevision:  int64(expectedRevision),
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
func (db *DB) UpdateDashboard(ctx context.Context, dashboard *models.Dashboard) error {
	if dashboard == nil {
		return fmt.Errorf("dashboard payload is required")
	}
	summary := models.SummarizeDashboardPanels(dashboard.PanelsJSON)
	revision, err := db.writeQueries.UpdateDashboard(ctx, sqlc.UpdateDashboardParams{
		Name:           dashboard.Name,
		Description:    nullString(dashboard.Description),
		PanelsJson:     string(dashboard.PanelsJSON),
		PanelCount:     int64(summary.PanelCount),
		TabCount:       int64(summary.TabCount),
		PanelTypes:     summary.PanelTypesJSON(),
		PanelTeamIds:   summary.PanelTeamIDsJSON(),
		PanelsCorrupt:  boolInt(summary.Corrupt),
		SummaryVersion: models.DashboardSummaryVersion,
		ID:             int64(dashboard.ID),
		Revision:       int64(dashboard.Revision),
	})
	if errors.Is(err, sql.ErrNoRows) {
		if _, getErr := db.GetDashboard(ctx, dashboard.ID); getErr != nil {
			return fmt.Errorf("error updating dashboard: %w", getErr)
		}
		return fmt.Errorf("error updating dashboard %d at revision %d: %w", dashboard.ID, dashboard.Revision, models.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("error updating dashboard: %w", err)
	}
	dashboard.Revision = int(revision)
	return nil
}

// DeleteDashboard removes a dashboard. Returns models.ErrNotFound when the id
// does not exist.
func (db *DB) DeleteDashboard(ctx context.Context, id int) error {
	if _, err := db.writeQueries.DeleteDashboard(ctx, int64(id)); err != nil {
		return fmt.Errorf("error deleting dashboard: %w", translateNotFound(err))
	}
	return nil
}
