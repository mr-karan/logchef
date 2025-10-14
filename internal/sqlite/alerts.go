package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

const (
	insertAlertQuery = `INSERT INTO alerts (
    team_id,
    source_id,
    name,
    description,
    query_type,
    query,
    lookback_seconds,
    threshold_operator,
    threshold_value,
    frequency_seconds,
    severity,
    channels,
    is_active
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, created_at, updated_at, last_evaluated_at, last_triggered_at`

	selectAlertBase = `SELECT
    id,
    team_id,
    source_id,
    name,
    description,
    query_type,
    query,
    lookback_seconds,
    threshold_operator,
    threshold_value,
    frequency_seconds,
    severity,
    channels,
    is_active,
    last_evaluated_at,
    last_triggered_at,
    created_at,
    updated_at
FROM alerts`

	listActiveAlertsDueQuery = selectAlertBase + `
WHERE is_active = 1
  AND (
        last_evaluated_at IS NULL
        OR last_evaluated_at <= datetime('now', '-' || frequency_seconds || ' seconds')
      )`

	updateAlertEvaluatedQuery = `UPDATE alerts
SET last_evaluated_at = datetime('now'),
    updated_at = datetime('now')
WHERE id = ?`

	updateAlertTriggeredQuery = `UPDATE alerts
SET last_triggered_at = datetime('now'),
    last_evaluated_at = datetime('now'),
    updated_at = datetime('now')
WHERE id = ?`

	updateAlertQuery = `UPDATE alerts
SET name = ?,
    description = ?,
    query_type = ?,
    query = ?,
    lookback_seconds = ?,
    threshold_operator = ?,
    threshold_value = ?,
    frequency_seconds = ?,
    severity = ?,
    channels = ?,
    is_active = ?,
    updated_at = datetime('now')
WHERE id = ?`

	deleteAlertQuery = `DELETE FROM alerts WHERE id = ?`

	insertAlertHistoryQuery = `INSERT INTO alert_history (
    alert_id,
    status,
    value_text,
    channels,
    message
) VALUES (?, ?, ?, ?, ?)
RETURNING id, triggered_at, resolved_at, created_at`

	selectAlertHistoryBase = `SELECT
    id,
    alert_id,
    status,
    triggered_at,
    resolved_at,
    value_text,
    channels,
    message,
    created_at
FROM alert_history`

	getLatestUnresolvedHistoryQuery = selectAlertHistoryBase + `
WHERE alert_id = ? AND status = 'triggered'
ORDER BY triggered_at DESC
LIMIT 1`

	resolveAlertHistoryQuery = `UPDATE alert_history
SET status = 'resolved',
    resolved_at = datetime('now'),
    message = ?
WHERE id = ?`

	pruneAlertHistoryQuery = `WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (ORDER BY triggered_at DESC, id DESC) AS rn
    FROM alert_history
    WHERE alert_id = ?
)
DELETE FROM alert_history
WHERE alert_id = ?
  AND id IN (
    SELECT id FROM ranked WHERE rn > ?
 )`
)

// CreateAlert inserts a new alert definition for a team/source pair.
func (db *DB) CreateAlert(ctx context.Context, alert *models.Alert) error {
	if alert == nil {
		return fmt.Errorf("alert payload is required")
	}

	channelsJSON, err := json.Marshal(alert.Channels)
	if err != nil {
		return fmt.Errorf("failed to marshal alert channels: %w", err)
	}

	row := db.db.QueryRowContext(ctx, insertAlertQuery,
		int64(alert.TeamID),
		int64(alert.SourceID),
		alert.Name,
		nullableString(alert.Description),
		string(alert.QueryType),
		alert.Query,
		alert.LookbackSeconds,
		string(alert.ThresholdOperator),
		alert.ThresholdValue,
		alert.FrequencySeconds,
		string(alert.Severity),
		string(channelsJSON),
		boolToInt(alert.IsActive),
	)

	var (
		id              int64
		createdAt       time.Time
		updatedAt       time.Time
		lastEvaluatedAt sql.NullTime
		lastTriggeredAt sql.NullTime
	)

	if err := row.Scan(&id, &createdAt, &updatedAt, &lastEvaluatedAt, &lastTriggeredAt); err != nil {
		return fmt.Errorf("failed to insert alert: %w", err)
	}

	alert.ID = models.AlertID(id)
	alert.CreatedAt = createdAt
	alert.UpdatedAt = updatedAt
	if lastEvaluatedAt.Valid {
		alert.LastEvaluatedAt = &lastEvaluatedAt.Time
	}
	if lastTriggeredAt.Valid {
		alert.LastTriggeredAt = &lastTriggeredAt.Time
	}
	return nil
}

// UpdateAlert persists changes to an existing alert definition.
func (db *DB) UpdateAlert(ctx context.Context, alert *models.Alert) error {
	if alert == nil {
		return fmt.Errorf("alert payload is required")
	}

	channelsJSON, err := json.Marshal(alert.Channels)
	if err != nil {
		return fmt.Errorf("failed to marshal alert channels: %w", err)
	}

	res, err := db.db.ExecContext(ctx, updateAlertQuery,
		alert.Name,
		nullableString(alert.Description),
		string(alert.QueryType),
		alert.Query,
		alert.LookbackSeconds,
		string(alert.ThresholdOperator),
		alert.ThresholdValue,
		alert.FrequencySeconds,
		string(alert.Severity),
		string(channelsJSON),
		boolToInt(alert.IsActive),
		int64(alert.ID),
	)
	if err != nil {
		return fmt.Errorf("failed to update alert: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetAlert retrieves an alert by its identifier.
func (db *DB) GetAlert(ctx context.Context, alertID models.AlertID) (*models.Alert, error) {
	query := selectAlertBase + " WHERE id = ?"
	row := db.db.QueryRowContext(ctx, query, int64(alertID))
	return scanAlert(row)
}

// GetAlertForTeamSource ensures the alert belongs to the requested team and source.
func (db *DB) GetAlertForTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, alertID models.AlertID) (*models.Alert, error) {
	query := selectAlertBase + " WHERE id = ? AND team_id = ? AND source_id = ?"
	row := db.db.QueryRowContext(ctx, query, int64(alertID), int64(teamID), int64(sourceID))
	return scanAlert(row)
}

// ListAlertsByTeamAndSource fetches all alerts scoped to a specific team and source.
func (db *DB) ListAlertsByTeamAndSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) ([]*models.Alert, error) {
	query := selectAlertBase + " WHERE team_id = ? AND source_id = ? ORDER BY created_at DESC"
	rows, err := db.db.QueryContext(ctx, query, int64(teamID), int64(sourceID))
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*models.Alert
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating alerts: %w", err)
	}
	return alerts, nil
}

// DeleteAlert removes an alert definition from the database.
func (db *DB) DeleteAlert(ctx context.Context, alertID models.AlertID) error {
	res, err := db.db.ExecContext(ctx, deleteAlertQuery, int64(alertID))
	if err != nil {
		return fmt.Errorf("failed to delete alert: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListActiveAlertsDue returns alerts that need to be evaluated.
func (db *DB) ListActiveAlertsDue(ctx context.Context) ([]*models.Alert, error) {
	rows, err := db.db.QueryContext(ctx, listActiveAlertsDueQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch due alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*models.Alert
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating due alerts: %w", err)
	}
	return alerts, nil
}

// MarkAlertEvaluated updates the bookkeeping fields when an alert evaluation completes without triggering.
func (db *DB) MarkAlertEvaluated(ctx context.Context, alertID models.AlertID) error {
	if _, err := db.db.ExecContext(ctx, updateAlertEvaluatedQuery, int64(alertID)); err != nil {
		return fmt.Errorf("failed to mark alert evaluated: %w", err)
	}
	return nil
}

// MarkAlertTriggered updates the bookkeeping fields when an alert triggers.
func (db *DB) MarkAlertTriggered(ctx context.Context, alertID models.AlertID) error {
	if _, err := db.db.ExecContext(ctx, updateAlertTriggeredQuery, int64(alertID)); err != nil {
		return fmt.Errorf("failed to mark alert triggered: %w", err)
	}
	return nil
}

// InsertAlertHistory creates a history entry for an alert trigger or resolution event.
func (db *DB) InsertAlertHistory(ctx context.Context, entry *models.AlertHistoryEntry) error {
	if entry == nil {
		return fmt.Errorf("history entry is required")
	}
	channelsJSON, err := json.Marshal(entry.Channels)
	if err != nil {
		return fmt.Errorf("failed to marshal history channels: %w", err)
	}

	row := db.db.QueryRowContext(ctx, insertAlertHistoryQuery,
		entry.AlertID,
		string(entry.Status),
		entry.ValueText,
		string(channelsJSON),
		nullableString(entry.Message),
	)

	var (
		id          int64
		triggeredAt time.Time
		resolvedAt  sql.NullTime
		createdAt   time.Time
	)
	if err := row.Scan(&id, &triggeredAt, &resolvedAt, &createdAt); err != nil {
		return fmt.Errorf("failed to insert alert history: %w", err)
	}
	entry.ID = id
	entry.TriggeredAt = triggeredAt
	entry.CreatedAt = createdAt
	if resolvedAt.Valid {
		entry.ResolvedAt = &resolvedAt.Time
	}
	return nil
}

// ResolveAlertHistory marks a previously triggered history entry as resolved.
func (db *DB) ResolveAlertHistory(ctx context.Context, historyID int64, message string) error {
	res, err := db.db.ExecContext(ctx, resolveAlertHistoryQuery, nullableString(message), historyID)
	if err != nil {
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// PruneAlertHistory removes older history entries beyond the configured limit.
func (db *DB) PruneAlertHistory(ctx context.Context, alertID models.AlertID, limit int) error {
	if limit <= 0 {
		limit = models.DefaultAlertHistoryLimit
	}

	if _, err := db.db.ExecContext(ctx, pruneAlertHistoryQuery, int64(alertID), int64(alertID), limit); err != nil {
		return fmt.Errorf("failed to prune alert history: %w", err)
	}
	return nil
}

// GetLatestUnresolvedAlertHistory fetches the most recent trigger entry that has not been resolved.
func (db *DB) GetLatestUnresolvedAlertHistory(ctx context.Context, alertID models.AlertID) (*models.AlertHistoryEntry, error) {
	row := db.db.QueryRowContext(ctx, getLatestUnresolvedHistoryQuery, alertID)
	return scanAlertHistory(row)
}

// ListAlertHistory returns the most recent history entries for an alert.
func (db *DB) ListAlertHistory(ctx context.Context, alertID models.AlertID, limit int) ([]*models.AlertHistoryEntry, error) {
	if limit <= 0 {
		limit = models.DefaultAlertHistoryLimit
	}
	query := selectAlertHistoryBase + " WHERE alert_id = ? ORDER BY triggered_at DESC LIMIT ?"
	rows, err := db.db.QueryContext(ctx, query, alertID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list alert history: %w", err)
	}
	defer rows.Close()

	var history []*models.AlertHistoryEntry
	for rows.Next() {
		entry, err := scanAlertHistory(rows)
		if err != nil {
			return nil, err
		}
		history = append(history, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating alert history: %w", err)
	}
	return history, nil
}

func scanAlert(scanner interface{ Scan(dest ...any) error }) (*models.Alert, error) {
	var (
		id                int64
		teamID            int64
		sourceID          int64
		name              string
		description       sql.NullString
		queryType         string
		query             string
		lookbackSeconds   int
		thresholdOperator string
		thresholdValue    float64
		frequencySeconds  int
		severity          string
		channelsJSON      string
		isActive          int64
		lastEvaluatedAt   sql.NullTime
		lastTriggeredAt   sql.NullTime
		createdAt         time.Time
		updatedAt         time.Time
	)
	if err := scanner.Scan(&id, &teamID, &sourceID, &name, &description, &queryType, &query, &lookbackSeconds, &thresholdOperator, &thresholdValue, &frequencySeconds, &severity, &channelsJSON, &isActive, &lastEvaluatedAt, &lastTriggeredAt, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf("failed to scan alert: %w", err)
	}

	var channels []models.AlertChannel
	if channelsJSON != "" {
		if err := json.Unmarshal([]byte(channelsJSON), &channels); err != nil {
			return nil, fmt.Errorf("failed to unmarshal alert channels: %w", err)
		}
	}

	alert := &models.Alert{
		ID:                models.AlertID(id),
		TeamID:            models.TeamID(teamID),
		SourceID:          models.SourceID(sourceID),
		Name:              name,
		Description:       description.String,
		QueryType:         models.AlertQueryType(queryType),
		Query:             query,
		LookbackSeconds:   lookbackSeconds,
		ThresholdOperator: models.AlertThresholdOperator(thresholdOperator),
		ThresholdValue:    thresholdValue,
		FrequencySeconds:  frequencySeconds,
		Severity:          models.AlertSeverity(severity),
		Channels:          channels,
		IsActive:          isActive == 1,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}
	if lastEvaluatedAt.Valid {
		alert.LastEvaluatedAt = &lastEvaluatedAt.Time
	}
	if lastTriggeredAt.Valid {
		alert.LastTriggeredAt = &lastTriggeredAt.Time
	}
	return alert, nil
}

func scanAlertHistory(scanner interface{ Scan(dest ...any) error }) (*models.AlertHistoryEntry, error) {
	var (
		id           int64
		alertID      int64
		status       string
		triggeredAt  time.Time
		resolvedAt   sql.NullTime
		valueText    sql.NullString
		channelsJSON sql.NullString
		message      sql.NullString
		createdAt    time.Time
	)
	if err := scanner.Scan(&id, &alertID, &status, &triggeredAt, &resolvedAt, &valueText, &channelsJSON, &message, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf("failed to scan alert history: %w", err)
	}

	var channels []models.AlertChannel
	if channelsJSON.Valid && channelsJSON.String != "" {
		if err := json.Unmarshal([]byte(channelsJSON.String), &channels); err != nil {
			return nil, fmt.Errorf("failed to unmarshal alert history channels: %w", err)
		}
	}

	entry := &models.AlertHistoryEntry{
		ID:          id,
		AlertID:     models.AlertID(alertID),
		Status:      models.AlertStatus(status),
		TriggeredAt: triggeredAt,
		ValueText:   valueText.String,
		Channels:    channels,
		Message:     message.String,
		CreatedAt:   createdAt,
	}
	if resolvedAt.Valid {
		entry.ResolvedAt = &resolvedAt.Time
	}
	return entry, nil
}

func nullableString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}
