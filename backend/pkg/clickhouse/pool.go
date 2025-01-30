package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"backend-v2/pkg/logger"
	"backend-v2/pkg/models"
)

// Pool manages a pool of Clickhouse connections
type Pool struct {
	mu      sync.RWMutex
	health  *HealthChecker
	connMap map[string]*Connection
	log     *slog.Logger
}

// NewPool creates a new connection pool
func NewPool() *Pool {
	return &Pool{
		health:  NewHealthChecker(),
		connMap: make(map[string]*Connection),
		log:     logger.Default().With("component", "clickhouse_pool"),
	}
}

// AddSource adds or updates a source in the pool
func (p *Pool) AddSource(source *models.Source) error {
	p.log.Info("adding source to pool",
		"source_id", source.ID,
		"database", source.Connection.Database,
		"table", source.Connection.TableName,
		"schema_type", source.SchemaType,
	)

	// Create new connection using the existing NewConnection function
	conn, err := NewConnection(source)
	if err != nil {
		return fmt.Errorf("error creating connection: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Close existing connection if it exists
	if oldConn, exists := p.connMap[source.ID]; exists {
		p.log.Info("closing existing connection",
			"source_id", source.ID,
		)
		_ = oldConn.Close()
	}

	p.connMap[source.ID] = conn
	p.health.AddConnection(conn)

	return nil
}

// RemoveSource removes a source from the pool
func (p *Pool) RemoveSource(sourceID string) error {
	p.log.Info("removing source from pool",
		"source_id", sourceID,
	)

	p.mu.Lock()
	defer p.mu.Unlock()

	if conn, exists := p.connMap[sourceID]; exists {
		if err := conn.Close(); err != nil {
			p.log.Error("error closing connection",
				"source_id", sourceID,
				"error", err,
			)
		}
		delete(p.connMap, sourceID)
		p.health.RemoveConnection(sourceID)
	}

	return nil
}

// GetConnection returns a connection for a given source ID
func (p *Pool) GetConnection(sourceID string) (*Connection, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	conn, exists := p.connMap[sourceID]
	if !exists {
		return nil, fmt.Errorf("no connection found for source %s", sourceID)
	}

	return conn, nil
}

// DescribeTable retrieves the schema information for a source's table
func (p *Pool) DescribeTable(ctx context.Context, source *models.Source) ([]models.ColumnInfo, error) {
	p.log.Debug("describing table",
		"source_id", source.ID,
		"database", source.Connection.Database,
		"table", source.Connection.TableName,
		"schema_type", source.SchemaType,
	)

	conn, err := p.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting connection: %w", err)
	}

	// Get root columns from system.columns
	query := fmt.Sprintf("SELECT name, type FROM system.columns WHERE database = '%s' AND table = '%s'",
		source.Connection.Database, source.Connection.TableName)
	rows, err := conn.DB.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("error describing table: %w", err)
	}
	defer rows.Close()

	var columns []models.ColumnInfo
	for rows.Next() {
		var col models.ColumnInfo
		if err := rows.Scan(&col.Name, &col.Type); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		columns = append(columns, col)
	}

	// For managed sources, also get unique attributes from the materialized view
	if source.SchemaType == models.SchemaTypeManaged {
		attrs, err := conn.GetUniqueAttributes(ctx)
		if err != nil {
			// Don't fail if we can't get attributes, just log the error
			p.log.Error("error getting unique attributes",
				"source_id", source.ID,
				"error", err,
			)
		} else {
			// Add each attribute as a column with type String
			for _, attr := range attrs {
				columns = append(columns, models.ColumnInfo{
					Name: fmt.Sprintf("log_attributes['%s']", attr),
					Type: "String",
				})
			}
		}
	}

	return columns, nil
}

// GetTableSchema returns the CREATE TABLE statement for a source's table
func (p *Pool) GetTableSchema(ctx context.Context, source *models.Source) (string, error) {
	p.log.Debug("getting table schema",
		"source_id", source.ID,
		"database", source.Connection.Database,
		"table", source.Connection.TableName,
	)

	conn, err := p.GetConnection(source.ID)
	if err != nil {
		return "", fmt.Errorf("error getting connection: %w", err)
	}

	return conn.GetTableSchema(ctx)
}

// Close closes all connections in the pool
func (p *Pool) Close() error {
	p.log.Info("closing all connections in pool")

	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for id, conn := range p.connMap {
		if err := conn.Close(); err != nil {
			p.log.Error("error closing connection",
				"source_id", id,
				"error", err,
			)
			lastErr = err
		}
	}

	return lastErr
}

// QueryLogs queries logs from a source with pagination
func (p *Pool) QueryLogs(ctx context.Context, sourceID string, params LogQueryParams) (*LogQueryResult, error) {
	conn, err := p.GetConnection(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting connection: %w", err)
	}
	return conn.QueryLogs(ctx, params)
}

// GetHealth returns the health status for a given source ID
func (p *Pool) GetHealth(sourceID string) (models.SourceHealth, error) {
	return p.health.GetHealth(sourceID)
}

// HealthUpdates returns a channel that receives health updates
func (p *Pool) HealthUpdates() <-chan models.SourceHealth {
	return p.health.HealthUpdates()
}
