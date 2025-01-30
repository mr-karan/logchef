package clickhouse

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"backend-v2/pkg/logger"
	"backend-v2/pkg/models"
)

// HealthChecker manages health checks for Clickhouse connections
type HealthChecker struct {
	mu          sync.RWMutex
	connections map[string]*Connection
	healthChan  chan models.SourceHealth
	stopChan    chan struct{}
	log         *slog.Logger
}

// NewHealthChecker creates a new health checker
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		connections: make(map[string]*Connection),
		healthChan:  make(chan models.SourceHealth, healthChannelBuffer),
		stopChan:    make(chan struct{}),
		log:         logger.Default().With("component", "clickhouse_health"),
	}
}

// AddConnection adds a connection to be monitored
func (h *HealthChecker) AddConnection(conn *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.connections[conn.Source.ID] = conn
	h.log.Info("added connection for health monitoring",
		"source_id", conn.Source.ID,
		"database", conn.Source.Connection.Database,
		"table", conn.Source.Connection.TableName,
	)
	go h.runHealthCheck(conn.Source.ID)
}

// RemoveConnection removes a connection from monitoring
func (h *HealthChecker) RemoveConnection(sourceID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.connections[sourceID]; exists {
		h.log.Info("removed connection from health monitoring",
			"source_id", sourceID,
		)
		delete(h.connections, sourceID)
	}
}

// GetHealth returns the health status for a given source ID
func (h *HealthChecker) GetHealth(sourceID string) (models.SourceHealth, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	conn, exists := h.connections[sourceID]
	if !exists {
		h.log.Debug("health status requested for unknown source",
			"source_id", sourceID,
		)
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

	log := h.log.With("source_id", sourceID)
	log.Debug("starting health check routine")

	for {
		select {
		case <-h.stopChan:
			log.Debug("stopping health check routine")
			return
		case <-ticker.C:
			h.mu.RLock()
			conn, exists := h.connections[sourceID]
			h.mu.RUnlock()

			if !exists {
				log.Debug("connection no longer exists, stopping health check")
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
			health := conn.CheckHealth(ctx)
			cancel()

			h.mu.Lock()
			conn.LastHealth = health
			conn.Source.IsConnected = health.IsHealthy
			h.mu.Unlock()

			if !health.IsHealthy {
				log.Error("health check failed",
					"error", health.Error,
					"latency", health.Latency.String(),
				)
			} else {
				log.Debug("health check successful",
					"latency", health.Latency.String(),
				)
			}

			// Send health update
			select {
			case h.healthChan <- health:
			default:
				log.Warn("health update channel is full, skipping update")
			}
		}
	}
}

// Stop stops all health checks
func (h *HealthChecker) Stop() {
	h.log.Info("stopping all health checks")
	close(h.stopChan)
	close(h.healthChan)
}
