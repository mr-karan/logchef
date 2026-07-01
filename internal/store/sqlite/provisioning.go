package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// IsSourceManaged returns true if the source is managed by provisioning config.
func (db *DB) IsSourceManaged(ctx context.Context, id models.SourceID) (bool, error) {
	managed, err := db.readQueries.IsSourceManaged(ctx, int64(id))
	if err != nil {
		return false, err
	}
	return managed == 1, nil
}

// IsTeamManaged returns true if the team is managed by provisioning config.
func (db *DB) IsTeamManaged(ctx context.Context, id models.TeamID) (bool, error) {
	managed, err := db.readQueries.IsTeamManaged(ctx, int64(id))
	if err != nil {
		return false, err
	}
	return managed == 1, nil
}

// IsUserManaged returns true if the user is managed by provisioning config.
func (db *DB) IsUserManaged(ctx context.Context, id models.UserID) (bool, error) {
	managed, err := db.readQueries.IsUserManaged(ctx, int64(id))
	if err != nil {
		return false, err
	}
	return managed == 1, nil
}

// ListManagedSources returns all sources currently marked managed.
func (db *DB) ListManagedSources(ctx context.Context) ([]*models.Source, error) {
	rows, err := db.readQueries.ListManagedSources(ctx)
	if err != nil {
		db.log.Error("failed to list managed sources", "error", err)
		return nil, fmt.Errorf("error listing managed sources: %w", err)
	}
	sources := make([]*models.Source, 0, len(rows))
	for i := range rows {
		if s := mapSourceRowToModel(&rows[i]); s != nil {
			sources = append(sources, s)
		}
	}
	return sources, nil
}

// ListManagedTeams returns all teams currently marked managed.
func (db *DB) ListManagedTeams(ctx context.Context) ([]*models.Team, error) {
	rows, err := db.readQueries.ListManagedTeams(ctx)
	if err != nil {
		db.log.Error("failed to list managed teams", "error", err)
		return nil, fmt.Errorf("error listing managed teams: %w", err)
	}
	teams := make([]*models.Team, 0, len(rows))
	for _, row := range rows {
		teams = append(teams, &models.Team{
			ID:          models.TeamID(row.ID),
			Name:        row.Name,
			Description: row.Description.String,
			Timestamps: models.Timestamps{
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
			Managed: row.Managed == 1,
		})
	}
	return teams, nil
}

// GetSourceByNameForProvisioning looks a source up by name (managed or not).
// Returns models.ErrNotFound when no source has that name.
func (db *DB) GetSourceByNameForProvisioning(ctx context.Context, name string) (*models.Source, error) {
	row, err := db.readQueries.GetSourceByNameForProvisioning(ctx, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrNotFound
		}
		db.log.Error("failed to get source by name for provisioning", "error", err, "name", name)
		return nil, fmt.Errorf("error getting source by name: %w", err)
	}
	return mapSourceRowToModel(&row), nil
}

// SetSourceManaged marks a source managed/unmanaged, recording the secret
// reference that provided its credentials.
func (db *DB) SetSourceManaged(ctx context.Context, id models.SourceID, managed bool, secretRef string) error {
	err := db.writeQueries.SetSourceManaged(ctx, sqlc.SetSourceManagedParams{
		Managed:   boolToInt(managed),
		SecretRef: sql.NullString{String: secretRef, Valid: secretRef != ""},
		ID:        int64(id),
	})
	if err != nil {
		db.log.Error("failed to set source managed flag", "error", err, "source_id", id)
		return fmt.Errorf("error setting source managed: %w", err)
	}
	return nil
}

// SetTeamManaged marks a team managed/unmanaged.
func (db *DB) SetTeamManaged(ctx context.Context, id models.TeamID, managed bool) error {
	err := db.writeQueries.SetTeamManaged(ctx, sqlc.SetTeamManagedParams{
		Managed: boolToInt(managed),
		ID:      int64(id),
	})
	if err != nil {
		db.log.Error("failed to set team managed flag", "error", err, "team_id", id)
		return fmt.Errorf("error setting team managed: %w", err)
	}
	return nil
}

// SetUserManaged marks a user managed/unmanaged.
func (db *DB) SetUserManaged(ctx context.Context, id models.UserID, managed bool) error {
	err := db.writeQueries.SetUserManaged(ctx, sqlc.SetUserManagedParams{
		Managed: boolToInt(managed),
		ID:      int64(id),
	})
	if err != nil {
		db.log.Error("failed to set user managed flag", "error", err, "user_id", id)
		return fmt.Errorf("error setting user managed: %w", err)
	}
	return nil
}
