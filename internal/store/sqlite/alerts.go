package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mr-karan/logchef/internal/store"
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
	if !alert.IsActive && !db.inTx {
		return db.WithTx(ctx, func(tx store.StoreOps) error { return tx.UpdateAlert(ctx, alert) })
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
	if !alert.IsActive {
		if err := db.writeQueries.InterruptAlertDeliveries(ctx, sqlc.InterruptAlertDeliveriesParams{AlertID: int64(alert.ID)}); err != nil {
			return fmt.Errorf("failed to interrupt alert deliveries: %w", err)
		}
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

// TouchAlertEvaluated records an evaluation without changing the alert state.
func (db *DB) TouchAlertEvaluated(ctx context.Context, alertID models.AlertID) error {
	if err := db.writeQueries.TouchAlertEvaluated(ctx, int64(alertID)); err != nil {
		return fmt.Errorf("failed to touch alert evaluated: %w", err)
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
func (db *DB) InsertAlertHistory(ctx context.Context, alertID models.AlertID, groupKey string, group map[string]string, status models.AlertStatus, value *float64, message string, payload map[string]any) (*models.AlertHistoryEntry, error) {
	payloadJSON, err := marshalPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal history payload: %w", err)
	}
	groupJSON, err := marshalStringMap(group)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal history group: %w", err)
	}

	row, err := db.writeQueries.InsertAlertHistory(ctx, sqlc.InsertAlertHistoryParams{
		AlertID:     int64(alertID),
		GroupKey:    groupKey,
		GroupJson:   nullString(groupJSON),
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

// ListOpenAlertHistory returns every open episode of the alert, newest first.
func (db *DB) ListOpenAlertHistory(ctx context.Context, alertID models.AlertID) ([]*models.AlertHistoryEntry, error) {
	rows, err := db.readQueries.ListOpenAlertHistory(ctx, int64(alertID))
	if err != nil {
		return nil, fmt.Errorf("failed to list open alert history: %w", err)
	}
	return alertHistoryEntriesFromSQLC(rows)
}

// GetAlertHistoryEntry fetches one history entry of an alert.
func (db *DB) GetAlertHistoryEntry(ctx context.Context, alertID models.AlertID, historyID int64) (*models.AlertHistoryEntry, error) {
	row, err := db.readQueries.GetAlertHistoryEntry(ctx, sqlc.GetAlertHistoryEntryParams{ID: historyID, AlertID: int64(alertID)})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get alert history entry: %w", err)
	}
	return alertHistoryFromSQLC(row)
}

// ResolveAlertHistory resolves all firing history for the selected entry's alert.
func (db *DB) ResolveAlertHistory(ctx context.Context, historyID int64, message string) error {
	if !db.inTx {
		return db.WithTx(ctx, func(tx store.StoreOps) error { return tx.ResolveAlertHistory(ctx, historyID, message) })
	}
	alertID, err := db.writeQueries.GetTriggeredAlertHistoryAlertID(ctx, historyID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}
	if err := db.writeQueries.InterruptAlertDeliveries(ctx, sqlc.InterruptAlertDeliveriesParams{AlertID: alertID}); err != nil {
		return fmt.Errorf("failed to interrupt legacy alert deliveries: %w", err)
	}
	if err := db.writeQueries.ResolveAlertHistoryForAlert(ctx, sqlc.ResolveAlertHistoryForAlertParams{AlertID: alertID, Message: nullString(message)}); err != nil {
		return fmt.Errorf("failed to resolve legacy alert history: %w", err)
	}
	if err := db.writeQueries.DeleteAlertInstances(ctx, alertID); err != nil {
		return fmt.Errorf("failed to delete alert instances: %w", err)
	}
	if err := db.writeQueries.MarkAlertResolved(ctx, alertID); err != nil {
		return fmt.Errorf("failed to mark alert resolved: %w", err)
	}
	return nil
}

// ResolveAlertGroup resolves the open episode of one group of the alert.
func (db *DB) ResolveAlertGroup(ctx context.Context, alertID models.AlertID, groupKey, message string) error {
	if !db.inTx {
		return db.WithTx(ctx, func(tx store.StoreOps) error { return tx.ResolveAlertGroup(ctx, alertID, groupKey, message) })
	}
	ids, err := db.writeQueries.ListOpenAlertGroupHistoryIDs(ctx, sqlc.ListOpenAlertGroupHistoryIDsParams{AlertID: int64(alertID), GroupKey: groupKey})
	if err != nil {
		return fmt.Errorf("failed to find open alert episode: %w", err)
	}
	if len(ids) == 0 {
		return models.ErrNotFound
	}
	for _, id := range ids {
		if err := db.writeQueries.InterruptAlertDeliveries(ctx, sqlc.InterruptAlertDeliveriesParams{HistoryID: id}); err != nil {
			return fmt.Errorf("failed to interrupt alert deliveries: %w", err)
		}
	}
	if err := db.writeQueries.ResolveAlertGroupHistory(ctx, sqlc.ResolveAlertGroupHistoryParams{AlertID: int64(alertID), GroupKey: groupKey, Message: nullString(message)}); err != nil {
		return fmt.Errorf("failed to resolve alert episode: %w", err)
	}
	if err := db.writeQueries.DeleteAlertInstance(ctx, sqlc.DeleteAlertInstanceParams{AlertID: int64(alertID), GroupKey: groupKey}); err != nil {
		return fmt.Errorf("failed to delete alert instance: %w", err)
	}
	if err := db.writeQueries.RefreshAlertState(ctx, int64(alertID)); err != nil {
		return fmt.Errorf("failed to refresh alert state: %w", err)
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

	return alertHistoryEntriesFromSQLC(rows)
}

func alertHistoryEntriesFromSQLC(rows []sqlc.AlertHistory) ([]*models.AlertHistoryEntry, error) {
	history := make([]*models.AlertHistoryEntry, 0, len(rows))
	for i := range rows {
		entry, err := alertHistoryFromSQLC(rows[i])
		if err != nil {
			return nil, fmt.Errorf("failed to decode alert history: %w", err)
		}
		history = append(history, entry)
	}
	return history, nil
}

// PruneAlertHistory keeps every open episode and the newest keep closed entries.
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
	groupByJSON, err := marshalStringSlice(alert.GroupBy)
	if err != nil {
		return sqlc.CreateAlertParams{}, fmt.Errorf("failed to marshal group_by: %w", err)
	}

	params := sqlc.CreateAlertParams{
		SourceID:             int64(alert.SourceID),
		Name:                 alert.Name,
		Description:          nullString(alert.Description),
		QueryLanguage:        string(alert.QueryLanguage),
		EditorMode:           string(alert.EditorMode),
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
		GroupByJson:          nullString(groupByJSON),
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
		QueryLanguage:        createParams.QueryLanguage,
		EditorMode:           createParams.EditorMode,
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
		GroupByJson:          createParams.GroupByJson,
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
	groupBy, err := unmarshalStringSlice(row.GroupByJson)
	if err != nil {
		return nil, fmt.Errorf("failed to decode group_by: %w", err)
	}

	alert := &models.Alert{
		ID:                models.AlertID(row.ID),
		SourceID:          models.SourceID(row.SourceID),
		Name:              row.Name,
		Description:       row.Description.String,
		QueryLanguage:     models.QueryLanguage(row.QueryLanguage),
		EditorMode:        models.NormalizeAlertEditorMode(models.AlertEditorMode(row.EditorMode)),
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
		GroupBy:           groupBy,
		IsActive:          row.IsActive == 1,
		LastState:         models.AlertState(row.LastState),
		EvalSeq:           row.EvalSeq,
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

	group, err := unmarshalStringMap(row.GroupJson)
	if err != nil {
		return nil, fmt.Errorf("failed to decode history group: %w", err)
	}

	entry := &models.AlertHistoryEntry{
		ID:          row.ID,
		AlertID:     models.AlertID(row.AlertID),
		GroupKey:    row.GroupKey,
		Group:       group,
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

// RecoverAlertDeliveries finalizes delivery reservations left by a stopped manager.
func (db *DB) RecoverAlertDeliveries(ctx context.Context) error {
	if err := db.writeQueries.InterruptAlertDeliveries(ctx, sqlc.InterruptAlertDeliveriesParams{}); err != nil {
		return fmt.Errorf("failed to recover alert deliveries: %w", err)
	}
	return nil
}

// AdvanceAlertEvalSeq increments eval_seq when it still equals seq.
func (db *DB) AdvanceAlertEvalSeq(ctx context.Context, alertID models.AlertID, seq int64) error {
	if _, err := db.writeQueries.AdvanceAlertEvalSeq(ctx, sqlc.AdvanceAlertEvalSeqParams{ID: int64(alertID), EvalSeq: seq}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrConflict
		}
		return fmt.Errorf("failed to advance alert eval_seq: %w", err)
	}
	return nil
}

// ListAlertInstances returns the alert's instances ordered by group key.
func (db *DB) ListAlertInstances(ctx context.Context, alertID models.AlertID) ([]*models.AlertInstance, error) {
	rows, err := db.readQueries.ListAlertInstances(ctx, int64(alertID))
	if err != nil {
		return nil, fmt.Errorf("failed to list alert instances: %w", err)
	}
	instances := make([]*models.AlertInstance, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		group, err := alertjson.Decode[map[string]string](row.GroupJson)
		if err != nil {
			return nil, fmt.Errorf("failed to decode alert instance group: %w", err)
		}
		instance := &models.AlertInstance{
			AlertID:    models.AlertID(row.AlertID),
			GroupKey:   row.GroupKey,
			Group:      group,
			State:      models.AlertInstanceState(row.State),
			StateSince: row.StateSince,
			Notified:   row.Notified == 1,
		}
		if row.Value.Valid {
			value := row.Value.Float64
			instance.Value = &value
		}
		if row.LastSeenAt.Valid {
			seen := row.LastSeenAt.Time
			instance.LastSeenAt = &seen
		}
		if row.HistoryID.Valid {
			historyID := row.HistoryID.Int64
			instance.HistoryID = &historyID
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

// CountAlertInstances returns the instance counts of each given alert that has instances.
func (db *DB) CountAlertInstances(ctx context.Context, alertIDs []models.AlertID) (map[models.AlertID]models.AlertInstanceCounts, error) {
	counts := make(map[models.AlertID]models.AlertInstanceCounts)
	if len(alertIDs) == 0 {
		return counts, nil
	}
	ids := make([]int64, len(alertIDs))
	for i, id := range alertIDs {
		ids[i] = int64(id)
	}
	rows, err := db.readQueries.CountAlertInstancesByState(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to count alert instances: %w", err)
	}
	for _, row := range rows {
		alertCounts := counts[models.AlertID(row.AlertID)]
		alertCounts.Add(models.AlertInstanceState(row.State), int(row.Instances))
		counts[models.AlertID(row.AlertID)] = alertCounts
	}
	return counts, nil
}

// UpsertAlertInstance inserts or replaces the instance of (alert, group).
func (db *DB) UpsertAlertInstance(ctx context.Context, instance *models.AlertInstance) error {
	groupJSON, err := alertjson.EncodeGroup(instance.Group)
	if err != nil {
		return fmt.Errorf("failed to marshal alert instance group: %w", err)
	}
	params := sqlc.UpsertAlertInstanceParams{
		AlertID:    int64(instance.AlertID),
		GroupKey:   instance.GroupKey,
		GroupJson:  groupJSON,
		State:      string(instance.State),
		Value:      nullFloat64(instance.Value),
		StateSince: instance.StateSince.UTC(),
		Notified:   boolToInt(instance.Notified),
	}
	if instance.LastSeenAt != nil {
		params.LastSeenAt = sql.NullTime{Time: instance.LastSeenAt.UTC(), Valid: true}
	}
	if instance.HistoryID != nil {
		params.HistoryID = sql.NullInt64{Int64: *instance.HistoryID, Valid: true}
	}
	if err := db.writeQueries.UpsertAlertInstance(ctx, params); err != nil {
		return fmt.Errorf("failed to upsert alert instance: %w", err)
	}
	return nil
}

// MarkAlertInstanceNotified sets notified on the instance whose open episode is historyID.
func (db *DB) MarkAlertInstanceNotified(ctx context.Context, alertID models.AlertID, groupKey string, historyID int64) error {
	if _, err := db.writeQueries.MarkAlertInstanceNotified(ctx, sqlc.MarkAlertInstanceNotifiedParams{
		AlertID:   int64(alertID),
		GroupKey:  groupKey,
		HistoryID: sql.NullInt64{Int64: historyID, Valid: true},
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to mark alert instance notified: %w", err)
	}
	return nil
}
