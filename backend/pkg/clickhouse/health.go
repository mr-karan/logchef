package clickhouse

import (
	"context"
	"sync"
	"time"

	"backend-v2/pkg/models"
)

// HealthChecker manages health checks for Clickhouse connections
type HealthChecker struct {
	mu          sync.RWMutex
	connections map[string]*Connection
	healthChan  chan models.SourceHealth
	stopChan    chan struct{}
}

// NewHealthChecker creates a new health checker
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		connections: make(map[string]*Connection),
		healthChan:  make(chan models.SourceHealth, healthChannelBuffer),
		stopChan:    make(chan struct{}),
	}
}

// AddConnection adds a connection to be monitored
func (h *HealthChecker) AddConnection(conn *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.connections[conn.Source.ID] = conn
	go h.runHealthCheck(conn.Source.ID)
}

// RemoveConnection removes a connection from monitoring
func (h *HealthChecker) RemoveConnection(sourceID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.connections, sourceID)
}

// GetHealth returns the health status for a given source ID
func (h *HealthChecker) GetHealth(sourceID string) (models.SourceHealth, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	conn, exists := h.connections[sourceID]
	if !exists {
		return models.SourceHealth{}, ErrConnectionNotFound
	}

	return conn.LastHealth, nil
}

// HealthUpdates returns a channel that receives health updates
func (h *HealthChecker) HealthUpdates() <-chan models.SourceHealth {
	return h.healthChan
}

// runHealthCheck performs periodic health checks for a connection
func (h *HealthChecker) runHealthCheck(sourceID string) {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopChan:
			return
		case <-ticker.C:
			h.mu.RLock()
			conn, exists := h.connections[sourceID]
			h.mu.RUnlock()

			if !exists {
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
			health := conn.CheckHealth(ctx)
			cancel()

			h.mu.Lock()
			conn.LastHealth = health
			conn.Source.IsConnected = health.IsHealthy
			h.mu.Unlock()

			// Send health update
			select {
			case h.healthChan <- health:
			default:
				// Channel is full, skip this update
			}
		}
	}
}

// Stop stops all health checks
func (h *HealthChecker) Stop() {
	close(h.stopChan)
	close(h.healthChan)
}
