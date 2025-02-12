package sqlite

import (
	"context"
	"fmt"
	"time"

	"backend-v2/pkg/models"
)

// Space methods

// CreateSpace creates a new space
func (d *DB) CreateSpace(ctx context.Context, space *models.Space) error {
	result, err := d.queries.CreateSpace.ExecContext(ctx,
		space.ID,
		space.Name,
		space.Description,
		space.CreatedBy,
		time.Now(),
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("error creating space: %w", err)
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

// GetSpace retrieves a space by ID
func (d *DB) GetSpace(ctx context.Context, spaceID string) (*models.Space, error) {
	var space models.Space
	err := d.queries.GetSpace.GetContext(ctx, &space, spaceID)
	if err != nil {
		return nil, fmt.Errorf("error getting space: %w", err)
	}
	return &space, nil
}

// UpdateSpace updates an existing space
func (d *DB) UpdateSpace(ctx context.Context, space *models.Space) error {
	result, err := d.queries.UpdateSpace.ExecContext(ctx,
		space.Name,
		space.Description,
		time.Now(),
		space.ID,
	)
	if err != nil {
		return fmt.Errorf("error updating space: %w", err)
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

// DeleteSpace deletes a space by ID
func (d *DB) DeleteSpace(ctx context.Context, spaceID string) error {
	result, err := d.queries.DeleteSpace.ExecContext(ctx, spaceID)
	if err != nil {
		return fmt.Errorf("error deleting space: %w", err)
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

// ListSpaces returns all spaces
func (d *DB) ListSpaces(ctx context.Context) ([]*models.Space, error) {
	var spaces []*models.Space
	err := d.queries.ListSpaces.SelectContext(ctx, &spaces)
	if err != nil {
		return nil, fmt.Errorf("error listing spaces: %w", err)
	}
	return spaces, nil
}

// Space data source methods

// AddSpaceDataSource adds a data source to a space
func (d *DB) AddSpaceDataSource(ctx context.Context, spaceDS *models.SpaceDataSource) error {
	// First check if the space exists
	_, err := d.GetSpace(ctx, spaceDS.SpaceID)
	if err != nil {
		return fmt.Errorf("error checking space existence: %w", err)
	}

	// Then check if the data source exists
	_, err = d.GetSource(ctx, spaceDS.DataSourceID)
	if err != nil {
		return fmt.Errorf("error checking data source existence: %w", err)
	}

	// Finally add the data source to the space
	result, err := d.queries.AddSpaceDataSource.ExecContext(ctx,
		spaceDS.SpaceID,
		spaceDS.DataSourceID,
		time.Now(),
	)
	if err != nil {
		if isUniqueConstraintError(err, "space_data_sources", "space_id") {
			return fmt.Errorf("data source %s is already added to space %s", spaceDS.DataSourceID, spaceDS.SpaceID)
		}
		return fmt.Errorf("error adding data source to space: %w", err)
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

// RemoveSpaceDataSource removes a data source from a space
func (d *DB) RemoveSpaceDataSource(ctx context.Context, spaceID, dataSourceID string) error {
	result, err := d.queries.RemoveSpaceDataSource.ExecContext(ctx, spaceID, dataSourceID)
	if err != nil {
		return fmt.Errorf("error removing data source from space: %w", err)
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

// ListSpaceDataSources lists all data sources in a space
func (d *DB) ListSpaceDataSources(ctx context.Context, spaceID string) ([]*models.SpaceDataSource, error) {
	var sources []*models.SpaceDataSource
	err := d.queries.ListSpaceDataSources.SelectContext(ctx, &sources, spaceID)
	if err != nil {
		return nil, fmt.Errorf("error listing space data sources: %w", err)
	}
	return sources, nil
}
