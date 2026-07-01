package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/alertjson"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// Alert JSON columns are (un)marshalled via the shared alertjson codec; only the
// NULL extraction (sql.NullString.String is "" when NULL) is backend-specific.
func marshalStringMap(m map[string]string) (string, error) {
	return alertjson.Encode(m, len(m) == 0)
}

func unmarshalStringMap(raw sql.NullString) (map[string]string, error) {
	return alertjson.Decode[map[string]string](raw.String)
}

func marshalUserIDs(ids []models.UserID) (string, error) {
	return alertjson.Encode(ids, len(ids) == 0)
}

func unmarshalUserIDs(raw sql.NullString) ([]models.UserID, error) {
	return alertjson.Decode[[]models.UserID](raw.String)
}

func marshalStringSlice(values []string) (string, error) {
	return alertjson.Encode(values, len(values) == 0)
}

func unmarshalStringSlice(raw sql.NullString) ([]string, error) {
	return alertjson.Decode[[]string](raw.String)
}

func unmarshalPayload(raw sql.NullString) (map[string]any, error) {
	return alertjson.Decode[map[string]any](raw.String)
}

// CreateAlert inserts a new alert definition. Alerts are scoped to a single
// source; visibility is governed by source membership at the application layer.
func (db *DB) CreateAlert(ctx context.Context, alert *models.Alert) error {
	if alert == nil {
		return fmt.Errorf("alert payload is required")
	}

	params, err := alertCreateParams(alert)
	if err != nil {
		return err
	}
	row, err := db.writeQueries.CreateAlert(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to insert alert: %w", err)
	}

	created, err := alertFromSQLC(row)
	if err != nil {
		return fmt.Errorf("failed to decode created alert: %w", err)
	}
	*alert = *created
	return nil
}

// UpdateAlert persists changes to an existing alert definition.
func (db *DB) UpdateAlert(ctx context.Context, alert *models.Alert) error {
	if alert == nil {
		return fmt.Errorf("alert payload is required")
	}

	params, err := alertUpdateParams(alert)
	if err != nil {
		return err
	}
	if _, err := db.writeQueries.UpdateAlert(ctx, params); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to update alert: %w", err)
	}
	return nil
}

// DeleteAlert removes an alert definition.
func (db *DB) DeleteAlert(ctx context.Context, alertID models.AlertID) error {
	if _, err := db.writeQueries.DeleteAlert(ctx, int64(alertID)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to delete alert: %w", err)
	}
	return nil
}

// GetAlert retrieves an alert by ID.
func (db *DB) GetAlert(ctx context.Context, alertID models.AlertID) (*models.Alert, error) {
	row, err := db.readQueries.GetAlert(ctx, int64(alertID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get alert: %w", err)
	}
	return alertFromSQLC(row)
}

// ListAlertsBySource returns alerts for one source.
func (db *DB) ListAlertsBySource(ctx context.Context, sourceID models.SourceID) ([]*models.Alert, error) {
	rows, err := db.readQueries.ListAlertsBySource(ctx, int64(sourceID))
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}
	return alertsFromSQLC(rows)
}

// ListAlertsForUser returns alerts the user can see (cross-source via team membership).
func (db *DB) ListAlertsForUser(ctx context.Context, userID models.UserID) ([]*models.Alert, error) {
	rows, err := db.readQueries.ListAlertsForUser(ctx, int64(userID))
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts for user: %w", err)
	}
	return alertsFromSQLC(rows)
}

// ListActiveAlertsDue returns alerts that need evaluation.
func (db *DB) ListActiveAlertsDue(ctx context.Context) ([]*models.Alert, error) {
	rows, err := db.readQueries.ListActiveAlertsDue(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list active alerts: %w", err)
	}
	return alertsFromSQLC(rows)
}

// MarkAlertEvaluated updates state after evaluation finishes without triggering.
func (db *DB) MarkAlertEvaluated(ctx context.Context, alertID models.AlertID) error {
	if err := db.writeQueries.MarkAlertEvaluated(ctx, int64(alertID)); err != nil {
		return fmt.Errorf("failed to mark alert evaluated: %w", err)
	}
	return nil
}

// MarkAlertTriggered updates state when an alert fires.
func (db *DB) MarkAlertTriggered(ctx context.Context, alertID models.AlertID) error {
	if err := db.writeQueries.MarkAlertTriggered(ctx, int64(alertID)); err != nil {
		return fmt.Errorf("failed to mark alert triggered: %w", err)
	}
	return nil
}

// InsertAlertHistory records a history entry and returns the hydrated entry.
func (db *DB) InsertAlertHistory(ctx context.Context, alertID models.AlertID, status models.AlertStatus, value *float64, message string, payload map[string]any) (*models.AlertHistoryEntry, error) {
	payloadJSON, err := marshalPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal history payload: %w", err)
	}

	row, err := db.writeQueries.InsertAlertHistory(ctx, sqlc.InsertAlertHistoryParams{
		AlertID:     int64(alertID),
		Status:      string(status),
		Value:       nullFloat64(value),
		Message:     nullString(message),
		PayloadJson: nullString(payloadJSON),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to insert alert history: %w", err)
	}
	return alertHistoryFromSQLC(row)
}

// GetLatestUnresolvedAlertHistory fetches the newest unresolved history entry.
func (db *DB) GetLatestUnresolvedAlertHistory(ctx context.Context, alertID models.AlertID) (*models.AlertHistoryEntry, error) {
	row, err := db.readQueries.GetLatestUnresolvedAlertHistory(ctx, int64(alertID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get alert history: %w", err)
	}
	return alertHistoryFromSQLC(row)
}

// ResolveAlertHistory marks a history entry as resolved with an optional message.
func (db *DB) ResolveAlertHistory(ctx context.Context, historyID int64, message string) error {
	if _, err := db.writeQueries.ResolveAlertHistory(ctx, sqlc.ResolveAlertHistoryParams{
		Message: nullString(message),
		ID:      historyID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}
	return nil
}

// UpdateAlertHistoryPayload updates the payload of an existing alert history entry.
func (db *DB) UpdateAlertHistoryPayload(ctx context.Context, historyID int64, payload map[string]any) error {
	payloadJSON, err := marshalPayload(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal history payload: %w", err)
	}

	if _, err := db.writeQueries.UpdateAlertHistoryPayload(ctx, sqlc.UpdateAlertHistoryPayloadParams{
		PayloadJson: nullString(payloadJSON),
		ID:          historyID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to update alert history payload: %w", err)
	}
	return nil
}

// ListAlertHistory returns recent history entries for an alert.
func (db *DB) ListAlertHistory(ctx context.Context, alertID models.AlertID, limit int) ([]*models.AlertHistoryEntry, error) {
	rows, err := db.readQueries.ListAlertHistory(ctx, sqlc.ListAlertHistoryParams{
		AlertID: int64(alertID),
		Limit:   int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list alert history: %w", err)
	}

	history := make([]*models.AlertHistoryEntry, 0, len(rows))
	for i := range rows {
		row := rows[i]
		entry, err := alertHistoryFromSQLC(row)
		if err != nil {
			return nil, fmt.Errorf("failed to decode alert history: %w", err)
		}
		history = append(history, entry)
	}
	return history, nil
}

// PruneAlertHistory keeps the most recent N entries for an alert.
func (db *DB) PruneAlertHistory(ctx context.Context, alertID models.AlertID, keep int) error {
	if keep <= 0 {
		return nil
	}
	if err := db.writeQueries.PruneAlertHistory(ctx, sqlc.PruneAlertHistoryParams{
		AlertID:   int64(alertID),
		AlertID_2: int64(alertID),
		Limit:     int64(keep),
	}); err != nil {
		return fmt.Errorf("failed to prune alert history: %w", err)
	}
	return nil
}

func alertCreateParams(alert *models.Alert) (sqlc.CreateAlertParams, error) {
	labelsJSON, err := marshalStringMap(alert.Labels)
	if err != nil {
		return sqlc.CreateAlertParams{}, fmt.Errorf("failed to marshal labels: %w", err)
	}
	annotationsJSON, err := marshalStringMap(alert.Annotations)
	if err != nil {
		return sqlc.CreateAlertParams{}, fmt.Errorf("failed to marshal annotations: %w", err)
	}
	recipientUserIDsJSON, err := marshalUserIDs(alert.RecipientUserIDs)
	if err != nil {
		return sqlc.CreateAlertParams{}, fmt.Errorf("failed to marshal recipient user IDs: %w", err)
	}
	webhookURLsJSON, err := marshalStringSlice(alert.WebhookURLs)
	if err != nil {
		return sqlc.CreateAlertParams{}, fmt.Errorf("failed to marshal webhook URLs: %w", err)
	}

	params := sqlc.CreateAlertParams{
		SourceID:             int64(alert.SourceID),
		Name:                 alert.Name,
		Description:          nullString(alert.Description),
		QueryType:            string(alert.QueryType),
		Query:                nullString(alert.Query),
		ConditionJson:        nullString(alert.ConditionJSON),
		LookbackSeconds:      int64(alert.LookbackSeconds),
		ThresholdOperator:    string(alert.ThresholdOperator),
		ThresholdValue:       alert.ThresholdValue,
		FrequencySeconds:     int64(alert.FrequencySeconds),
		Severity:             string(alert.Severity),
		LabelsJson:           nullString(labelsJSON),
		AnnotationsJson:      nullString(annotationsJSON),
		RecipientUserIdsJson: nullString(recipientUserIDsJSON),
		WebhookUrlsJson:      nullString(webhookURLsJSON),
		GeneratorUrl:         nullString(alert.GeneratorURL),
		IsActive:             boolToInt(alert.IsActive),
	}
	if alert.CreatedBy != nil {
		params.CreatedBy = sql.NullInt64{Int64: int64(*alert.CreatedBy), Valid: true}
	}
	return params, nil
}

func alertUpdateParams(alert *models.Alert) (sqlc.UpdateAlertParams, error) {
	createParams, err := alertCreateParams(alert)
	if err != nil {
		return sqlc.UpdateAlertParams{}, err
	}
	return sqlc.UpdateAlertParams{
		Name:                 createParams.Name,
		Description:          createParams.Description,
		QueryType:            createParams.QueryType,
		Query:                createParams.Query,
		ConditionJson:        createParams.ConditionJson,
		LookbackSeconds:      createParams.LookbackSeconds,
		ThresholdOperator:    createParams.ThresholdOperator,
		ThresholdValue:       createParams.ThresholdValue,
		FrequencySeconds:     createParams.FrequencySeconds,
		Severity:             createParams.Severity,
		LabelsJson:           createParams.LabelsJson,
		AnnotationsJson:      createParams.AnnotationsJson,
		RecipientUserIdsJson: createParams.RecipientUserIdsJson,
		WebhookUrlsJson:      createParams.WebhookUrlsJson,
		GeneratorUrl:         createParams.GeneratorUrl,
		IsActive:             createParams.IsActive,
		ID:                   int64(alert.ID),
	}, nil
}

func alertsFromSQLC(rows []sqlc.Alert) ([]*models.Alert, error) {
	alerts := make([]*models.Alert, 0, len(rows))
	for i := range rows {
		row := rows[i]
		alert, err := alertFromSQLC(row)
		if err != nil {
			return nil, fmt.Errorf("failed to decode alert: %w", err)
		}
		alerts = append(alerts, alert)
	}
	return alerts, nil
}

func alertFromSQLC(row sqlc.Alert) (*models.Alert, error) {
	labels, err := unmarshalStringMap(row.LabelsJson)
	if err != nil {
		return nil, fmt.Errorf("failed to decode labels: %w", err)
	}
	annotations, err := unmarshalStringMap(row.AnnotationsJson)
	if err != nil {
		return nil, fmt.Errorf("failed to decode annotations: %w", err)
	}
	recipientUserIDs, err := unmarshalUserIDs(row.RecipientUserIdsJson)
	if err != nil {
		return nil, fmt.Errorf("failed to decode recipient user IDs: %w", err)
	}
	webhookURLs, err := unmarshalStringSlice(row.WebhookUrlsJson)
	if err != nil {
		return nil, fmt.Errorf("failed to decode webhook URLs: %w", err)
	}

	alert := &models.Alert{
		ID:                models.AlertID(row.ID),
		SourceID:          models.SourceID(row.SourceID),
		Name:              row.Name,
		Description:       row.Description.String,
		QueryType:         models.AlertQueryType(row.QueryType),
		Query:             row.Query.String,
		ConditionJSON:     row.ConditionJson.String,
		LookbackSeconds:   int(row.LookbackSeconds),
		ThresholdOperator: models.AlertThresholdOperator(row.ThresholdOperator),
		ThresholdValue:    row.ThresholdValue,
		FrequencySeconds:  int(row.FrequencySeconds),
		Severity:          models.AlertSeverity(row.Severity),
		Labels:            labels,
		Annotations:       annotations,
		RecipientUserIDs:  recipientUserIDs,
		WebhookURLs:       webhookURLs,
		GeneratorURL:      row.GeneratorUrl.String,
		IsActive:          row.IsActive == 1,
		LastState:         models.AlertState(row.LastState),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
	if row.LastEvaluatedAt.Valid {
		alert.LastEvaluatedAt = &row.LastEvaluatedAt.Time
	}
	if row.LastTriggeredAt.Valid {
		alert.LastTriggeredAt = &row.LastTriggeredAt.Time
	}
	if row.CreatedBy.Valid {
		uid := models.UserID(row.CreatedBy.Int64)
		alert.CreatedBy = &uid
	}
	return alert, nil
}

func alertHistoryFromSQLC(row sqlc.AlertHistory) (*models.AlertHistoryEntry, error) {
	payloadMap, err := unmarshalPayload(row.PayloadJson)
	if err != nil {
		return nil, fmt.Errorf("failed to decode history payload: %w", err)
	}

	entry := &models.AlertHistoryEntry{
		ID:          row.ID,
		AlertID:     models.AlertID(row.AlertID),
		Status:      models.AlertStatus(row.Status),
		TriggeredAt: row.TriggeredAt,
		Message:     row.Message.String,
		Payload:     payloadMap,
		CreatedAt:   row.CreatedAt,
	}
	if row.ResolvedAt.Valid {
		entry.ResolvedAt = &row.ResolvedAt.Time
	}
	if row.Value.Valid {
		val := row.Value.Float64
		entry.Value = &val
	}
	return entry, nil
}

func marshalPayload(payload map[string]any) (string, error) {
	return alertjson.Encode(payload, len(payload) == 0)
}

func nullFloat64(value *float64) sql.NullFloat64 {
	if value == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}
