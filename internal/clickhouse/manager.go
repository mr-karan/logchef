package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"logchef/pkg/logger"
	"logchef/pkg/models"
)

// Default values
const (
	DefaultQueryLimit  = 100
	HealthCheckTimeout = 5 * time.Second
)

// Manager manages multiple ClickHouse connections
type Manager struct {
	clients    map[string]*Client
	clientsMux sync.RWMutex
	logger     *slog.Logger
}

// NewManager creates a new connection manager
func NewManager(log *slog.Logger) *Manager {
	if log == nil {
		log = logger.NewLogger("clickhouse")
	}

	return &Manager{
		clients: make(map[string]*Client),
		logger:  log.With("component", "clickhouse_manager"),
	}
}

// AddSource creates and stores a new connection
func (m *Manager) AddSource(source *models.Source) error {
	if source == nil {
		return errors.New("source cannot be nil")
	}

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
	}, m.logger.With("source_id", source.ID))

	if err != nil {
		return fmt.Errorf("creating client for source %s: %w", source.ID, err)
	}

	m.clientsMux.Lock()
	defer m.clientsMux.Unlock()

	// Close existing client if it exists
	if oldClient, exists := m.clients[source.ID]; exists {
		m.logger.Info("closing existing client", "source_id", source.ID)
		_ = oldClient.Close()
	}

	m.clients[source.ID] = client
	return nil
}

// RemoveSource removes a source connection
func (m *Manager) RemoveSource(sourceID string) error {
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
func (m *Manager) GetConnection(sourceID string) (*Client, error) {
	m.clientsMux.RLock()
	defer m.clientsMux.RUnlock()

	client, ok := m.clients[sourceID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSourceNotConnected, sourceID)
	}

	return client, nil
}

// GetClient returns a client for a given source ID (alias for GetConnection for backward compatibility)
func (m *Manager) GetClient(sourceID string) (*Client, error) {
	return m.GetConnection(sourceID)
}

// QueryLogs executes a log query against a specific source
func (m *Manager) QueryLogs(ctx context.Context, sourceID string, source *models.Source, params LogQueryParams) (*models.QueryResult, error) {
	// Get client
	client, err := m.GetClient(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting client: %w", err)
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
func (m *Manager) GetHealth(sourceID string) models.SourceHealth {
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
