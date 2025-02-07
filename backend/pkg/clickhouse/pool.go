package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"backend-v2/pkg/logger"
	"backend-v2/pkg/models"
)

// Pool manages a pool of ClickHouse HTTP clients
type Pool struct {
	mu      sync.RWMutex
	clients map[string]*HTTPClient
	log     *slog.Logger
}

// NewPool creates a new connection pool
func NewPool() *Pool {
	return &Pool{
		clients: make(map[string]*HTTPClient),
		log:     logger.Default().With("component", "clickhouse_pool"),
	}
}

// AddSource adds or updates a source in the pool
func (p *Pool) AddSource(source *models.Source) error {
	p.log.Info("adding source to pool",
		"source_id", source.ID,
		"database", source.Connection.Database,
		"table", source.Connection.TableName,
	)

	// Create new HTTP client
	client := NewHTTPClient(
		source.Connection.Host,
		DefaultHTTPSettings(),
		p.log.With("source_id", source.ID),
	)

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
func (p *Pool) GetClient(sourceID string) (*HTTPClient, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	client, exists := p.clients[sourceID]
	if !exists {
		return nil, fmt.Errorf("no client found for source %s", sourceID)
	}

	return client, nil
}

// QueryLogs queries logs from a source
func (p *Pool) QueryLogs(ctx context.Context, sourceID string, params LogQueryParams) (*LogQueryResult, error) {
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
	var extraParams map[string]string

	switch params.Mode {
	case models.QueryModeRawSQL:
		if params.RawSQL == "" {
			return nil, fmt.Errorf("raw SQL query cannot be empty")
		}
		query = params.RawSQL
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
	resp, err := client.Query(ctx, query, extraParams)
	if err != nil {
		return nil, fmt.Errorf("error executing query: %w", err)
	}

	// Convert response to LogQueryResult
	return &LogQueryResult{
		Data:    ConvertToMap(resp),
		Stats:   GetQueryStats(resp),
		Columns: GetColumns(resp),
	}, nil
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
	// TODO: Implement getting source from database
	// For now, return a source with the correct table name
	return &models.Source{
		ID: sourceID,
		Connection: models.ConnectionInfo{
			Database:  "logs",
			TableName: "vector_logs",
		},
	}, nil
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
