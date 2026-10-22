package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// ErrSourceNotFound is returned when a source is not found.
var ErrSourceNotFound = fmt.Errorf("source not found")

// ErrSourceAlreadyExists is returned when a source with the same identity already exists.
var ErrSourceAlreadyExists = fmt.Errorf("source already exists")

// ListSources returns stored sources with cached health and provider capabilities.
func ListSources(ctx context.Context, db store.Store, ds *datasource.Service) ([]*models.Source, error) {
	sources, err := db.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing sources: %w", err)
	}

	if err := populateSourceStatuses(ds, sources); err != nil {
		return nil, err
	}

	return sources, nil
}

func populateSourceStatuses(ds *datasource.Service, sources []*models.Source) error {
	for _, source := range sources {
		if source == nil {
			continue
		}
		source.IsConnected = ds.CachedSourceConnectionStatus(source)
		source.Columns = nil
		source.Schema = ""
		source.Engine = ""
		source.EngineParams = nil
		source.SortKeys = nil
		if err := ds.ApplySourceMetadata(source); err != nil {
			return fmt.Errorf("error annotating source features: %w", err)
		}
	}

	return nil
}

// LoadSource returns stored source metadata with provider capabilities and no backend requests.
func LoadSource(ctx context.Context, ds *datasource.Service, id models.SourceID) (*models.Source, error) {
	source, err := ds.LoadSource(ctx, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrSourceNotFound
		}
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if err := ds.ApplySourceMetadata(source); err != nil {
		return nil, fmt.Errorf("error annotating source features: %w", err)
	}
	return source, nil
}

// GetSource retrieves a source by ID including connection status and provider-populated details.
func GetSource(ctx context.Context, ds *datasource.Service, id models.SourceID) (*models.Source, error) {
	source, err := ds.GetSource(ctx, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
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
		if errors.Is(err, models.ErrNotFound) {
			return ErrSourceNotFound
		}
		return fmt.Errorf("error deleting source: %w", err)
	}
	return nil
}

type SourceInspection = datasource.SourceInspection

func InspectSource(ctx context.Context, ds *datasource.Service, sourceID models.SourceID) (*SourceInspection, error) {
	result, err := ds.InspectSource(ctx, sourceID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrSourceNotFound
		}
		return nil, err
	}
	return result, nil
}

func RefreshSourceInspection(ctx context.Context, ds *datasource.Service, sourceID models.SourceID) (*SourceInspection, error) {
	result, err := ds.RefreshSourceInspection(ctx, sourceID)
	if errors.Is(err, models.ErrNotFound) {
		return nil, ErrSourceNotFound
	}
	return result, err
}

// InspectTeamSourceActivity returns recent ingestion activity for a source
// the caller reached through a team.
func InspectTeamSourceActivity(ctx context.Context, ds *datasource.Service, src access.AuthorizedSource, refresh bool) (*datasource.SourceActivity, error) {
	return inspectSourceActivity(ctx, ds, src.SourceID(), refresh)
}

// InspectSourceActivityAsAdmin returns recent ingestion activity for any
// source. The principal must be a global admin and hold sources:read.
func InspectSourceActivityAsAdmin(ctx context.Context, ds *datasource.Service, p access.Principal, sourceID models.SourceID, refresh bool) (*datasource.SourceActivity, error) {
	if err := p.RequireGlobalAdmin(); err != nil {
		return nil, err
	}
	if err := p.Require(models.TokenScopeSourcesRead); err != nil {
		return nil, err
	}
	return inspectSourceActivity(ctx, ds, sourceID, refresh)
}

func inspectSourceActivity(ctx context.Context, ds *datasource.Service, sourceID models.SourceID, refresh bool) (*datasource.SourceActivity, error) {
	var result *datasource.SourceActivity
	var err error
	if refresh {
		result, err = ds.RefreshSourceActivity(ctx, sourceID)
	} else {
		result, err = ds.InspectSourceActivity(ctx, sourceID)
	}
	if errors.Is(err, models.ErrNotFound) {
		return nil, ErrSourceNotFound
	}
	return result, err
}
