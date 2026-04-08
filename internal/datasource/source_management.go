package datasource

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

func (s *Service) GetSource(ctx context.Context, sourceID models.SourceID) (*models.Source, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}

	source.IsConnected = provider.CheckSourceConnectionStatus(ctx, source)
	if err := provider.PopulateSourceDetails(ctx, source); err != nil {
		return nil, fmt.Errorf("populate source details: %w", err)
	}

	return source, nil
}

func (s *Service) UpdateSource(ctx context.Context, sourceID models.SourceID, req *models.UpdateSourceRequest) (*models.Source, error) {
	if req == nil {
		return nil, fmt.Errorf("update source request is required")
	}

	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}

	original := cloneSource(source)
	working := cloneSource(source)

	result, err := provider.UpdateSource(ctx, working, req)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Source == nil {
		return nil, fmt.Errorf("provider returned empty source update result")
	}

	if err := result.Source.SyncConnectionConfig(); err != nil {
		return nil, err
	}

	if !result.Changed {
		return s.GetSource(ctx, sourceID)
	}

	if result.Source.IdentityKey != original.IdentityKey {
		existingSource, err := s.db.GetSourceByIdentityKey(ctx, result.Source.IdentityKey)
		if err == nil && existingSource != nil && existingSource.ID != sourceID {
			return nil, fmt.Errorf("source identity %q already exists (ID: %d): %w", result.Source.IdentityKey, existingSource.ID, ErrSourceAlreadyExists)
		}
		if err != nil && !sqlite.IsNotFoundError(err) && !sqlite.IsSourceNotFoundError(err) {
			return nil, fmt.Errorf("check existing source identity: %w", err)
		}
	}

	if err := s.db.UpdateSource(ctx, result.Source); err != nil {
		return nil, fmt.Errorf("update source configuration: %w", err)
	}

	if result.Reinitialize {
		if err := provider.RemoveSource(sourceID); err != nil {
			s.log.Warn("failed to remove existing datasource connection before reinitialize",
				"source_id", sourceID,
				"error", err)
		}
		if err := provider.InitializeSource(ctx, result.Source); err != nil {
			if rollbackErr := s.db.UpdateSource(ctx, original); rollbackErr != nil {
				s.log.Error("failed to rollback source after initialization error",
					"source_id", sourceID,
					"error", rollbackErr)
			}
			if restoreErr := provider.InitializeSource(ctx, original); restoreErr != nil {
				s.log.Error("failed to restore original datasource connection after initialization error",
					"source_id", sourceID,
					"error", restoreErr)
			}
			return nil, fmt.Errorf("initialize updated source: %w", err)
		}
	}

	return s.GetSource(ctx, sourceID)
}

func (s *Service) DeleteSource(ctx context.Context, sourceID models.SourceID) error {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return err
	}

	if err := provider.RemoveSource(sourceID); err != nil {
		s.log.Warn("failed to remove datasource connection before delete",
			"source_id", sourceID,
			"error", err)
	}

	if err := s.db.DeleteSource(ctx, source.ID); err != nil {
		return fmt.Errorf("delete source from database: %w", err)
	}

	return nil
}

func cloneSource(source *models.Source) *models.Source {
	if source == nil {
		return nil
	}

	cloned := *source
	if source.ConnectionConfig != nil {
		cloned.ConnectionConfig = append(json.RawMessage(nil), source.ConnectionConfig...)
	}
	if source.Columns != nil {
		cloned.Columns = append([]models.ColumnInfo(nil), source.Columns...)
	}
	if source.EngineParams != nil {
		cloned.EngineParams = append([]string(nil), source.EngineParams...)
	}
	if source.SortKeys != nil {
		cloned.SortKeys = append([]string(nil), source.SortKeys...)
	}

	return &cloned
}
