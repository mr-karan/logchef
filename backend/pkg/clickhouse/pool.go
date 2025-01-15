package clickhouse

import (
	"context"
	"fmt"
	"sync"

	"backend-v2/pkg/models"
)

// Pool manages a pool of Clickhouse connections
type Pool struct {
	mu      sync.RWMutex
	health  *HealthChecker
	connMap map[string]*Connection
}

// NewPool creates a new connection pool
func NewPool() *Pool {
	return &Pool{
		health:  NewHealthChecker(),
		connMap: make(map[string]*Connection),
	}
}

// AddSource adds or updates a source in the pool
func (p *Pool) AddSource(source *models.Source) error {
	// Create new connection using the existing NewConnection function
	conn, err := NewConnection(source)
	if err != nil {
		return fmt.Errorf("error creating connection: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Close existing connection if it exists
	if oldConn, exists := p.connMap[source.ID]; exists {
		_ = oldConn.Close()
	}

	p.connMap[source.ID] = conn
	p.health.AddConnection(conn)

	return nil
}

// RemoveSource removes a source from the pool
func (p *Pool) RemoveSource(sourceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if conn, exists := p.connMap[sourceID]; exists {
		delete(p.connMap, sourceID)
		p.health.RemoveConnection(sourceID)
		return conn.Close()
	}
	return nil
}

// Close closes all connections in the pool
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.health.Stop()

	var lastErr error
	for id, conn := range p.connMap {
		if err := conn.Close(); err != nil {
			lastErr = err
		}
		delete(p.connMap, id)
	}
	return lastErr
}

// DescribeTable retrieves the schema information for a source's table
func (p *Pool) DescribeTable(ctx context.Context, source *models.Source) ([]models.ColumnInfo, error) {
	conn, err := p.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting connection: %w", err)
	}

	query := fmt.Sprintf("SELECT name, type FROM system.columns WHERE database = '%s' AND table = '%s'",
		source.Database, source.TableName)
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

	return columns, nil
}

// QueryLogs retrieves logs from a source with pagination
func (p *Pool) QueryLogs(ctx context.Context, source *models.Source, limit, offset int) ([]map[string]interface{}, error) {
	conn, err := p.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting connection: %w", err)
	}

	query := fmt.Sprintf("SELECT * FROM %s ORDER BY timestamp DESC LIMIT %d OFFSET %d",
		source.GetFullTableName(), limit, offset)

	rows, err := conn.DB.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("error querying logs: %w", err)
	}
	defer rows.Close()

	var logs []map[string]interface{}
	converter := NewTypeConverter()

	for rows.Next() {
		row, err := converter.ScanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		logs = append(logs, row)
	}

	return logs, nil
}

// GetConnection returns a connection for a given source ID
func (p *Pool) GetConnection(sourceID string) (*Connection, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	conn, exists := p.connMap[sourceID]
	if !exists {
		return nil, fmt.Errorf("connection not found for source: %s", sourceID)
	}

	return conn, nil
}

// GetHealth returns the health status for a given source ID
func (p *Pool) GetHealth(sourceID string) (models.SourceHealth, error) {
	return p.health.GetHealth(sourceID)
}

// HealthUpdates returns a channel that receives health updates
func (p *Pool) HealthUpdates() <-chan models.SourceHealth {
	return p.health.HealthUpdates()
}
