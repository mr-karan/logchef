package backends

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

type BackendRegistry struct {
	mu       sync.RWMutex
	managers map[models.BackendType]BackendManager
	sources  map[models.SourceID]models.BackendType
	logger   *slog.Logger
}

func NewBackendRegistry(logger *slog.Logger) *BackendRegistry {
	return &BackendRegistry{
		managers: make(map[models.BackendType]BackendManager),
		sources:  make(map[models.SourceID]models.BackendType),
		logger:   logger,
	}
}

func (r *BackendRegistry) RegisterClickHouseManager(manager *clickhouse.Manager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.managers[models.BackendClickHouse] = NewClickHouseManagerAdapter(manager)
}

func (r *BackendRegistry) RegisterManager(backendType models.BackendType, manager BackendManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.managers[backendType] = manager
}

func (r *BackendRegistry) GetClient(sourceID models.SourceID) (BackendClient, error) {
	r.mu.RLock()
	backendType, ok := r.sources[sourceID]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("source %d not registered in backend registry", sourceID)
	}

	r.mu.RLock()
	manager, ok := r.managers[backendType]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no manager registered for backend type: %s", backendType)
	}

	return manager.GetClient(sourceID)
}

func (r *BackendRegistry) GetClickHouseClient(sourceID models.SourceID) (*clickhouse.Client, error) {
	client, err := r.GetClient(sourceID)
	if err != nil {
		return nil, err
	}

	adapter, ok := client.(*ClickHouseAdapter)
	if !ok {
		return nil, fmt.Errorf("source %d is not a ClickHouse source", sourceID)
	}

	return adapter.UnwrapClient(), nil
}

func (r *BackendRegistry) AddSource(ctx context.Context, source *models.Source) error {
	backendType := source.GetEffectiveBackendType()

	r.mu.RLock()
	manager, ok := r.managers[backendType]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no manager registered for backend type: %s", backendType)
	}

	if err := manager.AddSource(ctx, source); err != nil {
		return err
	}

	r.mu.Lock()
	r.sources[source.ID] = backendType
	r.mu.Unlock()

	return nil
}

func (r *BackendRegistry) RemoveSource(sourceID models.SourceID) error {
	r.mu.RLock()
	backendType, ok := r.sources[sourceID]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("source %d not registered in backend registry", sourceID)
	}

	r.mu.RLock()
	manager, ok := r.managers[backendType]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no manager registered for backend type: %s", backendType)
	}

	if err := manager.RemoveSource(sourceID); err != nil {
		return err
	}

	r.mu.Lock()
	delete(r.sources, sourceID)
	r.mu.Unlock()

	return nil
}

func (r *BackendRegistry) GetHealth(ctx context.Context, sourceID models.SourceID) models.SourceHealth {
	r.mu.RLock()
	backendType, ok := r.sources[sourceID]
	r.mu.RUnlock()

	if !ok {
		return models.SourceHealth{
			SourceID:    sourceID,
			Status:      models.HealthStatusUnhealthy,
			Error:       "source not registered",
			LastChecked: time.Now(),
		}
	}

	r.mu.RLock()
	manager, ok := r.managers[backendType]
	r.mu.RUnlock()

	if !ok {
		return models.SourceHealth{
			SourceID:    sourceID,
			Status:      models.HealthStatusUnhealthy,
			Error:       fmt.Sprintf("no manager for backend type: %s", backendType),
			LastChecked: time.Now(),
		}
	}

	return manager.GetHealth(ctx, sourceID)
}

func (r *BackendRegistry) GetCachedHealth(sourceID models.SourceID) models.SourceHealth {
	r.mu.RLock()
	backendType, ok := r.sources[sourceID]
	r.mu.RUnlock()

	if !ok {
		return models.SourceHealth{
			SourceID:    sourceID,
			Status:      models.HealthStatusUnhealthy,
			Error:       "source not registered",
			LastChecked: time.Now(),
		}
	}

	r.mu.RLock()
	manager, ok := r.managers[backendType]
	r.mu.RUnlock()

	if !ok {
		return models.SourceHealth{
			SourceID:    sourceID,
			Status:      models.HealthStatusUnhealthy,
			Error:       fmt.Sprintf("no manager for backend type: %s", backendType),
			LastChecked: time.Now(),
		}
	}

	return manager.GetCachedHealth(sourceID)
}

func (r *BackendRegistry) CreateTemporaryClient(ctx context.Context, source *models.Source) (BackendClient, error) {
	backendType := source.GetEffectiveBackendType()

	r.mu.RLock()
	manager, ok := r.managers[backendType]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no manager registered for backend type: %s", backendType)
	}

	return manager.CreateTemporaryClient(ctx, source)
}

func (r *BackendRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for _, manager := range r.managers {
		if err := manager.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (r *BackendRegistry) StartBackgroundHealthChecks(interval time.Duration) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, manager := range r.managers {
		manager.StartBackgroundHealthChecks(interval)
	}
}

func (r *BackendRegistry) StopBackgroundHealthChecks() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, manager := range r.managers {
		manager.StopBackgroundHealthChecks()
	}
}

func (r *BackendRegistry) GetClickHouseManager() *clickhouse.Manager {
	r.mu.RLock()
	defer r.mu.RUnlock()

	manager, ok := r.managers[models.BackendClickHouse]
	if !ok {
		return nil
	}

	adapter, ok := manager.(*ClickHouseManagerAdapter)
	if !ok {
		return nil
	}

	return adapter.UnwrapManager()
}
