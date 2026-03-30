package sqlite

import (
	"context"

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
