package backends

import (
	"context"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

var _ BackendManager = (*ClickHouseManagerAdapter)(nil)

type ClickHouseManagerAdapter struct {
	manager *clickhouse.Manager
}

func NewClickHouseManagerAdapter(manager *clickhouse.Manager) *ClickHouseManagerAdapter {
	return &ClickHouseManagerAdapter{manager: manager}
}

func (a *ClickHouseManagerAdapter) GetClient(sourceID models.SourceID) (BackendClient, error) {
	client, err := a.manager.GetClient(sourceID)
	if err != nil {
		return nil, err
	}
	return NewClickHouseAdapter(client), nil
}

func (a *ClickHouseManagerAdapter) AddSource(ctx context.Context, source *models.Source) error {
	return a.manager.AddSource(ctx, source)
}

func (a *ClickHouseManagerAdapter) RemoveSource(sourceID models.SourceID) error {
	return a.manager.RemoveSource(sourceID)
}

func (a *ClickHouseManagerAdapter) GetHealth(ctx context.Context, sourceID models.SourceID) models.SourceHealth {
	return a.manager.GetHealth(ctx, sourceID)
}

func (a *ClickHouseManagerAdapter) GetCachedHealth(sourceID models.SourceID) models.SourceHealth {
	return a.manager.GetCachedHealth(sourceID)
}

func (a *ClickHouseManagerAdapter) CreateTemporaryClient(ctx context.Context, source *models.Source) (BackendClient, error) {
	client, err := a.manager.CreateTemporaryClient(ctx, source)
	if err != nil {
		return nil, err
	}
	return NewClickHouseAdapter(client), nil
}

func (a *ClickHouseManagerAdapter) Close() error {
	return a.manager.Close()
}

func (a *ClickHouseManagerAdapter) StartBackgroundHealthChecks(interval time.Duration) {
	a.manager.StartBackgroundHealthChecks(interval)
}

func (a *ClickHouseManagerAdapter) StopBackgroundHealthChecks() {
	a.manager.StopBackgroundHealthChecks()
}

func (a *ClickHouseManagerAdapter) UnwrapManager() *clickhouse.Manager {
	return a.manager
}
