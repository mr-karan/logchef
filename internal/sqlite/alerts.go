package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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
    condition_json,
    lookback_seconds,
    threshold_operator,
    threshold_value,
    frequency_seconds,
    severity,
    labels_json,
    annotations_json,
    recipient_user_ids_json,
    webhook_urls_json,
    generator_url,
    is_active
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, created_at, updated_at, last_state, last_evaluated_at, last_triggered_at`

	selectAlertBase = `SELECT
    id,
    team_id,
    source_id,
    name,
    description,
    query_type,
    query,
    condition_json,
    lookback_seconds,
    threshold_operator,
    threshold_value,
    frequency_seconds,
    severity,
    labels_json,
    annotations_json,
    recipient_user_ids_json,
    webhook_urls_json,
    generator_url,
    is_active,
    last_state,
    last_evaluated_at,
    last_triggered_at,
    created_at,
    updated_at
FROM alerts`

	listAlertsByTeamSourceQuery = selectAlertBase + `
WHERE team_id = ? AND source_id = ?
ORDER BY updated_at DESC, created_at DESC`

	getAlertByIDQuery = selectAlertBase + `
WHERE id = ?`

	getAlertForTeamSourceQuery = selectAlertBase + `
WHERE id = ? AND team_id = ? AND source_id = ?`

	listActiveAlertsDueQuery = selectAlertBase + `
WHERE is_active = 1
  AND (
        last_evaluated_at IS NULL
        OR last_evaluated_at <= datetime('now', '-' || frequency_seconds || ' seconds')
      )`

	updateAlertEvaluatedQuery = `UPDATE alerts
SET last_state = 'resolved',
    last_evaluated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?`

	updateAlertTriggeredQuery = `UPDATE alerts
SET last_state = 'firing',
    last_triggered_at = CASE WHEN last_state = 'firing' THEN last_triggered_at ELSE strftime('%Y-%m-%dT%H:%M:%SZ', 'now') END,
    last_evaluated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?`

	updateAlertQuery = `UPDATE alerts
SET name = ?,
    description = ?,
    query_type = ?,
    query = ?,
    condition_json = ?,
    lookback_seconds = ?,
    threshold_operator = ?,
    threshold_value = ?,
    frequency_seconds = ?,
    severity = ?,
    labels_json = ?,
    annotations_json = ?,
    recipient_user_ids_json = ?,
    webhook_urls_json = ?,
    generator_url = ?,
    is_active = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?`

	deleteAlertQuery = `DELETE FROM alerts WHERE id = ?`

	insertAlertHistoryQuery = `INSERT INTO alert_history (
    alert_id,
    status,
    value,
    message,
    payload_json
) VALUES (?, ?, ?, ?, ?)
RETURNING id, triggered_at, resolved_at, created_at`

	selectAlertHistoryBase = `SELECT
    id,
    alert_id,
    status,
    triggered_at,
    resolved_at,
    value,
    message,
    payload_json,
    created_at
FROM alert_history`

	getLatestUnresolvedHistoryQuery = selectAlertHistoryBase + `
WHERE alert_id = ? AND status = 'triggered'
ORDER BY triggered_at DESC, id DESC
LIMIT 1`

	resolveAlertHistoryQuery = `UPDATE alert_history
SET status = 'resolved',
    resolved_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
    message = ?
WHERE id = ?`

	updateAlertHistoryPayloadQuery = `UPDATE alert_history
SET payload_json = ?
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

func marshalStringMap(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "", nil
	}
	buf, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func unmarshalStringMap(raw sql.NullString) (map[string]string, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func marshalUserIDs(ids []models.UserID) (string, error) {
	if len(ids) == 0 {
		return "", nil
	}
	buf, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func unmarshalUserIDs(raw sql.NullString) ([]models.UserID, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var out []models.UserID
	if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func marshalStringSlice(values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	buf, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func unmarshalStringSlice(raw sql.NullString) ([]string, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func unmarshalPayload(raw sql.NullString) (map[string]any, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateAlert inserts a new alert definition for a team/source pair.
func (db *DB) CreateAlert(ctx context.Context, alert *models.Alert) error {
	if alert == nil {
		return fmt.Errorf("alert payload is required")
	}

	labelsJSON, err := marshalStringMap(alert.Labels)
	if err != nil {
		return fmt.Errorf("failed to marshal labels: %w", err)
	}
	annotationsJSON, err := marshalStringMap(alert.Annotations)
	if err != nil {
		return fmt.Errorf("failed to marshal annotations: %w", err)
	}
	recipientUserIDsJSON, err := marshalUserIDs(alert.RecipientUserIDs)
	if err != nil {
		return fmt.Errorf("failed to marshal recipient user IDs: %w", err)
	}
	webhookURLsJSON, err := marshalStringSlice(alert.WebhookURLs)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook URLs: %w", err)
	}

	row := db.writeDB.QueryRowContext(ctx, insertAlertQuery,
		int64(alert.TeamID),
		int64(alert.SourceID),
		alert.Name,
		nullableString(alert.Description),
		string(alert.QueryType),
		nullableString(alert.Query),
		nullableString(alert.ConditionJSON),
		alert.LookbackSeconds,
		string(alert.ThresholdOperator),
		alert.ThresholdValue,
		alert.FrequencySeconds,
		string(alert.Severity),
		nullableString(labelsJSON),
		nullableString(annotationsJSON),
		nullableString(recipientUserIDsJSON),
		nullableString(webhookURLsJSON),
		nullableString(alert.GeneratorURL),
		boolToInt(alert.IsActive),
	)

	var (
		id              int64
		createdAt       time.Time
		updatedAt       time.Time
		lastState       string
		lastEvaluatedAt sql.NullTime
		lastTriggeredAt sql.NullTime
	)

	if err := row.Scan(&id, &createdAt, &updatedAt, &lastState, &lastEvaluatedAt, &lastTriggeredAt); err != nil {
		return fmt.Errorf("failed to insert alert: %w", err)
	}

	alert.ID = models.AlertID(id)
	alert.CreatedAt = createdAt
	alert.UpdatedAt = updatedAt
	alert.LastState = models.AlertState(lastState)
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

	labelsJSON, err := marshalStringMap(alert.Labels)
	if err != nil {
		return fmt.Errorf("failed to marshal labels: %w", err)
	}
	annotationsJSON, err := marshalStringMap(alert.Annotations)
	if err != nil {
		return fmt.Errorf("failed to marshal annotations: %w", err)
	}
	recipientUserIDsJSON, err := marshalUserIDs(alert.RecipientUserIDs)
	if err != nil {
		return fmt.Errorf("failed to marshal recipient user IDs: %w", err)
	}
	webhookURLsJSON, err := marshalStringSlice(alert.WebhookURLs)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook URLs: %w", err)
	}

	res, err := db.writeDB.ExecContext(ctx, updateAlertQuery,
		alert.Name,
		nullableString(alert.Description),
		string(alert.QueryType),
		nullableString(alert.Query),
		nullableString(alert.ConditionJSON),
		alert.LookbackSeconds,
		string(alert.ThresholdOperator),
		alert.ThresholdValue,
		alert.FrequencySeconds,
		string(alert.Severity),
		nullableString(labelsJSON),
		nullableString(annotationsJSON),
		nullableString(recipientUserIDsJSON),
		nullableString(webhookURLsJSON),
		nullableString(alert.GeneratorURL),
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

// DeleteAlert removes an alert definition.
func (db *DB) DeleteAlert(ctx context.Context, alertID models.AlertID) error {
	res, err := db.writeDB.ExecContext(ctx, deleteAlertQuery, int64(alertID))
	if err != nil {
		return fmt.Errorf("failed to delete alert: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanAlert(scanner interface {
	Scan(dest ...any) error
}) (*models.Alert, error) {
	var (
		id, teamID, sourceID             int64
		name                             string
		description                      sql.NullString
		queryType                        string
		query                            sql.NullString
		conditionJSON                    sql.NullString
		lookbackSeconds                  int
		thresholdOperator                string
		thresholdValue                   float64
		frequencySeconds                 int
		severity                         string
		labelsJSON                       sql.NullString
		annotationsJSON                  sql.NullString
		recipientUserIDsJSON             sql.NullString
		webhookURLsJSON                  sql.NullString
		generatorURL                     sql.NullString
		isActive                         int
		lastState                        string
		lastEvaluatedAt, lastTriggeredAt sql.NullTime
		createdAt, updatedAt             time.Time
	)

	if err := scanner.Scan(
		&id,
		&teamID,
		&sourceID,
		&name,
		&description,
		&queryType,
		&query,
		&conditionJSON,
		&lookbackSeconds,
		&thresholdOperator,
		&thresholdValue,
		&frequencySeconds,
		&severity,
		&labelsJSON,
		&annotationsJSON,
		&recipientUserIDsJSON,
		&webhookURLsJSON,
		&generatorURL,
		&isActive,
		&lastState,
		&lastEvaluatedAt,
		&lastTriggeredAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}

	labels, err := unmarshalStringMap(labelsJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decode labels: %w", err)
	}
	annotations, err := unmarshalStringMap(annotationsJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decode annotations: %w", err)
	}
	recipientUserIDs, err := unmarshalUserIDs(recipientUserIDsJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decode recipient user IDs: %w", err)
	}
	webhookURLs, err := unmarshalStringSlice(webhookURLsJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decode webhook URLs: %w", err)
	}

	alert := &models.Alert{
		ID:                models.AlertID(id),
		TeamID:            models.TeamID(teamID),
		SourceID:          models.SourceID(sourceID),
		Name:              name,
		Description:       description.String,
		QueryType:         models.AlertQueryType(queryType),
		Query:             query.String,
		ConditionJSON:     conditionJSON.String,
		LookbackSeconds:   lookbackSeconds,
		ThresholdOperator: models.AlertThresholdOperator(thresholdOperator),
		ThresholdValue:    thresholdValue,
		FrequencySeconds:  frequencySeconds,
		Severity:          models.AlertSeverity(severity),
		Labels:            labels,
		Annotations:       annotations,
		RecipientUserIDs:  recipientUserIDs,
		WebhookURLs:       webhookURLs,
		GeneratorURL:      generatorURL.String,
		IsActive:          isActive == 1,
		LastState:         models.AlertState(lastState),
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

// GetAlert retrieves an alert by ID.
func (db *DB) GetAlert(ctx context.Context, alertID models.AlertID) (*models.Alert, error) {
	row := db.readDB.QueryRowContext(ctx, getAlertByIDQuery, int64(alertID))
	alert, err := scanAlert(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan alert: %w", err)
	}
	return alert, nil
}

// GetAlertForTeamSource retrieves an alert by ID ensuring it belongs to the specified team/source.
func (db *DB) GetAlertForTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, alertID models.AlertID) (*models.Alert, error) {
	row := db.readDB.QueryRowContext(ctx, getAlertForTeamSourceQuery, int64(alertID), int64(teamID), int64(sourceID))
	alert, err := scanAlert(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan alert: %w", err)
	}
	return alert, nil
}

// ListAlertsByTeamAndSource returns alerts for the given team/source.
func (db *DB) ListAlertsByTeamAndSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) ([]*models.Alert, error) {
	rows, err := db.readDB.QueryContext(ctx, listAlertsByTeamSourceQuery, int64(teamID), int64(sourceID))
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*models.Alert
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}
		alerts = append(alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating alerts: %w", err)
	}
	return alerts, nil
}

// ListActiveAlertsDue returns alerts that need evaluation.
func (db *DB) ListActiveAlertsDue(ctx context.Context) ([]*models.Alert, error) {
	rows, err := db.readDB.QueryContext(ctx, listActiveAlertsDueQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to list active alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*models.Alert
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}
		alerts = append(alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating alerts: %w", err)
	}
	return alerts, nil
}

// MarkAlertEvaluated updates state after evaluation finishes without triggering.
func (db *DB) MarkAlertEvaluated(ctx context.Context, alertID models.AlertID) error {
	_, err := db.writeDB.ExecContext(ctx, updateAlertEvaluatedQuery, int64(alertID))
	if err != nil {
		return fmt.Errorf("failed to mark alert evaluated: %w", err)
	}
	return nil
}

// MarkAlertTriggered updates state when an alert fires.
func (db *DB) MarkAlertTriggered(ctx context.Context, alertID models.AlertID) error {
	_, err := db.writeDB.ExecContext(ctx, updateAlertTriggeredQuery, int64(alertID))
	if err != nil {
		return fmt.Errorf("failed to mark alert triggered: %w", err)
	}
	return nil
}

// InsertAlertHistory records a history entry and returns the hydrated entry.
func (db *DB) InsertAlertHistory(ctx context.Context, alertID models.AlertID, status models.AlertStatus, value *float64, message string, payload map[string]any) (*models.AlertHistoryEntry, error) {
	var valuePtr any
	if value != nil {
		valuePtr = *value
	} else {
		valuePtr = nil
	}
	var payloadJSON string
	if len(payload) > 0 {
		buf, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal history payload: %w", err)
		}
		payloadJSON = string(buf)
	}

	row := db.writeDB.QueryRowContext(ctx, insertAlertHistoryQuery,
		int64(alertID),
		string(status),
		valuePtr,
		nullableString(message),
		nullableString(payloadJSON),
	)

	var (
		id          int64
		triggeredAt time.Time
		resolvedAt  sql.NullTime
		createdAt   time.Time
	)

	if err := row.Scan(&id, &triggeredAt, &resolvedAt, &createdAt); err != nil {
		return nil, fmt.Errorf("failed to insert alert history: %w", err)
	}

	entry := &models.AlertHistoryEntry{
		ID:          id,
		AlertID:     alertID,
		Status:      status,
		TriggeredAt: triggeredAt,
		CreatedAt:   createdAt,
		Message:     message,
	}
	if value != nil {
		entry.Value = value
	}
	if resolvedAt.Valid {
		entry.ResolvedAt = &resolvedAt.Time
	}
	if len(payloadJSON) > 0 {
		payloadMap := make(map[string]any)
		if err := json.Unmarshal([]byte(payloadJSON), &payloadMap); err == nil {
			entry.Payload = payloadMap
		}
	}
	return entry, nil
}

// GetLatestUnresolvedAlertHistory fetches the newest unresolved history entry.
func (db *DB) GetLatestUnresolvedAlertHistory(ctx context.Context, alertID models.AlertID) (*models.AlertHistoryEntry, error) {
	row := db.readDB.QueryRowContext(ctx, getLatestUnresolvedHistoryQuery, int64(alertID))
	entry, err := scanAlertHistory(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan alert history: %w", err)
	}
	return entry, nil
}

// ResolveAlertHistory marks a history entry as resolved with an optional message.
func (db *DB) ResolveAlertHistory(ctx context.Context, historyID int64, message string) error {
	res, err := db.writeDB.ExecContext(ctx, resolveAlertHistoryQuery, nullableString(message), historyID)
	if err != nil {
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateAlertHistoryPayload updates the payload of an existing alert history entry.
func (db *DB) UpdateAlertHistoryPayload(ctx context.Context, historyID int64, payload map[string]any) error {
	var payloadJSON string
	if len(payload) > 0 {
		buf, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal history payload: %w", err)
		}
		payloadJSON = string(buf)
	}

	res, err := db.writeDB.ExecContext(ctx, updateAlertHistoryPayloadQuery, nullableString(payloadJSON), historyID)
	if err != nil {
		return fmt.Errorf("failed to update alert history payload: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListAlertHistory returns recent history entries for an alert.
func (db *DB) ListAlertHistory(ctx context.Context, alertID models.AlertID, limit int) ([]*models.AlertHistoryEntry, error) {
	query := selectAlertHistoryBase + `
WHERE alert_id = ?
ORDER BY triggered_at DESC, id DESC
LIMIT ?`

	rows, err := db.readDB.QueryContext(ctx, query, int64(alertID), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list alert history: %w", err)
	}
	defer rows.Close()

	var history []*models.AlertHistoryEntry
	for rows.Next() {
		entry, err := scanAlertHistory(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan alert history: %w", err)
		}
		history = append(history, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating alert history: %w", err)
	}
	return history, nil
}

// PruneAlertHistory keeps the most recent N entries for an alert.
func (db *DB) PruneAlertHistory(ctx context.Context, alertID models.AlertID, keep int) error {
	if keep <= 0 {
		return nil
	}
	if _, err := db.writeDB.ExecContext(ctx, pruneAlertHistoryQuery, int64(alertID), int64(alertID), keep); err != nil {
		return fmt.Errorf("failed to prune alert history: %w", err)
	}
	return nil
}

func scanAlertHistory(scanner interface {
	Scan(dest ...any) error
}) (*models.AlertHistoryEntry, error) {
	var (
		id, alertID int64
		status      string
		triggeredAt time.Time
		resolvedAt  sql.NullTime
		value       sql.NullFloat64
		message     sql.NullString
		payload     sql.NullString
		createdAt   time.Time
	)

	if err := scanner.Scan(
		&id,
		&alertID,
		&status,
		&triggeredAt,
		&resolvedAt,
		&value,
		&message,
		&payload,
		&createdAt,
	); err != nil {
		return nil, err
	}

	payloadMap, err := unmarshalPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to decode history payload: %w", err)
	}

	entry := &models.AlertHistoryEntry{
		ID:          id,
		AlertID:     models.AlertID(alertID),
		Status:      models.AlertStatus(status),
		TriggeredAt: triggeredAt,
		Message:     message.String,
		Payload:     payloadMap,
		CreatedAt:   createdAt,
	}
	if resolvedAt.Valid {
		entry.ResolvedAt = &resolvedAt.Time
	}
	if value.Valid {
		val := value.Float64
		entry.Value = &val
	}
	return entry, nil
}

func nullableString(val string) any {
	if strings.TrimSpace(val) == "" {
		return nil
	}
	return val
}
