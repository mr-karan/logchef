package sqlite

import (
	"context"
	"fmt"
	"time"

	"backend-v2/pkg/models"
)

// Team source methods

// AddTeamSource adds a source to a team
func (db *DB) AddTeamSource(ctx context.Context, teamID, sourceID string) error {
	result, err := db.queries.AddTeamSource.ExecContext(ctx,
		teamID,
		sourceID,
		time.Now(),
	)
	if err != nil {
		if isUniqueConstraintError(err, "team_sources", "team_id") {
			return fmt.Errorf("source %s is already in team %s", sourceID, teamID)
		}
		return fmt.Errorf("error adding team source: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// RemoveTeamSource removes a source from a team
func (db *DB) RemoveTeamSource(ctx context.Context, teamID, sourceID string) error {
	result, err := db.queries.RemoveTeamSource.ExecContext(ctx, teamID, sourceID)
	if err != nil {
		return fmt.Errorf("error removing team source: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// ListTeamSources lists all sources for a team
func (db *DB) ListTeamSources(ctx context.Context, teamID string) ([]*models.Source, error) {
	var rows []struct {
		ID          string    `db:"id"`
		Database    string    `db:"database"`
		TableName   string    `db:"table_name"`
		Description string    `db:"description"`
		CreatedAt   time.Time `db:"created_at"`
	}
	err := db.queries.ListTeamSources.SelectContext(ctx, &rows, teamID)
	if err != nil {
		return nil, fmt.Errorf("error listing team sources: %w", err)
	}

	sources := make([]*models.Source, len(rows))
	for i, row := range rows {
		sources[i] = &models.Source{
			ID:          row.ID,
			Description: row.Description,
			Connection: models.ConnectionInfo{
				Database:  row.Database,
				TableName: row.TableName,
			},
			CreatedAt: row.CreatedAt,
		}
	}

	return sources, nil
}

// ListSourceTeams returns all teams that have access to a source
func (db *DB) ListSourceTeams(ctx context.Context, sourceID string) ([]*models.Team, error) {
	var teams []*models.Team
	err := db.queries.ListSourceTeams.SelectContext(ctx, &teams, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error listing source teams: %w", err)
	}
	return teams, nil
}
