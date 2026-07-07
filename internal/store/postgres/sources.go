package postgres

import (
	"context"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func sourceToModel(r sqlc.Source) *models.Source {
	source := &models.Source{
		ID:                models.SourceID(r.ID),
		Name:              r.Name,
		MetaIsAutoCreated: r.MetaIsAutoCreated,
		SourceType:        models.SourceType(r.SourceType),
		MetaTSField:       r.MetaTsField,
		MetaSeverityField: textStr(r.MetaSeverityField),
		Description:       textStr(r.Description),
		TTLDays:           int(r.TtlDays),
		ConnectionConfig:  r.ConnectionConfig,
		IdentityKey:       r.IdentityKey,
		Timestamps:        models.Timestamps{CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time},
		Managed:           r.Managed,
		SecretRef:         textStr(r.SecretRef),
	}
	_ = source.HydrateConnection()
	return source
}

// CreateSource inserts a new source and populates ID + timestamps on success.
func (s *Store) CreateSource(ctx context.Context, source *models.Source) error {
	if err := source.SyncConnectionConfig(); err != nil {
		return fmt.Errorf("prepare source connection config: %w", err)
	}
	id, err := s.q.CreateSource(ctx, sqlc.CreateSourceParams{
		Name:              source.Name,
		MetaIsAutoCreated: source.MetaIsAutoCreated,
		SourceType:        source.SourceType.String(),
		MetaTsField:       source.MetaTSField,
		MetaSeverityField: text(source.MetaSeverityField),
		ConnectionConfig:  source.ConnectionConfig,
		IdentityKey:       source.IdentityKey,
		Description:       text(source.Description),
		TtlDays:           int64(source.TTLDays),
		Managed:           source.Managed,
		SecretRef:         text(source.SecretRef),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: source %s already exists", models.ErrConflict, source.IdentityKey)
		}
		s.log.Error("failed to create source record in db", "error", err)
		return fmt.Errorf("error creating source record: %w", err)
	}
	source.ID = models.SourceID(id)
	if row, err := s.q.GetSource(ctx, id); err == nil {
		source.CreatedAt = row.CreatedAt.Time
		source.UpdatedAt = row.UpdatedAt.Time
	}
	return nil
}

// GetSource retrieves a source by ID. Returns models.ErrNotFound if absent.
func (s *Store) GetSource(ctx context.Context, id models.SourceID) (*models.Source, error) {
	row, err := s.q.GetSource(ctx, int64(id))
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("getting source id %d: %w", id, err)
	}
	return sourceToModel(row), nil
}

// GetSourceByIdentityKey retrieves a source by its provider-computed identity
// key. Returns models.ErrNotFound if absent.
func (s *Store) GetSourceByIdentityKey(ctx context.Context, identityKey string) (*models.Source, error) {
	row, err := s.q.GetSourceByIdentityKey(ctx, identityKey)
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		s.log.Error("failed to get source by identity key from db", "error", err, "identity_key", identityKey)
		return nil, fmt.Errorf("error getting source by identity key: %w", err)
	}
	return sourceToModel(row), nil
}

// ListSources retrieves all sources, ordered by creation date.
func (s *Store) ListSources(ctx context.Context) ([]*models.Source, error) {
	rows, err := s.q.ListSources(ctx)
	if err != nil {
		s.log.Error("failed to list sources from db", "error", err)
		return nil, fmt.Errorf("error listing sources: %w", err)
	}
	sources := make([]*models.Source, 0, len(rows))
	for i := range rows {
		sources = append(sources, sourceToModel(rows[i]))
	}
	return sources, nil
}

// UpdateSource updates an existing source record.
func (s *Store) UpdateSource(ctx context.Context, source *models.Source) error {
	if err := source.SyncConnectionConfig(); err != nil {
		return fmt.Errorf("prepare source connection config: %w", err)
	}
	err := s.q.UpdateSource(ctx, sqlc.UpdateSourceParams{
		Name:              source.Name,
		MetaIsAutoCreated: source.MetaIsAutoCreated,
		SourceType:        source.SourceType.String(),
		MetaTsField:       source.MetaTSField,
		MetaSeverityField: text(source.MetaSeverityField),
		ConnectionConfig:  source.ConnectionConfig,
		IdentityKey:       source.IdentityKey,
		Description:       text(source.Description),
		TtlDays:           int64(source.TTLDays),
		Managed:           source.Managed,
		SecretRef:         text(source.SecretRef),
		ID:                int64(source.ID),
	})
	if err != nil {
		s.log.Error("failed to update source record in db", "error", err, "source_id", source.ID)
		return fmt.Errorf("error updating source record: %w", err)
	}
	return nil
}

// DeleteSource removes a source by ID.
func (s *Store) DeleteSource(ctx context.Context, id models.SourceID) error {
	if err := s.q.DeleteSource(ctx, int64(id)); err != nil {
		s.log.Error("failed to delete source record from db", "error", err, "source_id", id)
		return fmt.Errorf("error deleting source record: %w", err)
	}
	return nil
}
