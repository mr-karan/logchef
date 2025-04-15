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
	DefaultQueryLimit  = 100
	HealthCheckTimeout = 5 * time.Second
)

// Manager handles pooling and management of multiple ClickHouse client connections,
// one per data source. It also manages query hooks applied to clients.
type Manager struct {
	clients    map[models.SourceID]*Client
	clientsMux sync.RWMutex // Protects the clients map.
	logger     *slog.Logger
	health     map[models.SourceID]models.SourceHealth
	mu         sync.RWMutex // Protects the health map.
	hooks      []QueryHook  // Hooks applied to all managed clients.
}

// NewManager creates a new ClickHouse connection manager.
func NewManager(log *slog.Logger) *Manager {
	m := &Manager{
		clients: make(map[models.SourceID]*Client),
		logger:  log.With("component", "clickhouse_manager"),
		health:  make(map[models.SourceID]models.SourceHealth),
		hooks:   []QueryHook{}, // Initialize empty slice.
	}

	// Apply default hooks for basic logging.
	m.AddQueryHook(NewLogQueryHook(log, false))
	m.AddQueryHook(NewStructuredQueryLoggerHook(log))

	return m
}

// AddSource creates a new ClickHouse client connection based on the source details,
// applies existing hooks, and stores it in the manager pool.
func (m *Manager) AddSource(source *models.Source) error {
	m.clientsMux.Lock() // Lock clients map for writing.
	defer m.clientsMux.Unlock()
	m.mu.Lock() // Lock health map for writing.
	defer m.mu.Unlock()

	m.logger.Info("adding source to manager",
		"source_id", source.ID,
		"database", source.Connection.Database,
		"table", source.Connection.TableName,
	)

	// Check if client already exists to prevent overwriting.
	if _, exists := m.clients[source.ID]; exists {
		m.logger.Warn("source already exists in manager, skipping add", "source_id", source.ID)
		// Optionally update health status here if needed.
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
		return fmt.Errorf("creating client: %w", err)
	}

	// Apply any existing hooks to the newly created client.
	for _, hook := range m.hooks {
		client.AddQueryHook(hook)
	}

	// Store the client.
	m.clients[source.ID] = client

	// Initialize health status.
	m.health[source.ID] = models.SourceHealth{
		SourceID:    source.ID,
		Status:      models.HealthStatusHealthy, // Assume healthy on add; health check verifies later.
		LastChecked: time.Now(),
	}

	return nil
}

// RemoveSource closes the connection for the given source ID and removes it from the manager.
func (m *Manager) RemoveSource(sourceID models.SourceID) error {
	m.logger.Info("removing source from manager", "source_id", sourceID)

	m.clientsMux.Lock()
	client, exists := m.clients[sourceID]
	delete(m.clients, sourceID) // Remove from map regardless of close success.
	m.clientsMux.Unlock()

	m.mu.Lock()
	delete(m.health, sourceID) // Remove health status.
	m.mu.Unlock()

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

// GetHealth checks the connectivity of a specific source and returns its health status.
func (m *Manager) GetHealth(sourceID models.SourceID) models.SourceHealth {
	m.mu.Lock() // Lock health map for update.
	defer m.mu.Unlock()

	health := models.SourceHealth{
		SourceID:    sourceID,
		LastChecked: time.Now(), // Update check time regardless of outcome.
	}

	client, err := m.GetConnection(sourceID) // GetConnection uses RLock on clientsMux.
	if err != nil {
		health.Status = models.HealthStatusUnhealthy
		health.Error = err.Error()
		m.health[sourceID] = health // Store updated unhealthy status.
		return health
	}

	// Perform the actual health check.
	ctx, cancel := context.WithTimeout(context.Background(), HealthCheckTimeout)
	defer cancel()

	err = client.CheckHealth(ctx)
	if err != nil {
		health.Status = models.HealthStatusUnhealthy
		health.Error = err.Error()
	} else {
		health.Status = models.HealthStatusHealthy
	}

	m.health[sourceID] = health // Store updated health status.
	return health
}

// Close iterates through all managed client connections and closes them.
// It returns the last error encountered during closing, if any.
func (m *Manager) Close() error {
	m.logger.Info("closing all managed clickhouse connections")

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
		// Remove closed client from map? Or rely on RemoveSource?
		// delete(m.clients, id) // Optional: Clear map after closing.
	}
	m.clients = make(map[models.SourceID]*Client) // Reset map after closing all.

	m.mu.Lock()
	m.health = make(map[models.SourceID]models.SourceHealth) // Clear health map.
	m.mu.Unlock()

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
