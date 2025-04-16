package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// Default values
const (
	DefaultQueryLimit          = 100
	HealthCheckTimeout         = 5 * time.Second
	DefaultHealthCheckInterval = 30 * time.Second
)

// Manager handles pooling and management of multiple ClickHouse client connections,
// one per data source. It also manages query hooks and background health checks.
type Manager struct {
	clients    map[models.SourceID]*Client
	clientsMux sync.RWMutex // Protects the clients map.
	logger     *slog.Logger
	health     map[models.SourceID]models.SourceHealth
	healthMux  sync.RWMutex  // Protects the health map.
	hooks      []QueryHook   // Hooks applied to all managed clients.
	stopHealth chan struct{} // Channel to signal health check goroutine to stop.
}

// NewManager creates a new ClickHouse connection manager.
func NewManager(log *slog.Logger) *Manager {
	m := &Manager{
		clients:    make(map[models.SourceID]*Client),
		logger:     log.With("component", "clickhouse_manager"),
		health:     make(map[models.SourceID]models.SourceHealth),
		hooks:      []QueryHook{}, // Initialize empty slice.
		stopHealth: make(chan struct{}),
	}

	// Apply default hooks for basic logging.
	m.AddQueryHook(NewLogQueryHook(log, false))
	m.AddQueryHook(NewStructuredQueryLoggerHook(log))

	return m
}

// StartBackgroundHealthChecks launches a goroutine to periodically check
// the health of all managed connections.
func (m *Manager) StartBackgroundHealthChecks(interval time.Duration) {
	if interval <= 0 {
		interval = DefaultHealthCheckInterval
	}
	m.logger.Info("starting background health checks", "interval", interval)

	ticker := time.NewTicker(interval)

	go func() {
		defer ticker.Stop()
		m.logger.Debug("background health check routine started")
		// Perform an initial check immediately
		m.checkAllSourcesHealth()

		for {
			select {
			case <-ticker.C:
				m.logger.Debug("performing periodic health check")
				m.checkAllSourcesHealth()
			case <-m.stopHealth:
				m.logger.Info("stopping background health checks")
				return
			}
		}
	}()
}

// StopBackgroundHealthChecks signals the health check goroutine to stop.
func (m *Manager) StopBackgroundHealthChecks() {
	m.logger.Info("signaling background health checks to stop")
	close(m.stopHealth)
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

	var wg sync.WaitGroup
	for _, id := range idsToCheck {
		wg.Add(1)
		go func(sourceID models.SourceID) {
			defer wg.Done()
			m.performSingleSourceCheck(sourceID)
		}(id)
	}
	wg.Wait()
	m.logger.Debug("finished checking health for all sources", "count", len(idsToCheck))
}

// performSingleSourceCheck checks a single source and updates the health map.
func (m *Manager) performSingleSourceCheck(sourceID models.SourceID) {
	// Get the client connection. GetConnection handles locking internally.
	client, err := m.GetConnection(sourceID)

	currentHealth := models.SourceHealth{
		SourceID:    sourceID,
		LastChecked: time.Now(),
	}

	if err != nil { // Error getting client (e.g., removed during check)
		currentHealth.Status = models.HealthStatusUnhealthy
		currentHealth.Error = fmt.Sprintf("failed to get client for health check: %v", err)
		// Do not update health map if client doesn't exist
		m.logger.Warn("client not found during health check", "source_id", sourceID)
		return
	}

	// Perform the actual health check (Ping).
	ctx, cancel := context.WithTimeout(context.Background(), HealthCheckTimeout)
	defer cancel()

	err = client.Ping(ctx) // Use the Ping method
	if err != nil {
		currentHealth.Status = models.HealthStatusUnhealthy
		currentHealth.Error = err.Error()
	} else {
		currentHealth.Status = models.HealthStatusHealthy
	}

	// Update the health map.
	m.healthMux.Lock()
	m.health[sourceID] = currentHealth
	m.healthMux.Unlock()
}

// GetCachedHealth retrieves the latest known health status for a source ID from the cache.
// Returns a default unhealthy status if the source hasn't been checked yet.
func (m *Manager) GetCachedHealth(sourceID models.SourceID) models.SourceHealth {
	m.healthMux.RLock()
	health, ok := m.health[sourceID]
	m.healthMux.RUnlock()

	if !ok {
		// Return a default status if not found (e.g., source just added, first check pending)
		return models.SourceHealth{
			SourceID:    sourceID,
			Status:      models.HealthStatusUnhealthy, // Use Unhealthy as default when status is unknown
			LastChecked: time.Time{},                  // Zero time indicates never checked
			Error:       "source health not yet checked",
		}
	}
	return health
}

// AddSource creates a new ClickHouse client connection based on the source details,
// applies existing hooks, stores it in the manager pool, and initializes health.
func (m *Manager) AddSource(source *models.Source) error {
	m.clientsMux.Lock() // Lock clients map for writing.
	defer m.clientsMux.Unlock()
	m.healthMux.Lock() // Lock health map for writing.
	defer m.healthMux.Unlock()

	m.logger.Info("adding source to manager",
		"source_id", source.ID,
		"database", source.Connection.Database,
		"table", source.Connection.TableName,
	)

	// Check if client already exists to prevent overwriting.
	if _, exists := m.clients[source.ID]; exists {
		m.logger.Warn("source already exists in manager, skipping add", "source_id", source.ID)
		// Ensure health status exists, potentially trigger an immediate check?
		if _, healthExists := m.health[source.ID]; !healthExists {
			// Initialize with Unhealthy if somehow missing
			m.health[source.ID] = models.SourceHealth{SourceID: source.ID, Status: models.HealthStatusUnhealthy}
		}
		return nil // Not an error, already managed.
	}

	// Create new client.
	client, err := NewClient(ClientOptions{
		Host:     source.Connection.Host,
		Database: source.Connection.Database,
		Username: source.Connection.Username,
		Password: source.Connection.Password,
	}, m.logger)
	if err != nil {
		// If client creation fails, store unhealthy status
		m.health[source.ID] = models.SourceHealth{
			SourceID:    source.ID,
			Status:      models.HealthStatusUnhealthy,
			LastChecked: time.Now(),
			Error:       fmt.Sprintf("failed to create client: %v", err),
		}
		return fmt.Errorf("creating client: %w", err)
	}

	// Apply any existing hooks to the newly created client.
	for _, hook := range m.hooks {
		client.AddQueryHook(hook)
	}

	// Store the client.
	m.clients[source.ID] = client

	// Initialize health status as Unhealthy - background check will update it.
	m.health[source.ID] = models.SourceHealth{
		SourceID:    source.ID,
		Status:      models.HealthStatusUnhealthy, // Default to Unhealthy until first check passes
		LastChecked: time.Time{},                  // Zero time indicates initial state
	}

	// Optional: Trigger an immediate check for the newly added source?
	// go m.performSingleSourceCheck(source.ID) // Run in background

	return nil
}

// RemoveSource closes the connection for the given source ID and removes it from the manager.
func (m *Manager) RemoveSource(sourceID models.SourceID) error {
	m.logger.Info("removing source from manager", "source_id", sourceID)

	m.clientsMux.Lock()
	client, exists := m.clients[sourceID]
	delete(m.clients, sourceID) // Remove from map regardless of close success.
	m.clientsMux.Unlock()

	m.healthMux.Lock()
	delete(m.health, sourceID) // Remove health status.
	m.healthMux.Unlock()

	if exists && client != nil {
		if err := client.Close(); err != nil {
			m.logger.Error("error closing client during removal",
				"source_id", sourceID,
				"error", err,
			)
			// Return the close error if needed, otherwise just log.
			// return err
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

// GetClient is an alias for GetConnection for potential backward compatibility.
func (m *Manager) GetClient(sourceID models.SourceID) (*Client, error) {
	return m.GetConnection(sourceID)
}

// GetHealth performs a LIVE health check on a specific source and updates the cache.
// Deprecated: Use GetCachedHealth for regular status checks.
// Use this only if an immediate, live check is explicitly required.
func (m *Manager) GetHealth(sourceID models.SourceID) models.SourceHealth {
	m.logger.Warn("GetHealth called (performs live check), consider GetCachedHealth instead", "source_id", sourceID)
	m.performSingleSourceCheck(sourceID)
	return m.GetCachedHealth(sourceID)
}

// Close iterates through all managed client connections and closes them.
// It also stops the background health checker.
func (m *Manager) Close() error {
	m.logger.Info("closing clickhouse manager")
	// Stop health checks first
	m.StopBackgroundHealthChecks()

	m.clientsMux.Lock()
	defer m.clientsMux.Unlock()

	var lastErr error
	for id, client := range m.clients {
		if err := client.Close(); err != nil {
			m.logger.Error("error closing client",
				"source_id", id,
				"error", err,
			)
			lastErr = err // Keep track of the last error.
		}
	}
	m.clients = make(map[models.SourceID]*Client) // Reset map after closing all.

	m.healthMux.Lock()
	m.health = make(map[models.SourceID]models.SourceHealth) // Clear health map.
	m.healthMux.Unlock()

	return lastErr
}

// CreateTemporaryClient creates a new, unmanaged ClickHouse client instance,
// typically used for validating connection details before adding a source.
// The caller is responsible for closing the returned client.
func (m *Manager) CreateTemporaryClient(source *models.Source) (*Client, error) {
	m.logger.Info("creating temporary client for validation",
		"host", source.Connection.Host,
		"database", source.Connection.Database,
	)

	// Create new client with a specific logger attribute for validation context.
	client, err := NewClient(ClientOptions{
		Host:     source.Connection.Host,
		Database: source.Connection.Database,
		Username: source.Connection.Username,
		Password: source.Connection.Password,
	}, m.logger.With("validation", true))

	if err != nil {
		m.logger.Error("failed to create temporary client", "error", err)
		return nil, fmt.Errorf("error creating temporary client: %w", err)
	}

	return client, nil
}

// AddQueryHook adds a query hook to the manager's list.
// The hook will be applied to all currently managed clients and any
// subsequently added clients via AddSource.
func (m *Manager) AddQueryHook(hook QueryHook) {
	m.clientsMux.Lock() // Lock for both hooks slice and iterating clients map.
	defer m.clientsMux.Unlock()

	// Store the hook for future clients.
	m.hooks = append(m.hooks, hook)

	// Add hook to all existing clients.
	for _, client := range m.clients {
		client.AddQueryHook(hook)
	}

	m.logger.Info("added query hook to all clients", "hook_type", fmt.Sprintf("%T", hook))
}
