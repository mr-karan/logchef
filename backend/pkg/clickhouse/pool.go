package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"backend-v2/internal/sqlite"
	"backend-v2/pkg/logger"
	"backend-v2/pkg/models"
	"backend-v2/pkg/querybuilder"
)

const (
	// Health check constants
	healthCheckInterval = 30 * time.Second
	healthCheckTimeout  = 10 * time.Second
	healthCheckQuery    = "SELECT 1"
)

// Pool manages a pool of ClickHouse clients
type Pool struct {
	mu      sync.RWMutex
	clients map[string]*Client
	log     *slog.Logger
	sqlite  *sqlite.DB
}

// NewPool creates a new connection pool
func NewPool(sqliteDB *sqlite.DB) *Pool {
	return &Pool{
		clients: make(map[string]*Client),
		log:     logger.Default().With("component", "clickhouse_pool"),
		sqlite:  sqliteDB,
	}
}

// AddSource adds or updates a source in the pool
func (p *Pool) AddSource(source *models.Source) error {
	p.log.Info("adding source to pool",
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
	}, p.log.With("source_id", source.ID))
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Close existing client if it exists
	if oldClient, exists := p.clients[source.ID]; exists {
		p.log.Info("closing existing client",
			"source_id", source.ID,
		)
		_ = oldClient.Close()
	}

	p.clients[source.ID] = client
	return nil
}

// RemoveSource removes a source from the pool
func (p *Pool) RemoveSource(sourceID string) error {
	p.log.Info("removing source from pool",
		"source_id", sourceID,
	)

	p.mu.Lock()
	defer p.mu.Unlock()

	if client, exists := p.clients[sourceID]; exists {
		if err := client.Close(); err != nil {
			p.log.Error("error closing client",
				"source_id", sourceID,
				"error", err,
			)
		}
		delete(p.clients, sourceID)
	}

	return nil
}

// GetClient returns a client for a given source ID
func (p *Pool) GetClient(sourceID string) (*Client, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	client, exists := p.clients[sourceID]
	if !exists {
		return nil, fmt.Errorf("no client found for source %s", sourceID)
	}

	return client, nil
}

// QueryLogs queries logs from a source
func (p *Pool) QueryLogs(ctx context.Context, sourceID string, params LogQueryParams) (*models.QueryResult, error) {
	// Get source from the database
	source, err := p.getSource(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}

	// Get client
	client, err := p.GetClient(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting client: %w", err)
	}

	// Build query based on mode
	var query string
	switch params.Mode {
	case models.QueryModeRawSQL:
		if params.RawSQL == "" {
			return nil, fmt.Errorf("raw SQL query cannot be empty")
		}
		// Use the query builder to handle the raw SQL
		builder := querybuilder.NewRawSQLBuilder(source.GetFullTableName(), params.RawSQL, params.Limit)
		builtQuery, err := builder.Build()
		if err != nil {
			return nil, fmt.Errorf("error building raw SQL query: %w", err)
		}
		query = builtQuery.SQL
	case models.QueryModeLogChefQL:
		if params.LogChefQL == "" {
			return nil, fmt.Errorf("LogchefQL query cannot be empty")
		}
		// TODO: Implement LogChefQL parsing
		return nil, fmt.Errorf("LogChefQL mode not implemented")
	case models.QueryModeFilters:
		// Build filter query
		query = buildFilterQuery(params, source.GetFullTableName())
	default:
		return nil, fmt.Errorf("unsupported query mode: %s", params.Mode)
	}

	// Execute query
	return client.Query(ctx, query)
}

// getSource gets a source from the database
func (p *Pool) getSource(sourceID string) (*models.Source, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Get source from the pool's clients map
	_, exists := p.clients[sourceID]
	if !exists {
		return nil, fmt.Errorf("no client found for source %s", sourceID)
	}

	// Get source from the database
	source, err := p.sqlite.GetSource(context.Background(), sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source from database: %w", err)
	}
	if source == nil {
		return nil, fmt.Errorf("source %s not found in database", sourceID)
	}

	return source, nil
}

// GetHealth returns the health status for a given source ID
func (p *Pool) GetHealth(sourceID string) (models.SourceHealth, error) {
	client, err := p.GetClient(sourceID)
	if err != nil {
		return models.SourceHealth{
			SourceID: sourceID,
			Status:   models.HealthStatusUnhealthy,
			Error:    err.Error(),
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer cancel()

	err = client.CheckHealth(ctx)
	if err != nil {
		return models.SourceHealth{
			SourceID:    sourceID,
			Status:      models.HealthStatusUnhealthy,
			Error:       err.Error(),
			LastChecked: time.Now(),
		}, nil
	}

	return models.SourceHealth{
		SourceID:    sourceID,
		Status:      models.HealthStatusHealthy,
		LastChecked: time.Now(),
	}, nil
}

// Close closes all clients in the pool
func (p *Pool) Close() error {
	p.log.Info("closing all clients in pool")

	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for id, client := range p.clients {
		if err := client.Close(); err != nil {
			p.log.Error("error closing client",
				"source_id", id,
				"error", err,
			)
			lastErr = err
		}
	}

	return lastErr
}
