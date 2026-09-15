package metrics

import (
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// ClickHouseMetrics provides a metrics collector for ClickHouse operations
type ClickHouseMetrics struct {
	source *models.Source
}

// NewClickHouseMetrics creates a metrics collector for a specific source
func NewClickHouseMetrics(source *models.Source) *ClickHouseMetrics {
	return &ClickHouseMetrics{
		source: source,
	}
}

// RecordQueryMetrics records comprehensive query execution metrics
func (m *ClickHouseMetrics) RecordQueryMetrics(
	queryType string,
	success bool,
	duration time.Duration,
	rowsReturned int64,
	errorType string,
	timedOut bool,
) {
	// Record unified query metrics with rich context
	RecordQuery(m.source, queryType, success, duration, rowsReturned)

	// Record timeout if applicable
	if timedOut {
		RecordQueryTimeout(m.source, queryType)
	}

	// Record error if applicable
	if !success && errorType != "" {
		RecordQueryError(m.source, errorType)
	}
}

// RecordConnectionValidation records connection validation metrics
func (m *ClickHouseMetrics) RecordConnectionValidation(success bool) {
	RecordClickHouseValidation(m.source, success)
}

// RecordReconnection records reconnection attempt metrics
func (m *ClickHouseMetrics) RecordReconnection(success bool) {
	RecordClickHouseReconnection(m.source, success)
}

// UpdateConnectionStatus updates the connection health status
func (m *ClickHouseMetrics) UpdateConnectionStatus(healthy bool) {
	RecordClickHouseConnectionStatus(m.source, healthy)
}
