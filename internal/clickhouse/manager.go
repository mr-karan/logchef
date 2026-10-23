package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// Default values
const (
	HealthCheckTimeout         = models.SourceHealthTimeout
	DefaultHealthCheckInterval = 30 * time.Second
)

type healthState uint8

const (
	healthPending healthState = iota
	healthHealthy
	healthUnhealthy
)

// Manager handles pooling and management of multiple ClickHouse client connections,
// one per data source. It also manages query hooks and background health checks.
type Manager struct {
	clients       map[models.SourceID]*Client
	clientsMux    sync.RWMutex
	logger        *slog.Logger
	health        map[models.SourceID]healthState
	healthMux     sync.RWMutex    // Protects the health map.
	hooks         []QueryHook     // Hooks applied to all managed clients; fixed at construction.
	healthCtx     context.Context //nolint:containedctx // The manager owns cancellation of checks that outlive requests.
	cancelHealth  context.CancelFunc
	healthWG      sync.WaitGroup
	healthStarted bool
	healthStopped bool
}

// NewManager creates a ClickHouse connection manager whose clients all run hooks.
func NewManager(log *slog.Logger, hooks ...QueryHook) *Manager {
	healthCtx, cancelHealth := context.WithCancel(context.Background())
	return &Manager{
		clients:      make(map[models.SourceID]*Client),
		logger:       log.With("component", "clickhouse_manager"),
		health:       make(map[models.SourceID]healthState),
		hooks:        hooks,
		healthCtx:    healthCtx,
		cancelHealth: cancelHealth,
	}
}

// StartBackgroundHealthChecks launches a goroutine to periodically check
// the health of all managed connections.
func (m *Manager) StartBackgroundHealthChecks(interval time.Duration) {
	m.clientsMux.Lock()
	defer m.clientsMux.Unlock()
	if m.healthStarted || m.healthStopped {
		return
	}
	m.healthStarted = true
	if interval <= 0 {
		interval = DefaultHealthCheckInterval
	}
	m.healthWG.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		m.checkAllSourcesHealth()
		for {
			select {
			case <-ticker.C:
				m.checkAllSourcesHealth()
			case <-m.healthCtx.Done():
				return
			}
		}
	})
}

// StopBackgroundHealthChecks cancels periodic and initial health checks.
func (m *Manager) StopBackgroundHealthChecks() {
	m.clientsMux.Lock()
	defer m.clientsMux.Unlock()
	m.healthStopped = true
	m.cancelHealth()
}

// CachedSourceConnectionStatus reports false until a managed client passes a health check.
func (m *Manager) CachedSourceConnectionStatus(sourceID models.SourceID) bool {
	m.healthMux.RLock()
	defer m.healthMux.RUnlock()
	return m.health[sourceID] == healthHealthy
}

// checkAllSourcesHealth iterates through managed clients and updates their health status.
func (m *Manager) checkAllSourcesHealth() {
	m.clientsMux.RLock() // Lock clients map for reading.
	// Create a snapshot of IDs to check to avoid holding the lock during checks.
	idsToCheck := make([]models.SourceID, 0, len(m.clients))
	for id := range m.clients {
		idsToCheck = append(idsToCheck, id)
	}
	m.clientsMux.RUnlock()

	jobs := make(chan models.SourceID)
	var wg sync.WaitGroup
	for range min(32, len(idsToCheck)) {
		wg.Go(func() {
			for id := range jobs {
				m.checkSource(m.healthCtx, id)
			}
		})
	}
	for _, id := range idsToCheck {
		select {
		case jobs <- id:
		case <-m.healthCtx.Done():
			close(jobs)
			wg.Wait()
			return
		}
	}
	close(jobs)
	wg.Wait()
}

// updateHealthStatus ignores results from removed or replaced clients.
func (m *Manager) updateHealthStatus(sourceID models.SourceID, client *Client, err error) {
	m.clientsMux.RLock()
	defer m.clientsMux.RUnlock()
	if m.clients[sourceID] != client || m.healthStopped {
		return
	}

	m.healthMux.Lock()
	defer m.healthMux.Unlock()

	if client.metrics != nil {
		client.metrics.UpdateConnectionStatus(err == nil)
	}
	newState := healthUnhealthy
	if err == nil {
		newState = healthHealthy
	}
	oldState := m.health[sourceID]
	m.health[sourceID] = newState
	if oldState == newState {
		return
	}
	switch {
	case err != nil:
		m.logger.Warn("source unhealthy", "source_id", sourceID, "error", err)
	case oldState == healthUnhealthy:
		m.logger.Info("source healthy", "source_id", sourceID)
	default:
		m.logger.Debug("source healthy", "source_id", sourceID)
	}
}

// checkSource only records health: the driver pool redials dead connections itself.
func (m *Manager) checkSource(ctx context.Context, sourceID models.SourceID) {
	client, err := m.GetConnection(sourceID)
	if err != nil {
		return
	}

	pingCtx, cancel := context.WithTimeout(ctx, HealthCheckTimeout)
	defer cancel()
	m.updateHealthStatus(sourceID, client, client.Ping(pingCtx, client.source.Connection.Database, client.source.Connection.TableName))
}

// AddSource creates a managed client and schedules its first health check.
func (m *Manager) AddSource(ctx context.Context, source *models.Source) error {
	m.clientsMux.Lock() // Lock clients map for writing.
	defer m.clientsMux.Unlock()
	m.healthMux.Lock() // Lock health map for writing.
	defer m.healthMux.Unlock()

	if m.healthStopped {
		return fmt.Errorf("clickhouse manager is closed")
	}

	m.logger.Debug("adding source",
		"source_id", source.ID,
		"database", source.Connection.Database,
		"table", source.Connection.TableName,
	)

	// Check if client already exists to prevent overwriting.
	if _, exists := m.clients[source.ID]; exists {
		m.logger.Warn("source already exists in manager, skipping add", "source_id", source.ID)
		return nil
	}

	// Create new client without initial ping validation
	client, err := NewClient(ClientOptions{
		Host:          source.Connection.Host,
		Database:      source.Connection.Database,
		Username:      source.Connection.Username,
		Password:      source.Connection.Password,
		SourceID:      strconv.FormatInt(int64(source.ID), 10), // Convert SourceID to string for metrics
		Source:        source,                                  // Pass source for enhanced metrics
		TLSEnable:     source.Connection.TLSEnable,
		QuerySettings: source.Connection.Settings.ToSettingsMap(), // Per-source query settings.
		QueryHooks:    m.hooks,
	}, m.logger)

	if err != nil {
		// If client creation fails completely (not just connection), log and return error
		m.logger.Error("failed to create client instance",
			"source_id", source.ID,
			"error", err)
		return fmt.Errorf("creating client: %w", err)
	}

	m.clients[source.ID] = client
	m.health[source.ID] = healthPending

	// Trigger an immediate check for the newly added source in the background
	m.healthWG.Go(func() { m.checkSource(m.healthCtx, source.ID) })

	return nil
}

// RemoveSource closes the connection for the given source ID and removes it from the manager.
func (m *Manager) RemoveSource(sourceID models.SourceID) error {
	m.logger.Debug("removing source", "source_id", sourceID)

	m.clientsMux.Lock()
	client, exists := m.clients[sourceID]
	delete(m.clients, sourceID)
	m.healthMux.Lock()
	delete(m.health, sourceID)
	m.healthMux.Unlock()
	m.clientsMux.Unlock()

	if exists && client != nil {
		if err := client.Close(); err != nil {
			m.logger.Error("error closing client during removal",
				"source_id", sourceID,
				"error", err,
			)
		}
	} else {
		m.logger.Warn("attempted to remove source not found in manager", "source_id", sourceID)
	}

	return nil
}

// GetConnection returns the managed client connection for a given source ID.
// Returns ErrSourceNotConnected if the source is not currently managed.
func (m *Manager) GetConnection(sourceID models.SourceID) (*Client, error) {
	m.clientsMux.RLock()
	defer m.clientsMux.RUnlock()

	client, ok := m.clients[sourceID]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrSourceNotConnected, sourceID)
	}

	return client, nil
}

// Close stops background health checks and closes all managed clients.
func (m *Manager) Close() error {
	m.logger.Debug("closing clickhouse manager")
	m.StopBackgroundHealthChecks()
	m.healthWG.Wait()

	m.clientsMux.Lock()
	defer m.clientsMux.Unlock()

	var lastErr error
	for id, client := range m.clients {
		if err := client.Close(); err != nil {
			m.logger.Error("error closing client", "source_id", id, "error", err)
			lastErr = err
		}
	}
	clear(m.clients)
	m.healthMux.Lock()
	clear(m.health)
	m.healthMux.Unlock()
	return lastErr
}

// CreateTemporaryClient creates a new, unmanaged ClickHouse client instance,
// typically used for validating connection details before adding a source.
// The caller is responsible for closing the returned client.
func (m *Manager) CreateTemporaryClient(ctx context.Context, source *models.Source) (*Client, error) {
	client, err := NewClient(ClientOptions{
		Host:      source.Connection.Host,
		Database:  source.Connection.Database,
		Username:  source.Connection.Username,
		Password:  source.Connection.Password,
		TLSEnable: source.Connection.TLSEnable,
	}, m.logger.With("validation", true))

	if err != nil {
		m.logger.Error("failed to create temporary client", "error", err)
		return nil, fmt.Errorf("error creating temporary client: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, HealthCheckTimeout)
	defer cancel()

	if err := client.Ping(pingCtx, "", ""); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("basic connection ping failed: %w", err)
	}

	return client, nil
}
