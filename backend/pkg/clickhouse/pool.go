package clickhouse

import (
	"fmt"
	"sync"

	"backend-v2/pkg/models"
)

// Pool manages multiple Clickhouse connections
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

// AddSource adds a new source to the pool and establishes connection
func (p *Pool) AddSource(source *models.Source) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if connection already exists
	if _, exists := p.connMap[source.ID]; exists {
		return fmt.Errorf("connection for source %s already exists", source.ID)
	}

	// Create new connection
	conn, err := NewConnection(source)
	if err != nil {
		return fmt.Errorf("error creating connection: %w", err)
	}

	// Add to pool and start health monitoring
	p.connMap[source.ID] = conn
	p.health.AddConnection(conn)

	return nil
}

// RemoveSource removes a source from the pool and closes its connection
func (p *Pool) RemoveSource(sourceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	conn, exists := p.connMap[sourceID]
	if !exists {
		return ErrConnectionNotFound
	}

	// Stop health monitoring
	p.health.RemoveConnection(sourceID)

	// Close connection
	if err := conn.Close(); err != nil {
		return fmt.Errorf("error closing connection: %w", err)
	}

	delete(p.connMap, sourceID)
	return nil
}

// GetConnection returns a connection for a given source ID
func (p *Pool) GetConnection(sourceID string) (*Connection, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	conn, exists := p.connMap[sourceID]
	if !exists {
		return nil, ErrConnectionNotFound
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

// Close closes all connections in the pool
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop health checker
	p.health.Stop()

	var lastErr error
	for id, conn := range p.connMap {
		if err := conn.Close(); err != nil {
			lastErr = fmt.Errorf("error closing connection %s: %w", id, err)
		}
	}

	return lastErr
}
