package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// Default values
const (
	DefaultQueryLimit          = 100
	HealthCheckTimeout         = 1 * time.Second // Reduce to 1 second for faster health checks
	DefaultHealthCheckInterval = 30 * time.Second
)

// Manager handles pooling and management of multiple ClickHouse client connections,
// one per data source. It also manages query hooks and background health checks.
type Manager struct {
	clients    map[models.SourceID]*Client
	clientsMux sync.RWMutex // Protects the clients map.
	logger     *slog.Logger
	health     map[models.SourceID]models.SourceHealth
	healthMux  sync.RWMutex   // Protects the health map.
	hooks      []QueryHook    // Hooks applied to all managed clients.
	stopHealth chan struct{}  // Channel to signal health check goroutine to stop.
	healthWG   sync.WaitGroup // WaitGroup to wait for health check goroutine to exit.
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

	// Add to wait group before starting the goroutine
	m.healthWG.Add(1)

	go func() {
		defer ticker.Stop()
		defer m.healthWG.Done() // Signal completion when goroutine exits
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
			m.checkSource(sourceID)
		}(id)
	}
	wg.Wait()
	m.logger.Debug("finished checking health for all sources", "count", len(idsToCheck))
}

// updateHealthStatus is a helper method to update the health status of a source.
func (m *Manager) updateHealthStatus(sourceID models.SourceID, isHealthy bool, errorMsg string) {
	m.healthMux.Lock()
	defer m.healthMux.Unlock()

	status := models.HealthStatusUnhealthy
	if isHealthy {
		status = models.HealthStatusHealthy
	}

	m.health[sourceID] = models.SourceHealth{
		SourceID:    sourceID,
		Status:      status,
		LastChecked: time.Now(),
		Error:       errorMsg,
	}
}

// checkSource checks a single source and updates the health map.
// It attempts to reconnect if the connection is unhealthy.
// This function now respects timeouts better and avoids blocking for too long.
func (m *Manager) checkSource(sourceID models.SourceID) {
	start := time.Now()
	defer func() {
		m.logger.Debug("health check completed",
			"source_id", sourceID,
			"duration_ms", time.Since(start).Milliseconds())
	}()

	// Get the client connection. GetConnection handles locking internally.
	client, err := m.GetConnection(sourceID)

	if err != nil { // Error getting client (e.g., removed during check)
		m.logger.Warn("client not found during health check", "source_id", sourceID)
		m.updateHealthStatus(sourceID, false, fmt.Sprintf("failed to get client for health check: %v", err))
		return
	}

	// Create parent context with overall timeout for the check operation
	rootCtx, rootCancel := context.WithTimeout(context.Background(), HealthCheckTimeout*2) // e.g., 2 seconds total
	defer rootCancel()

	// 1. Perform the actual health check (Ping) with its own timeout.
	pingCtx, pingCancel := context.WithTimeout(rootCtx, HealthCheckTimeout) // e.g., 1 second for ping
	pingErr := client.Ping(pingCtx, "", "")
	pingCancel() // Cancel the ping context immediately after the call.

	// Handle the ping result
	if pingErr != nil {
		// Check if the error was specifically a timeout
		if errors.Is(pingErr, context.DeadlineExceeded) {
			m.logger.Warn("ping timed out during health check",
				"source_id", sourceID,
				"timeout", HealthCheckTimeout)
		} else {
			m.logger.Warn("ping failed during health check, attempting reconnect",
				"source_id", sourceID,
				"error", pingErr)
		}

		// 2. Attempt to reconnect if ping failed, using remaining time from rootCtx.
		reconnectCtx, reconnectCancel := context.WithTimeout(rootCtx, HealthCheckTimeout) // Give reconnect its own timeout budget
		reconnectErr := client.Reconnect(reconnectCtx)
		reconnectCancel() // Cancel reconnect context.

		if reconnectErr != nil {
			// Check if reconnect failed due to timeout
			if errors.Is(reconnectErr, context.DeadlineExceeded) {
				m.logger.Error("reconnection attempt timed out",
					"source_id", sourceID,
					"timeout", HealthCheckTimeout)
				reconnectErr = fmt.Errorf("reconnection timed out after %v: %w", HealthCheckTimeout, reconnectErr)
			} else {
				m.logger.Error("reconnection attempt failed",
					"source_id", sourceID,
					"error", reconnectErr)
			}
			m.updateHealthStatus(sourceID, false, fmt.Sprintf("reconnection failed: %v", reconnectErr))
		} else {
			// Reconnection successful
			m.logger.Info("successfully reconnected to source", "source_id", sourceID)
			m.updateHealthStatus(sourceID, true, "")
		}
	} else {
		// Connection is healthy after ping
		m.updateHealthStatus(sourceID, true, "")
	}
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
// Modified to always create a client entry even if initial connection fails.
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

	// Create new client without initial ping validation
	client, err := NewClient(ClientOptions{
		Host:     source.Connection.Host,
		Database: source.Connection.Database,
		Username: source.Connection.Username,
		Password: source.Connection.Password,
		SourceID: strconv.FormatInt(int64(source.ID), 10), // Convert SourceID to string for metrics
		Source:   source,                                  // Pass source for enhanced metrics
	}, m.logger)

	if err != nil {
		// If client creation fails completely (not just connection), log and return error
		m.logger.Error("failed to create client instance",
			"source_id", source.ID,
			"error", err)
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

	// Store the client regardless of connection status
	m.clients[source.ID] = client

	// Initialize health status as Unhealthy - background check will update it.
	m.health[source.ID] = models.SourceHealth{
		SourceID:    source.ID,
		Status:      models.HealthStatusUnhealthy, // Default to Unhealthy until first check passes
		LastChecked: time.Now(),                   // Set current time to indicate we've attempted
		Error:       "Initial connection pending",
	}

	// Trigger an immediate check for the newly added source in the background
	go m.checkSource(source.ID)

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
	m.checkSource(sourceID)
	return m.GetCachedHealth(sourceID)
}

// Close iterates through all managed client connections and closes them,
// with a timeout for each client to prevent hanging on unhealthy connections.
// It also stops the background health checker and waits for it to complete.
func (m *Manager) Close() error {
	m.logger.Info("closing clickhouse manager")

	// Stop health checks first and wait for the goroutine to exit
	m.StopBackgroundHealthChecks()

	// Wait for the health check goroutine to fully terminate
	waitChan := make(chan struct{})
	go func() {
		m.logger.Info("waiting for health check goroutine to exit")
		m.healthWG.Wait()
		close(waitChan)
	}()

	// Use a timeout to avoid hanging forever if something goes wrong
	select {
	case <-waitChan:
		m.logger.Info("health check goroutine exited successfully")
	case <-time.After(5 * time.Second):
		m.logger.Warn("timed out waiting for health check goroutine to exit, continuing with shutdown")
	}

	m.clientsMux.Lock()
	defer m.clientsMux.Unlock()

	// Close all clients with timeouts and in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex
	var lastErr error

	// Get all the source IDs first
	clientIDs := make([]models.SourceID, 0, len(m.clients))
	for id := range m.clients {
		clientIDs = append(clientIDs, id)
	}

	for _, id := range clientIDs {
		client := m.clients[id]
		wg.Add(1)

		// Close each client in a separate goroutine to allow parallel shutdown
		go func(sourceID models.SourceID, cl *Client) {
			defer wg.Done()

			// Use a timeout context for each client close operation
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer closeCancel()

			// Create a done channel to signal completion
			done := make(chan error, 1)

			go func() {
				// Each client.Close() already has its own timeout
				done <- cl.Close()
			}()

			// Wait for client to close or timeout
			select {
			case err := <-done:
				if err != nil {
					mu.Lock()
					m.logger.Error("error closing client", "source_id", sourceID, "error", err)
					lastErr = err // Keep track of the last error
					mu.Unlock()
				}
			case <-closeCtx.Done():
				mu.Lock()
				m.logger.Warn("timeout closing client", "source_id", sourceID)
				mu.Unlock()
			}
		}(id, client)
	}

	// Wait for all clients to be closed or timeout
	closeDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(closeDone)
	}()

	// Add an overall timeout for client closing phase
	select {
	case <-closeDone:
		m.logger.Info("all clients closed successfully")
	case <-time.After(8 * time.Second): // Overall timeout slightly longer than individual timeouts
		m.logger.Warn("timeout waiting for all clients to close, continuing shutdown")
	}

	// Clean up maps
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

	// Perform a basic ping to verify the connection without checking table existence.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Ping with empty database and table to only check basic connectivity.
	if err := client.Ping(ctx, "", ""); err != nil {
		// Attempt to close the potentially problematic connection before returning error.
		_ = client.Close() // Ignore error during cleanup
		// Wrap the error with context indicating basic connection failure.
		return nil, fmt.Errorf("basic connection ping failed: %w", err)
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
