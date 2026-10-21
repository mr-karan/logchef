package metrics

import (
	"fmt"
	"time"

	"github.com/VictoriaMetrics/metrics"

	"github.com/mr-karan/logchef/pkg/models"
)

// ClickHouseMetrics records the series of one source in its own set, so
// removing the source drops them all at once.
type ClickHouseMetrics struct {
	set    *metrics.Set
	labels string
	host   string
}

// NewClickHouseMetrics creates and registers a metrics collector for a specific source
func NewClickHouseMetrics(source *models.Source) *ClickHouseMetrics {
	m := &ClickHouseMetrics{
		set: metrics.NewSet(),
		labels: fmt.Sprintf(`source_id="%d",source_name=%q,database=%q,table=%q`,
			source.ID, source.Name, source.Connection.Database, source.Connection.TableName),
		host: source.Connection.Host,
	}
	metrics.RegisterSet(m.set)
	return m
}

// Unregister stops exporting the source's series. Writes from work still in
// flight land in the detached set and are never exported.
func (m *ClickHouseMetrics) Unregister() {
	metrics.UnregisterSet(m.set, true)
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
	m.set.GetOrCreateCounter(fmt.Sprintf(`logchef_query_total{%s,query_type=%q,result=%q}`,
		m.labels, queryType, resultLabel(success))).Inc()
	m.set.GetOrCreateHistogram(fmt.Sprintf(`logchef_query_duration_seconds{%s}`, m.labels)).Update(duration.Seconds())

	if success && rowsReturned >= 0 {
		m.set.GetOrCreateHistogram(fmt.Sprintf(`logchef_query_rows_returned{%s}`, m.labels)).Update(float64(rowsReturned))
	}

	if timedOut {
		m.set.GetOrCreateCounter(fmt.Sprintf(`logchef_query_timeouts_total{%s,query_type=%q}`, m.labels, queryType)).Inc()
	}

	if !success && errorType != "" {
		m.set.GetOrCreateCounter(fmt.Sprintf(`logchef_query_errors_total{%s,error_type=%q}`, m.labels, errorType)).Inc()
	}
}

// RecordConnectionValidation records connection validation metrics
func (m *ClickHouseMetrics) RecordConnectionValidation(success bool) {
	m.set.GetOrCreateCounter(fmt.Sprintf(`logchef_clickhouse_connection_validation_total{%s,host=%q,result=%q}`,
		m.labels, m.host, resultLabel(success))).Inc()
}

// UpdateConnectionStatus updates the connection health status
func (m *ClickHouseMetrics) UpdateConnectionStatus(healthy bool) {
	status := 0.0
	if healthy {
		status = 1.0
	}
	m.set.GetOrCreateGauge(fmt.Sprintf(`logchef_clickhouse_connection_status{%s,host=%q}`, m.labels, m.host), nil).Set(status)
}

func resultLabel(success bool) string {
	if success {
		return "success"
	}
	return "failure"
}
