package sqlite

import (
	"context"
	"fmt"
	"time"

	"backend-v2/pkg/models"
)

// Space team access methods

// SetSpaceTeamAccess sets a team's access to a space
func (d *DB) SetSpaceTeamAccess(ctx context.Context, spaceID, teamID, permission string) error {
	// First check if the space exists
	_, err := d.GetSpace(ctx, spaceID)
	if err != nil {
		return fmt.Errorf("error checking space existence: %w", err)
	}

	// Then check if the team exists
	_, err = d.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("error checking team existence: %w", err)
	}

	// Finally set the team's access to the space
	result, err := d.queries.SetSpaceTeamAccess.ExecContext(ctx,
		spaceID,
		teamID,
		permission,
		time.Now(),
		time.Now(),
	)
	if err != nil {
		if isUniqueConstraintError(err, "space_team_access", "space_id") {
			return fmt.Errorf("team %s already has access to space %s", teamID, spaceID)
		}
		return fmt.Errorf("error setting team access to space: %w", err)
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

// UpdateSpaceTeamAccess updates a team's access to a space
func (d *DB) UpdateSpaceTeamAccess(ctx context.Context, spaceID, teamID, permission string) error {
	// First check if the space team access exists
	_, err := d.GetSpaceTeamAccess(ctx, spaceID, teamID)
	if err != nil {
		return fmt.Errorf("error checking space team access existence: %w", err)
	}

	result, err := d.queries.UpdateSpaceTeamAccess.ExecContext(ctx,
		permission,
		time.Now(),
		spaceID,
		teamID,
	)
	if err != nil {
		return fmt.Errorf("error updating team access to space: %w", err)
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

// RemoveSpaceTeamAccess removes a team's access to a space
func (d *DB) RemoveSpaceTeamAccess(ctx context.Context, spaceID, teamID string) error {
	result, err := d.queries.RemoveSpaceTeamAccess.ExecContext(ctx, spaceID, teamID)
	if err != nil {
		return fmt.Errorf("error removing team access from space: %w", err)
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

// GetSpaceTeamAccess gets a team's access to a space
func (d *DB) GetSpaceTeamAccess(ctx context.Context, spaceID, teamID string) (*models.SpaceTeamAccess, error) {
	var access models.SpaceTeamAccess
	err := d.queries.GetSpaceTeamAccess.GetContext(ctx, &access, spaceID, teamID)
	if err != nil {
		return nil, fmt.Errorf("error getting team access to space: %w", err)
	}
	return &access, nil
}

// ListSpaceTeamAccess lists all team access for a space
func (d *DB) ListSpaceTeamAccess(ctx context.Context, spaceID string) ([]*models.Team, error) {
	var teams []*models.Team
	err := d.queries.ListSpaceTeamAccess.SelectContext(ctx, &teams, spaceID)
	if err != nil {
		return nil, fmt.Errorf("error listing team access for space: %w", err)
	}
	return teams, nil
}
