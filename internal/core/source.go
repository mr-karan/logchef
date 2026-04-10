package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// ErrSourceNotFound is returned when a source is not found.
var ErrSourceNotFound = fmt.Errorf("source not found")

// ErrSourceAlreadyExists is returned when a source with the same identity already exists.
var ErrSourceAlreadyExists = fmt.Errorf("source already exists")

// ListSources returns all sources with basic connection status but without schema details.
// This is optimized for list views where source metadata is enough.
func ListSources(ctx context.Context, db *sqlite.DB, ds *datasource.Service) ([]*models.Source, error) {
	sources, err := db.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing sources: %w", err)
	}

	var wg sync.WaitGroup
	for i := range sources {
		source := sources[i]
		if source == nil {
			continue
		}

		source.IsConnected = false
		source.Columns = nil
		source.Schema = ""
		source.Engine = ""
		source.EngineParams = nil
		source.SortKeys = nil
		if err := ds.ApplySourceMetadata(source); err != nil {
			return nil, fmt.Errorf("error annotating source features: %w", err)
		}

		wg.Add(1)
		go func(s *models.Source) {
			defer wg.Done()
			s.IsConnected = ds.CheckSourceConnectionStatus(ctx, s)
		}(source)
	}
	wg.Wait()

	return sources, nil
}

// GetSource retrieves a source by ID including connection status and provider-populated details.
func GetSource(ctx context.Context, ds *datasource.Service, id models.SourceID) (*models.Source, error) {
	source, err := ds.GetSource(ctx, id)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return nil, ErrSourceNotFound
		}
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	return source, nil
}

// UpdateSource updates an existing source through the datasource service.
func UpdateSource(ctx context.Context, ds *datasource.Service, id models.SourceID, req *models.UpdateSourceRequest) (*models.Source, error) {
	source, err := ds.UpdateSource(ctx, id, req)
	if err != nil {
		return nil, normalizeDatasourceError(err)
	}
	return source, nil
}

// DeleteSource removes a source and its provider state.
func DeleteSource(ctx context.Context, ds *datasource.Service, id models.SourceID) error {
	if err := ds.DeleteSource(ctx, id); err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return ErrSourceNotFound
		}
		return fmt.Errorf("error deleting source: %w", err)
	}
	return nil
}

// GetSourceHealth retrieves the health status of a source from its registered datasource provider.
func GetSourceHealth(ctx context.Context, ds *datasource.Service, id models.SourceID) (models.SourceHealth, error) {
	health, err := ds.GetSourceHealth(ctx, id)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return models.SourceHealth{}, ErrSourceNotFound
		}
		return models.SourceHealth{}, fmt.Errorf("error getting source health: %w", err)
	}
	return health, nil
}

type SourceInspection = datasource.SourceInspection

func InspectSource(ctx context.Context, ds *datasource.Service, sourceID models.SourceID) (*SourceInspection, error) {
	result, err := ds.InspectSource(ctx, sourceID)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return nil, ErrSourceNotFound
		}
		return nil, err
	}
	return result, nil
}
