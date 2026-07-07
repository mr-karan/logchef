package postgres

import (
	"context"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// IsSourceManaged returns true if the source is managed by provisioning config.
func (s *Store) IsSourceManaged(ctx context.Context, id models.SourceID) (bool, error) {
	managed, err := s.q.IsSourceManaged(ctx, int64(id))
	if err != nil {
		return false, err
	}
	return managed, nil
}

// IsTeamManaged returns true if the team is managed by provisioning config.
func (s *Store) IsTeamManaged(ctx context.Context, id models.TeamID) (bool, error) {
	managed, err := s.q.IsTeamManaged(ctx, int64(id))
	if err != nil {
		return false, err
	}
	return managed, nil
}

// IsUserManaged returns true if the user is managed by provisioning config.
func (s *Store) IsUserManaged(ctx context.Context, id models.UserID) (bool, error) {
	managed, err := s.q.IsUserManaged(ctx, int64(id))
	if err != nil {
		return false, err
	}
	return managed, nil
}

// ListManagedSources returns all sources currently marked managed.
func (s *Store) ListManagedSources(ctx context.Context) ([]*models.Source, error) {
	rows, err := s.q.ListManagedSources(ctx)
	if err != nil {
		s.log.Error("failed to list managed sources", "error", err)
		return nil, fmt.Errorf("error listing managed sources: %w", err)
	}
	sources := make([]*models.Source, 0, len(rows))
	for i := range rows {
		r := rows[i]
		sources = append(sources, sourceToModel(r))
	}
	return sources, nil
}

// ListManagedTeams returns all teams currently marked managed.
func (s *Store) ListManagedTeams(ctx context.Context) ([]*models.Team, error) {
	rows, err := s.q.ListManagedTeams(ctx)
	if err != nil {
		s.log.Error("failed to list managed teams", "error", err)
		return nil, fmt.Errorf("error listing managed teams: %w", err)
	}
	teams := make([]*models.Team, 0, len(rows))
	for _, r := range rows {
		teams = append(teams, teamToModel(r))
	}
	return teams, nil
}

// GetSourceByNameForProvisioning looks a source up by name (managed or not).
// Returns models.ErrNotFound when no source has that name.
func (s *Store) GetSourceByNameForProvisioning(ctx context.Context, name string) (*models.Source, error) {
	row, err := s.q.GetSourceByNameForProvisioning(ctx, name)
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		s.log.Error("failed to get source by name for provisioning", "error", err, "name", name)
		return nil, fmt.Errorf("error getting source by name: %w", err)
	}
	return sourceToModel(row), nil
}

// SetSourceManaged marks a source managed/unmanaged, recording the secret ref.
func (s *Store) SetSourceManaged(ctx context.Context, id models.SourceID, managed bool, secretRef string) error {
	err := s.q.SetSourceManaged(ctx, sqlc.SetSourceManagedParams{
		Managed:   managed,
		SecretRef: text(secretRef),
		ID:        int64(id),
	})
	if err != nil {
		s.log.Error("failed to set source managed flag", "error", err, "source_id", id)
		return fmt.Errorf("error setting source managed: %w", err)
	}
	return nil
}

// SetTeamManaged marks a team managed/unmanaged.
func (s *Store) SetTeamManaged(ctx context.Context, id models.TeamID, managed bool) error {
	err := s.q.SetTeamManaged(ctx, sqlc.SetTeamManagedParams{Managed: managed, ID: int64(id)})
	if err != nil {
		s.log.Error("failed to set team managed flag", "error", err, "team_id", id)
		return fmt.Errorf("error setting team managed: %w", err)
	}
	return nil
}

// SetUserManaged marks a user managed/unmanaged.
func (s *Store) SetUserManaged(ctx context.Context, id models.UserID, managed bool) error {
	err := s.q.SetUserManaged(ctx, sqlc.SetUserManagedParams{Managed: managed, ID: int64(id)})
	if err != nil {
		s.log.Error("failed to set user managed flag", "error", err, "user_id", id)
		return fmt.Errorf("error setting user managed: %w", err)
	}
	return nil
}
