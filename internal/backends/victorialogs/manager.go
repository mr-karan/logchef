package victorialogs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/backends"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	HealthCheckTimeout         = 5 * time.Second
	DefaultHealthCheckInterval = 30 * time.Second
)

var _ backends.BackendManager = (*Manager)(nil)

type Manager struct {
	clients    map[models.SourceID]*Client
	clientsMux sync.RWMutex
	logger     *slog.Logger
	health     map[models.SourceID]models.SourceHealth
	healthMux  sync.RWMutex
	stopHealth chan struct{}
	healthWG   sync.WaitGroup
}

func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		clients:    make(map[models.SourceID]*Client),
		logger:     logger.With("component", "victorialogs_manager"),
		health:     make(map[models.SourceID]models.SourceHealth),
		stopHealth: make(chan struct{}),
	}
}

func (m *Manager) GetClient(sourceID models.SourceID) (backends.BackendClient, error) {
	m.clientsMux.RLock()
	defer m.clientsMux.RUnlock()

	client, ok := m.clients[sourceID]
	if !ok {
		return nil, fmt.Errorf("VictoriaLogs source %d not found", sourceID)
	}

	return client, nil
}

func (m *Manager) AddSource(ctx context.Context, source *models.Source) error {
	if source.VictoriaLogsConnection == nil {
		return fmt.Errorf("VictoriaLogs connection info is nil for source %d", source.ID)
	}

	m.clientsMux.Lock()
	defer m.clientsMux.Unlock()
	m.healthMux.Lock()
	defer m.healthMux.Unlock()

	m.logger.Debug("adding VictoriaLogs source",
		"source_id", source.ID,
		"url", source.VictoriaLogsConnection.URL,
	)

	if _, exists := m.clients[source.ID]; exists {
		m.logger.Warn("VictoriaLogs source already exists, skipping add", "source_id", source.ID)
		return nil
	}

	client, err := NewClient(ClientOptions{
		URL:       source.VictoriaLogsConnection.URL,
		AccountID: source.VictoriaLogsConnection.AccountID,
		ProjectID: source.VictoriaLogsConnection.ProjectID,
		Timeout:   30 * time.Second,
	}, m.logger)

	if err != nil {
		m.logger.Error("failed to create VictoriaLogs client",
			"source_id", source.ID,
			"error", err)
		m.health[source.ID] = models.SourceHealth{
			SourceID:    source.ID,
			Status:      models.HealthStatusUnhealthy,
			LastChecked: time.Now(),
			Error:       fmt.Sprintf("failed to create client: %v", err),
		}
		return fmt.Errorf("creating VictoriaLogs client: %w", err)
	}

	m.clients[source.ID] = client
	m.health[source.ID] = models.SourceHealth{
		SourceID:    source.ID,
		Status:      models.HealthStatusUnhealthy,
		LastChecked: time.Now(),
		Error:       "Initial connection pending",
	}

	go m.checkSource(context.Background(), source.ID)

	return nil
}

func (m *Manager) RemoveSource(sourceID models.SourceID) error {
	m.logger.Debug("removing VictoriaLogs source", "source_id", sourceID)

	m.clientsMux.Lock()
	client, exists := m.clients[sourceID]
	delete(m.clients, sourceID)
	m.clientsMux.Unlock()

	m.healthMux.Lock()
	delete(m.health, sourceID)
	m.healthMux.Unlock()

	if exists && client != nil {
		if err := client.Close(); err != nil {
			m.logger.Error("error closing VictoriaLogs client",
				"source_id", sourceID,
				"error", err,
			)
		}
	}

	return nil
}

func (m *Manager) GetHealth(ctx context.Context, sourceID models.SourceID) models.SourceHealth {
	m.checkSource(ctx, sourceID)
	return m.GetCachedHealth(sourceID)
}

func (m *Manager) GetCachedHealth(sourceID models.SourceID) models.SourceHealth {
	m.healthMux.RLock()
	health, ok := m.health[sourceID]
	m.healthMux.RUnlock()

	if !ok {
		return models.SourceHealth{
			SourceID:    sourceID,
			Status:      models.HealthStatusUnhealthy,
			LastChecked: time.Time{},
			Error:       "source health not yet checked",
		}
	}
	return health
}

func (m *Manager) CreateTemporaryClient(ctx context.Context, source *models.Source) (backends.BackendClient, error) {
	if source.VictoriaLogsConnection == nil {
		return nil, fmt.Errorf("VictoriaLogs connection info is nil")
	}

	client, err := NewClient(ClientOptions{
		URL:       source.VictoriaLogsConnection.URL,
		AccountID: source.VictoriaLogsConnection.AccountID,
		ProjectID: source.VictoriaLogsConnection.ProjectID,
		Timeout:   10 * time.Second,
	}, m.logger.With("validation", true))

	if err != nil {
		return nil, fmt.Errorf("creating temporary VictoriaLogs client: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx, "", ""); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("VictoriaLogs connection ping failed: %w", err)
	}

	return client, nil
}

func (m *Manager) Close() error {
	m.logger.Debug("closing VictoriaLogs manager")

	m.StopBackgroundHealthChecks()

	waitChan := make(chan struct{})
	go func() {
		m.healthWG.Wait()
		close(waitChan)
	}()

	select {
	case <-waitChan:
	case <-time.After(5 * time.Second):
		m.logger.Warn("VictoriaLogs health check goroutine shutdown timeout")
	}

	m.clientsMux.Lock()
	defer m.clientsMux.Unlock()

	var lastErr error
	for id, client := range m.clients {
		if err := client.Close(); err != nil {
			m.logger.Error("error closing VictoriaLogs client", "source_id", id, "error", err)
			lastErr = err
		}
	}

	m.clients = make(map[models.SourceID]*Client)

	m.healthMux.Lock()
	m.health = make(map[models.SourceID]models.SourceHealth)
	m.healthMux.Unlock()

	return lastErr
}

func (m *Manager) StartBackgroundHealthChecks(interval time.Duration) {
	if interval <= 0 {
		interval = DefaultHealthCheckInterval
	}
	m.logger.Debug("starting VictoriaLogs background health checks", "interval", interval)

	ticker := time.NewTicker(interval)

	m.healthWG.Add(1)

	go func() {
		defer ticker.Stop()
		defer m.healthWG.Done()
		m.checkAllSourcesHealth()

		for {
			select {
			case <-ticker.C:
				m.checkAllSourcesHealth()
			case <-m.stopHealth:
				m.logger.Debug("stopping VictoriaLogs background health checks")
				return
			}
		}
	}()
}

func (m *Manager) StopBackgroundHealthChecks() {
	m.logger.Debug("signaling VictoriaLogs health check stop")
	close(m.stopHealth)
}

func (m *Manager) checkAllSourcesHealth() {
	m.clientsMux.RLock()
	idsToCheck := make([]models.SourceID, 0, len(m.clients))
	for id := range m.clients {
		idsToCheck = append(idsToCheck, id)
	}
	m.clientsMux.RUnlock()

	var wg sync.WaitGroup
	for _, id := range idsToCheck {
		wg.Add(1)
		go func(sourceID models.SourceID) {
			defer wg.Done()
			m.checkSource(context.Background(), sourceID)
		}(id)
	}
	wg.Wait()
}

func (m *Manager) checkSource(ctx context.Context, sourceID models.SourceID) {
	m.clientsMux.RLock()
	client, exists := m.clients[sourceID]
	m.clientsMux.RUnlock()

	if !exists {
		m.updateHealthStatus(sourceID, false, "client not found")
		return
	}

	pingCtx, cancel := context.WithTimeout(ctx, HealthCheckTimeout)
	defer cancel()

	err := client.Ping(pingCtx, "", "")
	if err != nil {
		m.logger.Warn("VictoriaLogs health check failed",
			"source_id", sourceID,
			"error", err)
		m.updateHealthStatus(sourceID, false, err.Error())
		return
	}

	m.updateHealthStatus(sourceID, true, "")
}

func (m *Manager) updateHealthStatus(sourceID models.SourceID, isHealthy bool, errorMsg string) {
	m.healthMux.Lock()
	defer m.healthMux.Unlock()

	newStatus := models.HealthStatusUnhealthy
	if isHealthy {
		newStatus = models.HealthStatusHealthy
	}

	oldHealth, existed := m.health[sourceID]
	statusChanged := !existed || oldHealth.Status != newStatus

	m.health[sourceID] = models.SourceHealth{
		SourceID:    sourceID,
		Status:      newStatus,
		LastChecked: time.Now(),
		Error:       errorMsg,
	}

	if statusChanged {
		if isHealthy {
			m.logger.Debug("VictoriaLogs source healthy", "source_id", sourceID)
		} else {
			m.logger.Warn("VictoriaLogs source unhealthy", "source_id", sourceID, "error", errorMsg)
		}
	}
}
