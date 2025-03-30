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

// Manager manages multiple ClickHouse connections
type Manager struct {
	clients    map[models.SourceID]*Client
	clientsMux sync.RWMutex
	logger     *slog.Logger
	health     map[models.SourceID]models.SourceHealth
	mu         sync.RWMutex
	hooks      []QueryHook // Store hooks to apply to new clients
}

// NewManager creates a new connection manager
func NewManager(log *slog.Logger) *Manager {
	return &Manager{
		clients: make(map[models.SourceID]*Client),
		logger: log.With("component", "clickhouse_manager"),
		health: make(map[models.SourceID]models.SourceHealth),
		hooks:  []QueryHook{}, // Initialize empty, hooks will be added below
	}

	// Add default hooks
	m.AddQueryHook(NewLogQueryHook(log, false)) // Keep the basic error/completion logger
	m.AddQueryHook(NewStructuredQueryLoggerHook(log)) // Add the new structured logger

	return m
}

// AddSource creates and stores a new connection
func (m *Manager) AddSource(source *models.Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("adding source to manager",
		"source_id", source.ID,
		"database", source.Connection.Database,
		"table", source.Connection.TableName,
	)

	// Create new client
	client, err := NewClient(ClientOptions{
		Host:     source.Connection.Host,
		Database: source.Connection.Database,
		Username: source.Connection.Username,
		Password: source.Connection.Password,
	}, m.logger)
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	// Apply any existing hooks to the new client
	m.clientsMux.Lock()
	for _, hook := range m.hooks {
		client.AddQueryHook(hook)
	}
	m.clientsMux.Unlock()

	// Store the client
	m.clientsMux.Lock()
	m.clients[source.ID] = client
	m.clientsMux.Unlock()

	// Initialize health status
	m.health[source.ID] = models.SourceHealth{
		SourceID:    source.ID,
		Status:      models.HealthStatusHealthy,
		LastChecked: time.Now(),
	}

	return nil
}

// RemoveSource removes a source connection
func (m *Manager) RemoveSource(sourceID models.SourceID) error {
	m.logger.Info("removing source from manager", "source_id", sourceID)

	m.clientsMux.Lock()
	defer m.clientsMux.Unlock()

	if client, exists := m.clients[sourceID]; exists {
		if err := client.Close(); err != nil {
			m.logger.Error("error closing client",
				"source_id", sourceID,
				"error", err,
			)
		}
		delete(m.clients, sourceID)
	}

	return nil
}

// GetConnection returns a connection for immediate use
func (m *Manager) GetConnection(sourceID models.SourceID) (*Client, error) {
	m.clientsMux.RLock()
	defer m.clientsMux.RUnlock()

	client, ok := m.clients[sourceID]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrSourceNotConnected, sourceID)
	}

	return client, nil
}

// GetClient returns a client for a given source ID (alias for GetConnection for backward compatibility)
func (m *Manager) GetClient(sourceID models.SourceID) (*Client, error) {
	return m.GetConnection(sourceID)
}

// QueryLogs executes a log query against a specific source
func (m *Manager) QueryLogs(ctx context.Context, sourceID models.SourceID, source *models.Source, params LogQueryParams) (*models.QueryResult, error) {
	// Get client
	client, err := m.GetClient(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting client for source %d: %w", sourceID, err)
	}

	// Validate query parameters
	if params.RawSQL == "" {
		return nil, ErrInvalidQuery
	}

	if params.Limit <= 0 {
		params.Limit = DefaultQueryLimit
	}

	// Get table name from source
	tableName := fmt.Sprintf("%s.%s", source.Connection.Database, source.Connection.TableName)

	// Build the query using QueryBuilder
	qb := NewQueryBuilder(tableName)
	query, err := qb.BuildRawQuery(params.RawSQL, params.Limit)
	if err != nil {
		return nil, fmt.Errorf("building query: %w", err)
	}

	// Execute the query
	return client.Query(ctx, query)
}

// GetHealth returns the health status for a given source
func (m *Manager) GetHealth(sourceID models.SourceID) models.SourceHealth {
	health := models.SourceHealth{
		SourceID:    sourceID,
		LastChecked: time.Now(),
	}

	client, err := m.GetConnection(sourceID)
	if err != nil {
		health.Status = models.HealthStatusUnhealthy
		health.Error = err.Error()
		return health
	}

	ctx, cancel := context.WithTimeout(context.Background(), HealthCheckTimeout)
	defer cancel()

	err = client.CheckHealth(ctx)
	if err != nil {
		health.Status = models.HealthStatusUnhealthy
		health.Error = err.Error()
		return health
	}

	health.Status = models.HealthStatusHealthy
	return health
}

// Close closes all connections
func (m *Manager) Close() error {
	m.logger.Info("closing all connections")

	m.clientsMux.Lock()
	defer m.clientsMux.Unlock()

	var lastErr error
	for id, client := range m.clients {
		if err := client.Close(); err != nil {
			m.logger.Error("error closing client",
				"source_id", id,
				"error", err,
			)
			lastErr = err
		}
	}

	return lastErr
}

// CreateTemporaryClient creates a temporary client for connection validation
// This client is not stored in the manager and should be closed by the caller
func (m *Manager) CreateTemporaryClient(source *models.Source) (*Client, error) {
	m.logger.Info("creating temporary client for validation",
		"host", source.Connection.Host,
		"database", source.Connection.Database,
	)

	// Create new client
	client, err := NewClient(ClientOptions{
		Host:     source.Connection.Host,
		Database: source.Connection.Database,
		Username: source.Connection.Username,
		Password: source.Connection.Password,
	}, m.logger.With("validation", true))

	if err != nil {
		m.logger.Error("failed to create temporary client", "error", err)
		return nil, fmt.Errorf("error creating client: %w", err)
	}

	return client, nil
}

// AddQueryHook adds a query hook to all clients
func (m *Manager) AddQueryHook(hook QueryHook) {
	m.clientsMux.Lock()
	defer m.clientsMux.Unlock()

	// Store the hook for future clients
	m.hooks = append(m.hooks, hook)

	// Add hook to all existing clients
	for _, client := range m.clients {
		client.AddQueryHook(hook)
	}

	m.logger.Info("added query hook to all clients", "hook_type", fmt.Sprintf("%T", hook))
}
