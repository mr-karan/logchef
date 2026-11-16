package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mr-karan/logchef/internal/store"
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
	if !alert.IsActive && s.pool != nil {
		return s.WithTx(ctx, func(tx store.StoreOps) error { return tx.UpdateAlert(ctx, alert) })
	}
	createParams, err := alertCreateParams(alert)
	if err != nil {
		return err
	}
	_, err = s.q.UpdateAlert(ctx, sqlc.UpdateAlertParams{
		Name:              createParams.Name,
		Description:       createParams.Description,
		QueryLanguage:     createParams.QueryLanguage,
		EditorMode:        createParams.EditorMode,
		Query:             createParams.Query,
		ConditionJson:     createParams.ConditionJson,
		LookbackSeconds:   createParams.LookbackSeconds,
		ThresholdOperator: createParams.ThresholdOperator,
		ThresholdValue:    createParams.ThresholdValue,
		FrequencySeconds:  createParams.FrequencySeconds,
		Severity:          createParams.Severity,
		LabelsJson:        createParams.LabelsJson,
		AnnotationsJson:   createParams.AnnotationsJson,
		GeneratorUrl:      createParams.GeneratorUrl,
		GroupByJson:       createParams.GroupByJson,
		IsActive:          createParams.IsActive,
		ID:                int64(alert.ID),
	})
	if err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to update alert: %w", err)
	}
	if !alert.IsActive {
		if err := s.q.InterruptAlertDeliveries(ctx, sqlc.InterruptAlertDeliveriesParams{AlertID: int64(alert.ID)}); err != nil {
			return fmt.Errorf("failed to interrupt alert deliveries: %w", err)
		}
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
	alert, err := alertFromSQLC(row)
	if err != nil {
		return nil, err
	}
	return alert, s.attachDestinationIDs(ctx, alert)
}

// ListAlertsBySource returns alerts for one source.
func (s *Store) ListAlertsBySource(ctx context.Context, sourceID models.SourceID) ([]*models.Alert, error) {
	rows, err := s.q.ListAlertsBySource(ctx, int64(sourceID))
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}
	return s.alertsWithDestinations(ctx, rows)
}

// ListAlertsForUser returns alerts the user can see via team membership.
func (s *Store) ListAlertsForUser(ctx context.Context, userID models.UserID) ([]*models.Alert, error) {
	rows, err := s.q.ListAlertsForUser(ctx, int64(userID))
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts for user: %w", err)
	}
	return s.alertsWithDestinations(ctx, rows)
}

func (s *Store) alertsWithDestinations(ctx context.Context, rows []sqlc.Alert) ([]*models.Alert, error) {
	alerts, err := alertsFromSQLC(rows)
	if err != nil {
		return nil, err
	}
	return alerts, s.attachDestinationIDs(ctx, alerts...)
}

// MarkAlertEvaluated updates state after evaluation finishes without triggering.
func (s *Store) MarkAlertEvaluated(ctx context.Context, alertID models.AlertID) error {
	if err := s.q.MarkAlertEvaluated(ctx, int64(alertID)); err != nil {
		return fmt.Errorf("failed to mark alert evaluated: %w", err)
	}
	return nil
}

// TouchAlertEvaluated records an evaluation without changing the alert state.
func (s *Store) TouchAlertEvaluated(ctx context.Context, alertID models.AlertID) error {
	if err := s.q.TouchAlertEvaluated(ctx, int64(alertID)); err != nil {
		return fmt.Errorf("failed to touch alert evaluated: %w", err)
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
func (s *Store) InsertAlertHistory(ctx context.Context, alertID models.AlertID, groupKey string, group map[string]string, status models.AlertStatus, value *float64, message string, payload map[string]any) (*models.AlertHistoryEntry, error) {
	payloadJSON, err := marshalPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal history payload: %w", err)
	}
	groupJSON, err := marshalStringMap(group)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal history group: %w", err)
	}
	row, err := s.q.InsertAlertHistory(ctx, sqlc.InsertAlertHistoryParams{
		AlertID:     int64(alertID),
		GroupKey:    groupKey,
		GroupJson:   text(groupJSON),
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

// ListOpenAlertHistory returns every open episode of the alert, newest first.
func (s *Store) ListOpenAlertHistory(ctx context.Context, alertID models.AlertID) ([]*models.AlertHistoryEntry, error) {
	rows, err := s.q.ListOpenAlertHistory(ctx, int64(alertID))
	if err != nil {
		return nil, fmt.Errorf("failed to list open alert history: %w", err)
	}
	return alertHistoryEntriesFromSQLC(rows)
}

// GetAlertHistoryEntry fetches one history entry of an alert.
func (s *Store) GetAlertHistoryEntry(ctx context.Context, alertID models.AlertID, historyID int64) (*models.AlertHistoryEntry, error) {
	row, err := s.q.GetAlertHistoryEntry(ctx, sqlc.GetAlertHistoryEntryParams{ID: historyID, AlertID: int64(alertID)})
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get alert history entry: %w", err)
	}
	return alertHistoryFromSQLC(row)
}

// ResolveAlertHistory resolves all firing history for the selected entry's alert.
func (s *Store) ResolveAlertHistory(ctx context.Context, historyID int64, message string) error {
	if s.pool != nil {
		return s.WithTx(ctx, func(tx store.StoreOps) error { return tx.ResolveAlertHistory(ctx, historyID, message) })
	}
	alertID, err := s.q.GetTriggeredAlertHistoryAlertID(ctx, historyID)
	if err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}
	if err := s.q.InterruptAlertDeliveries(ctx, sqlc.InterruptAlertDeliveriesParams{AlertID: alertID}); err != nil {
		return fmt.Errorf("failed to interrupt legacy alert deliveries: %w", err)
	}
	if err := s.q.ResolveAlertHistoryForAlert(ctx, sqlc.ResolveAlertHistoryForAlertParams{AlertID: alertID, Message: text(message)}); err != nil {
		return fmt.Errorf("failed to resolve legacy alert history: %w", err)
	}
	if err := s.q.DeleteAlertInstances(ctx, alertID); err != nil {
		return fmt.Errorf("failed to delete alert instances: %w", err)
	}
	if err := s.q.MarkAlertResolved(ctx, alertID); err != nil {
		return fmt.Errorf("failed to mark alert resolved: %w", err)
	}
	return nil
}

// ResolveAlertGroup resolves the open episode of one group of the alert.
func (s *Store) ResolveAlertGroup(ctx context.Context, alertID models.AlertID, groupKey, message string) error {
	if s.pool != nil {
		return s.WithTx(ctx, func(tx store.StoreOps) error { return tx.ResolveAlertGroup(ctx, alertID, groupKey, message) })
	}
	ids, err := s.q.ListOpenAlertGroupHistoryIDs(ctx, sqlc.ListOpenAlertGroupHistoryIDsParams{AlertID: int64(alertID), GroupKey: groupKey})
	if err != nil {
		return fmt.Errorf("failed to find open alert episode: %w", err)
	}
	if len(ids) == 0 {
		return models.ErrNotFound
	}
	for _, id := range ids {
		if err := s.q.InterruptAlertDeliveries(ctx, sqlc.InterruptAlertDeliveriesParams{HistoryID: id}); err != nil {
			return fmt.Errorf("failed to interrupt alert deliveries: %w", err)
		}
	}
	if err := s.q.ResolveAlertGroupHistory(ctx, sqlc.ResolveAlertGroupHistoryParams{AlertID: int64(alertID), GroupKey: groupKey, Message: text(message)}); err != nil {
		return fmt.Errorf("failed to resolve alert episode: %w", err)
	}
	if err := s.q.DeleteAlertInstance(ctx, sqlc.DeleteAlertInstanceParams{AlertID: int64(alertID), GroupKey: groupKey}); err != nil {
		return fmt.Errorf("failed to delete alert instance: %w", err)
	}
	if err := s.q.RefreshAlertState(ctx, int64(alertID)); err != nil {
		return fmt.Errorf("failed to refresh alert state: %w", err)
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
	groupByJSON, err := marshalStringSlice(alert.GroupBy)
	if err != nil {
		return sqlc.CreateAlertParams{}, fmt.Errorf("failed to marshal group_by: %w", err)
	}

	params := sqlc.CreateAlertParams{
		SourceID:          int64(alert.SourceID),
		Name:              alert.Name,
		Description:       text(alert.Description),
		QueryLanguage:     string(alert.QueryLanguage),
		EditorMode:        string(alert.EditorMode),
		Query:             text(alert.Query),
		ConditionJson:     text(alert.ConditionJSON),
		LookbackSeconds:   int64(alert.LookbackSeconds),
		ThresholdOperator: string(alert.ThresholdOperator),
		ThresholdValue:    alert.ThresholdValue,
		FrequencySeconds:  int64(alert.FrequencySeconds),
		Severity:          string(alert.Severity),
		LabelsJson:        text(labelsJSON),
		AnnotationsJson:   text(annotationsJSON),
		GeneratorUrl:      text(alert.GeneratorURL),
		GroupByJson:       text(groupByJSON),
		IsActive:          alert.IsActive,
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
	groupBy, err := unmarshalStringSlice(row.GroupByJson)
	if err != nil {
		return nil, fmt.Errorf("failed to decode group_by: %w", err)
	}

	alert := &models.Alert{
		ID:                models.AlertID(row.ID),
		SourceID:          models.SourceID(row.SourceID),
		Name:              row.Name,
		Description:       textStr(row.Description),
		QueryLanguage:     models.QueryLanguage(row.QueryLanguage),
		EditorMode:        models.NormalizeAlertEditorMode(models.AlertEditorMode(row.EditorMode)),
		Query:             textStr(row.Query),
		ConditionJSON:     textStr(row.ConditionJson),
		LookbackSeconds:   int(row.LookbackSeconds),
		ThresholdOperator: models.AlertThresholdOperator(row.ThresholdOperator),
		ThresholdValue:    row.ThresholdValue,
		FrequencySeconds:  int(row.FrequencySeconds),
		Severity:          models.AlertSeverity(row.Severity),
		Labels:            labels,
		Annotations:       annotations,
		GeneratorURL:      textStr(row.GeneratorUrl),
		GroupBy:           groupBy,
		IsActive:          row.IsActive,
		LastState:         models.AlertState(row.LastState),
		EvalSeq:           row.EvalSeq,
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

func marshalPayload(payload map[string]any) (string, error) {
	return alertjson.Encode(payload, len(payload) == 0)
}

func marshalStringSlice(values []string) (string, error) {
	return alertjson.Encode(values, len(values) == 0)
}

func unmarshalStringSlice(raw pgtype.Text) ([]string, error) {
	return alertjson.Decode[[]string](textStr(raw))
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

// RecoverAlertDeliveries finalizes delivery reservations left by a stopped manager.
func (s *Store) RecoverAlertDeliveries(ctx context.Context) error {
	if err := s.q.InterruptAlertDeliveries(ctx, sqlc.InterruptAlertDeliveriesParams{}); err != nil {
		return fmt.Errorf("failed to recover alert deliveries: %w", err)
	}
	return nil
}

// AdvanceAlertEvalSeq increments eval_seq when it still equals seq.
func (s *Store) AdvanceAlertEvalSeq(ctx context.Context, alertID models.AlertID, seq int64) error {
	if _, err := s.q.AdvanceAlertEvalSeq(ctx, sqlc.AdvanceAlertEvalSeqParams{ID: int64(alertID), EvalSeq: seq}); err != nil {
		if notFound(err) {
			return models.ErrConflict
		}
		return fmt.Errorf("failed to advance alert eval_seq: %w", err)
	}
	return nil
}

// ListAlertInstances returns the alert's instances ordered by group key.
func (s *Store) ListAlertInstances(ctx context.Context, alertID models.AlertID) ([]*models.AlertInstance, error) {
	rows, err := s.q.ListAlertInstances(ctx, int64(alertID))
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
			StateSince: row.StateSince.Time,
			LastSeenAt: tsPtr(row.LastSeenAt),
			Notified:   row.Notified,
		}
		if row.Value.Valid {
			value := row.Value.Float64
			instance.Value = &value
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
func (s *Store) CountAlertInstances(ctx context.Context, alertIDs []models.AlertID) (map[models.AlertID]models.AlertInstanceCounts, error) {
	counts := make(map[models.AlertID]models.AlertInstanceCounts)
	if len(alertIDs) == 0 {
		return counts, nil
	}
	ids := make([]int64, len(alertIDs))
	for i, id := range alertIDs {
		ids[i] = int64(id)
	}
	rows, err := s.q.CountAlertInstancesByState(ctx, ids)
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
func (s *Store) UpsertAlertInstance(ctx context.Context, instance *models.AlertInstance) error {
	groupJSON, err := alertjson.EncodeGroup(instance.Group)
	if err != nil {
		return fmt.Errorf("failed to marshal alert instance group: %w", err)
	}
	if err := s.q.UpsertAlertInstance(ctx, sqlc.UpsertAlertInstanceParams{
		AlertID:    int64(instance.AlertID),
		GroupKey:   instance.GroupKey,
		GroupJson:  groupJSON,
		State:      string(instance.State),
		Value:      float8FromPtr(instance.Value),
		StateSince: ts(instance.StateSince),
		LastSeenAt: tsFromPtr(instance.LastSeenAt),
		HistoryID:  int8FromPtr(instance.HistoryID),
		Notified:   instance.Notified,
	}); err != nil {
		return fmt.Errorf("failed to upsert alert instance: %w", err)
	}
	return nil
}

// MarkAlertInstanceNotified sets notified on the instance whose open episode is historyID.
func (s *Store) MarkAlertInstanceNotified(ctx context.Context, alertID models.AlertID, groupKey string, historyID int64) error {
	if _, err := s.q.MarkAlertInstanceNotified(ctx, sqlc.MarkAlertInstanceNotifiedParams{
		AlertID:   int64(alertID),
		GroupKey:  groupKey,
		HistoryID: int8Val(historyID),
	}); err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		return fmt.Errorf("failed to mark alert instance notified: %w", err)
	}
	return nil
}
