package datasource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// LoadSource returns the stored source without contacting the log backend.
func (s *Service) LoadSource(ctx context.Context, sourceID models.SourceID) (*models.Source, error) {
	return s.loadSource(ctx, sourceID)
}

func (s *Service) GetSource(ctx context.Context, sourceID models.SourceID) (*models.Source, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if err := s.populateSource(ctx, provider, source); err != nil {
		return nil, err
	}
	return source, nil
}

// populateSource adds the provider metadata, the connection status and the
// table details to a stored source.
func (s *Service) populateSource(ctx context.Context, provider Provider, source *models.Source) error {
	if err := s.ApplySourceMetadata(source); err != nil {
		return err
	}
	source.IsConnected = provider.CheckSourceConnectionStatus(ctx, source)
	if err := provider.PopulateSourceDetails(ctx, source); err != nil {
		return fmt.Errorf("populate source details: %w", err)
	}
	return nil
}

// UpdateSource applies req to a source. It returns the source before and
// after the write, both read in the transaction that writes, so that a
// concurrent change is never attributed to this update. after carries the
// provider details of GetSource.
func (s *Service) UpdateSource(ctx context.Context, sourceID models.SourceID, req *models.UpdateSourceRequest) (before, after *models.Source, err error) {
	if req == nil {
		return nil, nil, fmt.Errorf("update source request is required")
	}

	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, nil, err
	}

	// The provider may contact the backend, so it works on a snapshot outside
	// the transaction.
	working := cloneSource(source)
	result, err := provider.UpdateSource(ctx, working, req)
	if err != nil {
		return nil, nil, err
	}
	if result == nil || result.Source == nil {
		return nil, nil, fmt.Errorf("provider returned empty source update result")
	}

	if err := result.Source.SyncConnectionConfig(); err != nil {
		return nil, nil, err
	}

	if !result.Changed {
		// Nothing was written, so before and after are one read: a change
		// that another admin made meanwhile is not this update's.
		after, err := s.GetSource(ctx, sourceID)
		return cloneSource(after), after, err
	}

	before, after, err = s.writeSourceUpdate(ctx, result.Source)
	if err != nil {
		return nil, nil, err
	}

	if result.Reinitialize {
		if err := provider.RemoveSource(sourceID); err != nil {
			s.log.Warn("failed to remove existing datasource connection before reinitialize",
				"source_id", sourceID,
				"error", err)
		}
		if err := provider.InitializeSource(ctx, result.Source); err != nil {
			if rollbackErr := s.db.UpdateSource(ctx, before); rollbackErr != nil {
				s.log.Error("failed to rollback source after initialization error",
					"source_id", sourceID,
					"error", rollbackErr)
			}
			if restoreErr := provider.InitializeSource(ctx, before); restoreErr != nil {
				s.log.Error("failed to restore original datasource connection after initialization error",
					"source_id", sourceID,
					"error", restoreErr)
			}
			return nil, nil, fmt.Errorf("initialize updated source: %w", err)
		}
	}

	s.invalidateInspectionCache(sourceID)
	if err := s.populateSource(ctx, provider, after); err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

// writeSourceUpdate stores updated and returns the row it replaced and the
// row it wrote, both read under the row lock.
func (s *Service) writeSourceUpdate(ctx context.Context, updated *models.Source) (before, after *models.Source, err error) {
	err = s.db.WithTx(ctx, func(tx store.StoreOps) error {
		var err error
		if before, err = tx.GetSourceForUpdate(ctx, updated.ID); err != nil {
			return err
		}
		if updated.IdentityKey != before.IdentityKey {
			existing, err := tx.GetSourceByIdentityKey(ctx, updated.IdentityKey)
			if err == nil && existing != nil && existing.ID != updated.ID {
				return fmt.Errorf("source identity %q already exists (ID: %d): %w", updated.IdentityKey, existing.ID, ErrSourceAlreadyExists)
			}
			if err != nil && !errors.Is(err, models.ErrNotFound) {
				return fmt.Errorf("check existing source identity: %w", err)
			}
		}
		if err := tx.UpdateSource(ctx, updated); err != nil {
			return fmt.Errorf("update source configuration: %w", err)
		}
		after, err = tx.GetSource(ctx, updated.ID)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

// DeleteSource deletes a source and returns it as read in the transaction
// that deletes it.
func (s *Service) DeleteSource(ctx context.Context, sourceID models.SourceID) (*models.Source, error) {
	_, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}

	if err := provider.RemoveSource(sourceID); err != nil {
		s.log.Warn("failed to remove datasource connection before delete",
			"source_id", sourceID,
			"error", err)
	}

	var deleted *models.Source
	if err := s.db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.LockMembershipWrites(ctx); err != nil {
			return err
		}
		var err error
		if deleted, err = tx.GetSourceForUpdate(ctx, sourceID); err != nil {
			return err
		}
		return tx.DeleteSource(ctx, sourceID)
	}); err != nil {
		return nil, fmt.Errorf("delete source from database: %w", err)
	}

	s.invalidateInspectionCache(sourceID)
	return deleted, nil
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
