package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mr-karan/logchef/internal/store/alertjson"
	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// CreateAlert inserts a new alert definition and hydrates the model from the row.
func (s *Store) CreateAlert(ctx context.Context, alert *models.Alert) error {
	if alert == nil {
		return fmt.Errorf("alert payload is required")
	}
	params, err := alertCreateParams(alert)
	if err != nil {
		return err
	}
	row, err := s.q.CreateAlert(ctx, params)
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
func (s *Store) UpdateAlert(ctx context.Context, alert *models.Alert) error {
	if alert == nil {
		return fmt.Errorf("alert payload is required")
	}
	createParams, err := alertCreateParams(alert)
	if err != nil {
		return err
	}
	_, err = s.q.UpdateAlert(ctx, sqlc.UpdateAlertParams{
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
	})
	if err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to update alert: %w", err)
	}
	return nil
}

// DeleteAlert removes an alert definition.
func (s *Store) DeleteAlert(ctx context.Context, alertID models.AlertID) error {
	if _, err := s.q.DeleteAlert(ctx, int64(alertID)); err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to delete alert: %w", err)
	}
	return nil
}

// GetAlert retrieves an alert by ID. Returns models.ErrNotFound if absent.
func (s *Store) GetAlert(ctx context.Context, alertID models.AlertID) (*models.Alert, error) {
	row, err := s.q.GetAlert(ctx, int64(alertID))
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get alert: %w", err)
	}
	return alertFromSQLC(row)
}

// ListAlertsBySource returns alerts for one source.
func (s *Store) ListAlertsBySource(ctx context.Context, sourceID models.SourceID) ([]*models.Alert, error) {
	rows, err := s.q.ListAlertsBySource(ctx, int64(sourceID))
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}
	return alertsFromSQLC(rows)
}

// ListAlertsForUser returns alerts the user can see via team membership.
func (s *Store) ListAlertsForUser(ctx context.Context, userID models.UserID) ([]*models.Alert, error) {
	rows, err := s.q.ListAlertsForUser(ctx, int64(userID))
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts for user: %w", err)
	}
	return alertsFromSQLC(rows)
}

// ListActiveAlertsDue returns alerts that need evaluation.
func (s *Store) ListActiveAlertsDue(ctx context.Context) ([]*models.Alert, error) {
	rows, err := s.q.ListActiveAlertsDue(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list active alerts: %w", err)
	}
	return alertsFromSQLC(rows)
}

// MarkAlertEvaluated updates state after evaluation finishes without triggering.
func (s *Store) MarkAlertEvaluated(ctx context.Context, alertID models.AlertID) error {
	if err := s.q.MarkAlertEvaluated(ctx, int64(alertID)); err != nil {
		return fmt.Errorf("failed to mark alert evaluated: %w", err)
	}
	return nil
}

// MarkAlertTriggered updates state when an alert fires.
func (s *Store) MarkAlertTriggered(ctx context.Context, alertID models.AlertID) error {
	if err := s.q.MarkAlertTriggered(ctx, int64(alertID)); err != nil {
		return fmt.Errorf("failed to mark alert triggered: %w", err)
	}
	return nil
}

// InsertAlertHistory records a history entry and returns the hydrated entry.
func (s *Store) InsertAlertHistory(ctx context.Context, alertID models.AlertID, status models.AlertStatus, value *float64, message string, payload map[string]any) (*models.AlertHistoryEntry, error) {
	payloadJSON, err := marshalPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal history payload: %w", err)
	}
	row, err := s.q.InsertAlertHistory(ctx, sqlc.InsertAlertHistoryParams{
		AlertID:     int64(alertID),
		Status:      string(status),
		Value:       float8FromPtr(value),
		Message:     text(message),
		PayloadJson: text(payloadJSON),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to insert alert history: %w", err)
	}
	return alertHistoryFromSQLC(row)
}

// GetLatestUnresolvedAlertHistory fetches the newest unresolved history entry.
func (s *Store) GetLatestUnresolvedAlertHistory(ctx context.Context, alertID models.AlertID) (*models.AlertHistoryEntry, error) {
	row, err := s.q.GetLatestUnresolvedAlertHistory(ctx, int64(alertID))
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get alert history: %w", err)
	}
	return alertHistoryFromSQLC(row)
}

// ResolveAlertHistory marks a history entry resolved with an optional message.
func (s *Store) ResolveAlertHistory(ctx context.Context, historyID int64, message string) error {
	if _, err := s.q.ResolveAlertHistory(ctx, sqlc.ResolveAlertHistoryParams{
		Message: text(message),
		ID:      historyID,
	}); err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}
	return nil
}

// UpdateAlertHistoryPayload updates the payload of an existing history entry.
func (s *Store) UpdateAlertHistoryPayload(ctx context.Context, historyID int64, payload map[string]any) error {
	payloadJSON, err := marshalPayload(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal history payload: %w", err)
	}
	if _, err := s.q.UpdateAlertHistoryPayload(ctx, sqlc.UpdateAlertHistoryPayloadParams{
		PayloadJson: text(payloadJSON),
		ID:          historyID,
	}); err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to update alert history payload: %w", err)
	}
	return nil
}

// ListAlertHistory returns recent history entries for an alert.
func (s *Store) ListAlertHistory(ctx context.Context, alertID models.AlertID, limit int) ([]*models.AlertHistoryEntry, error) {
	rows, err := s.q.ListAlertHistory(ctx, sqlc.ListAlertHistoryParams{
		AlertID: int64(alertID),
		Limit:   int32(limit), //nolint:gosec // G115: query limit, small bounded value
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
func (s *Store) PruneAlertHistory(ctx context.Context, alertID models.AlertID, keep int) error {
	if keep <= 0 {
		return nil
	}
	if err := s.q.PruneAlertHistory(ctx, sqlc.PruneAlertHistoryParams{
		AlertID:   int64(alertID),
		AlertID_2: int64(alertID),
		Limit:     int32(keep), //nolint:gosec // G115: retention count, small bounded value
	}); err != nil {
		return fmt.Errorf("failed to prune alert history: %w", err)
	}
	return nil
}

// --- mapping + json helpers --------------------------------------------------

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
		Description:          text(alert.Description),
		QueryType:            string(alert.QueryType),
		Query:                text(alert.Query),
		ConditionJson:        text(alert.ConditionJSON),
		LookbackSeconds:      int64(alert.LookbackSeconds),
		ThresholdOperator:    string(alert.ThresholdOperator),
		ThresholdValue:       alert.ThresholdValue,
		FrequencySeconds:     int64(alert.FrequencySeconds),
		Severity:             string(alert.Severity),
		LabelsJson:           text(labelsJSON),
		AnnotationsJson:      text(annotationsJSON),
		RecipientUserIdsJson: text(recipientUserIDsJSON),
		WebhookUrlsJson:      text(webhookURLsJSON),
		GeneratorUrl:         text(alert.GeneratorURL),
		IsActive:             alert.IsActive,
	}
	if alert.CreatedBy != nil {
		params.CreatedBy = int8Val(int64(*alert.CreatedBy))
	}
	return params, nil
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
		Description:       textStr(row.Description),
		QueryType:         models.AlertQueryType(row.QueryType),
		Query:             textStr(row.Query),
		ConditionJSON:     textStr(row.ConditionJson),
		LookbackSeconds:   int(row.LookbackSeconds),
		ThresholdOperator: models.AlertThresholdOperator(row.ThresholdOperator),
		ThresholdValue:    row.ThresholdValue,
		FrequencySeconds:  int(row.FrequencySeconds),
		Severity:          models.AlertSeverity(row.Severity),
		Labels:            labels,
		Annotations:       annotations,
		RecipientUserIDs:  recipientUserIDs,
		WebhookURLs:       webhookURLs,
		GeneratorURL:      textStr(row.GeneratorUrl),
		IsActive:          row.IsActive,
		LastState:         models.AlertState(row.LastState),
		LastEvaluatedAt:   tsPtr(row.LastEvaluatedAt),
		LastTriggeredAt:   tsPtr(row.LastTriggeredAt),
		CreatedBy:         userIDPtr(row.CreatedBy),
		CreatedAt:         row.CreatedAt.Time,
		UpdatedAt:         row.UpdatedAt.Time,
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
		TriggeredAt: row.TriggeredAt.Time,
		ResolvedAt:  tsPtr(row.ResolvedAt),
		Message:     textStr(row.Message),
		Payload:     payloadMap,
		CreatedAt:   row.CreatedAt.Time,
	}
	if row.Value.Valid {
		val := row.Value.Float64
		entry.Value = &val
	}
	return entry, nil
}

// Alert JSON columns are (un)marshalled via the shared alertjson codec; only the
// NULL extraction (pgtype.Text via textStr) is backend-specific.
func marshalStringMap(m map[string]string) (string, error) {
	return alertjson.Encode(m, len(m) == 0)
}

func unmarshalStringMap(raw pgtype.Text) (map[string]string, error) {
	return alertjson.Decode[map[string]string](textStr(raw))
}

func marshalUserIDs(ids []models.UserID) (string, error) {
	return alertjson.Encode(ids, len(ids) == 0)
}

func unmarshalUserIDs(raw pgtype.Text) ([]models.UserID, error) {
	return alertjson.Decode[[]models.UserID](textStr(raw))
}

func marshalStringSlice(values []string) (string, error) {
	return alertjson.Encode(values, len(values) == 0)
}

func unmarshalStringSlice(raw pgtype.Text) ([]string, error) {
	return alertjson.Decode[[]string](textStr(raw))
}

func marshalPayload(payload map[string]any) (string, error) {
	return alertjson.Encode(payload, len(payload) == 0)
}

func unmarshalPayload(raw pgtype.Text) (map[string]any, error) {
	return alertjson.Decode[map[string]any](textStr(raw))
}

func float8FromPtr(v *float64) pgtype.Float8 {
	if v == nil {
		return pgtype.Float8{}
	}
	return pgtype.Float8{Float64: *v, Valid: true}
}
