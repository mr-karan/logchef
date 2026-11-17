package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/template"
	"github.com/mr-karan/logchef/internal/util"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrAlertNotFound is returned when an alert rule cannot be located.
	ErrAlertNotFound = errors.New("alert not found")
	// ErrInvalidAlertConfiguration indicates the request payload failed validation.
	ErrInvalidAlertConfiguration = errors.New("invalid alert configuration")
)

var validOperators = map[models.AlertThresholdOperator]struct{}{
	models.AlertThresholdGreaterThan:        {},
	models.AlertThresholdGreaterThanOrEqual: {},
	models.AlertThresholdLessThan:           {},
	models.AlertThresholdLessThanOrEqual:    {},
	models.AlertThresholdEqual:              {},
	models.AlertThresholdNotEqual:           {},
}

var validSeverities = map[models.AlertSeverity]struct{}{
	models.AlertSeverityInfo:     {},
	models.AlertSeverityWarning:  {},
	models.AlertSeverityCritical: {},
}

func sanitizeStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizeGroupBy trims the group column names and rejects a blank, a
// repeated or a fifth name.
func normalizeGroupBy(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > models.MaxAlertGroupBy {
		return nil, fmt.Errorf("group_by takes at most %d columns", models.MaxAlertGroupBy)
	}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		name := strings.TrimSpace(raw)
		if name == "" {
			return nil, fmt.Errorf("group_by has a blank column name")
		}
		if slices.Contains(out, name) {
			return nil, fmt.Errorf("group_by names column %q twice", name)
		}
		out = append(out, name)
	}
	return out, nil
}

func validateAlertModel(ctx context.Context, db store.StoreOps, ds *datasource.Service, sourceID models.SourceID, alert *models.Alert, checkQuery bool) error {
	if alert == nil {
		return fmt.Errorf("alert payload is required")
	}
	if strings.TrimSpace(alert.Name) == "" {
		return fmt.Errorf("name is required")
	}

	queryLanguage, editorMode, err := models.ResolveAlertMetadata(alert.QueryLanguage, alert.EditorMode)
	if err != nil {
		return err
	}
	if ds == nil {
		return fmt.Errorf("datasource service is required")
	}
	if err := ds.ValidateAlertSupport(ctx, sourceID, queryLanguage, editorMode); err != nil {
		return err
	}

	alert.QueryLanguage = queryLanguage
	alert.EditorMode = editorMode
	alert.Name = strings.TrimSpace(alert.Name)
	alert.Description = strings.TrimSpace(alert.Description)
	alert.Query = strings.TrimSpace(alert.Query)
	alert.ConditionJSON = strings.TrimSpace(alert.ConditionJSON)
	if err := normalizeGroupingAndPolicy(alert); err != nil {
		return err
	}

	switch alert.EditorMode {
	case models.AlertEditorModeNative:
		if alert.Query == "" {
			return fmt.Errorf("query is required for native alerts")
		}
	case models.AlertEditorModeCondition:
		if alert.ConditionJSON == "" {
			return fmt.Errorf("condition_json is required for condition alerts")
		}
		if alert.Query == "" {
			return fmt.Errorf("query is required for condition alerts")
		}
	}
	if _, ok := validOperators[alert.ThresholdOperator]; !ok {
		return fmt.Errorf("invalid threshold_operator %q", alert.ThresholdOperator)
	}
	if alert.FrequencySeconds <= 0 {
		return fmt.Errorf("frequency_seconds must be greater than zero")
	}
	if alert.LookbackSeconds <= 0 {
		return fmt.Errorf("lookback_seconds must be greater than zero")
	}
	if _, ok := validSeverities[alert.Severity]; !ok {
		return fmt.Errorf("invalid severity %q", alert.Severity)
	}
	if !checkQuery {
		return nil
	}
	return validateAlertQuery(ctx, db, ds, sourceID, alert)
}

// validateAlertQuery rejects a query that evaluation would refuse to run. A
// LogchefQL condition is compiled for the source, and its by keys become the
// rule's group_by.
func validateAlertQuery(ctx context.Context, db store.StoreOps, ds *datasource.Service, sourceID models.SourceID, alert *models.Alert) error {
	source, err := db.GetSource(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("failed to load source: %w", err)
	}
	if alert.QueryLanguage != models.QueryLanguageLogchefQL {
		return datasource.ValidateAlertQuery(source, alert.QueryLanguage, alert.Query)
	}
	condition, err := ds.CompileAlertCondition(ctx, sourceID, alert.Query, alert.LookbackSeconds)
	if err != nil {
		return err
	}
	// The by keys of the condition are its groups. A group_by in the request
	// may repeat them, never differ.
	if len(alert.GroupBy) > 0 && !slices.Equal(alert.GroupBy, condition.GroupBy) {
		return fmt.Errorf("group_by %q differs from the by keys %q of the LogchefQL condition. Leave group_by empty", alert.GroupBy, condition.GroupBy)
	}
	alert.GroupBy = condition.GroupBy
	return datasource.ValidateAlertQuery(source, condition.Language, condition.Query)
}

// normalizeGroupingAndPolicy checks group_by against the editor mode and
// defaults an absent no_data_policy to zero.
func normalizeGroupingAndPolicy(alert *models.Alert) error {
	var err error
	if alert.GroupBy, err = normalizeGroupBy(alert.GroupBy); err != nil {
		return err
	}
	// A condition saved before the server compiled conditions holds the
	// native query the form generated, which is ungrouped.
	if len(alert.GroupBy) > 0 && alert.EditorMode == models.AlertEditorModeCondition && alert.QueryLanguage != models.QueryLanguageLogchefQL {
		return fmt.Errorf("group_by on a condition needs query_language logchefql")
	}
	alert.NoDataPolicy = alert.NoDataPolicy.OrZero()
	if !alert.NoDataPolicy.Valid() {
		return fmt.Errorf("invalid no_data_policy %q: use zero, no_data or keep", alert.NoDataPolicy)
	}
	return nil
}

// CreateAlert creates a new alert rule for the specified source, owned by the
// user, with the destinations the user may use, together with the job that
// evaluates it.
func CreateAlert(ctx context.Context, db store.Store, ds *datasource.Service, log *slog.Logger, sourceID models.SourceID, user *models.User, req *models.CreateAlertRequest) (*models.Alert, error) {
	if req == nil {
		return nil, ErrInvalidAlertConfiguration
	}
	owner := user.ID
	alert := &models.Alert{
		SourceID:          sourceID,
		Name:              req.Name,
		Description:       req.Description,
		QueryLanguage:     req.QueryLanguage,
		EditorMode:        req.EditorMode,
		Query:             req.Query,
		ConditionJSON:     req.ConditionJSON,
		LookbackSeconds:   req.LookbackSeconds,
		ThresholdOperator: req.ThresholdOperator,
		ThresholdValue:    req.ThresholdValue,
		FrequencySeconds:  req.FrequencySeconds,
		Severity:          req.Severity,
		Labels:            sanitizeStringMap(req.Labels),
		Annotations:       sanitizeStringMap(req.Annotations),
		GeneratorURL:      strings.TrimSpace(req.GeneratorURL),
		GroupBy:           req.GroupBy,
		NoDataPolicy:      req.NoDataPolicy,
		NotifyOnResolve:   req.NotifyOnResolve == nil || *req.NotifyOnResolve,
		IsActive:          req.IsActive,
		CreatedBy:         &owner,
	}
	if err := validateFrequency(alert.FrequencySeconds); err != nil {
		return nil, err
	}
	if err := validateAlertModel(ctx, db, ds, sourceID, alert, true); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAlertConfiguration, err)
	}

	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		ids, err := resolveAlertDestinations(ctx, tx, user, req.DestinationIDs, nil)
		if err != nil {
			return err
		}
		if err := tx.CreateAlert(ctx, alert); err != nil {
			return err
		}
		if err := tx.SetAlertDestinations(ctx, alert.ID, ids); err != nil {
			return err
		}
		alert.DestinationIDs = ids
		return jobs.Upsert(ctx, tx, alerts.EvaluationJob(alert))
	})
	if err != nil {
		if errors.Is(err, ErrInvalidAlertConfiguration) || errors.Is(err, ErrDestinationForbidden) {
			return nil, err
		}
		log.Error("failed to create alert", "source_id", sourceID, "error", err)
		return nil, fmt.Errorf("failed to create alert: %w", err)
	}
	log.Info("alert created", "alert_id", alert.ID, "source_id", sourceID, "created_by", user.ID)
	return alert, nil
}

// GetAlert retrieves a single alert by ID.
func GetAlert(ctx context.Context, db store.StoreOps, log *slog.Logger, alertID models.AlertID) (*models.Alert, error) {
	alert, err := db.GetAlert(ctx, alertID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrAlertNotFound
		}
		log.Error("failed to get alert", "alert_id", alertID, "error", err)
		return nil, fmt.Errorf("failed to get alert: %w", err)
	}
	return alert, nil
}

// UpdateAlert updates an existing alert rule and its job. A change of group_by closes every
// open episode with "rule changed", because the old group keys no longer
// name any group of the new result, and sends their resolved notifications
// (alerts.CloseForRuleChange). Destinations the editor adds must be
// usable by the editor; the ones the rule has stay. It returns the rule before
// and after the write, both read in the write's transaction under the row
// lock.
func UpdateAlert(ctx context.Context, db store.Store, ds *datasource.Service, log *slog.Logger, user *models.User, alertID models.AlertID, req *models.UpdateAlertRequest) (before, after *models.Alert, err error) {
	if req == nil {
		return nil, nil, ErrInvalidAlertConfiguration
	}

	existing, err := db.GetAlert(ctx, alertID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, nil, ErrAlertNotFound
		}
		log.Error("failed to load alert for update", "alert_id", alertID, "error", err)
		return nil, nil, fmt.Errorf("failed to load alert: %w", err)
	}

	previous := *existing
	if err := applyAlertUpdates(existing, req); err != nil {
		return nil, nil, err
	}
	// An unchanged query skips the SQL check so owners can still pause, rename
	// or delete an alert whose stored query the parser rejects. Evaluation
	// still refuses to run it.
	// A LogchefQL condition's groups come from its compile, so a group_by
	// change is checked against the query too.
	queryChanged := existing.Query != previous.Query || existing.QueryLanguage != previous.QueryLanguage || existing.EditorMode != previous.EditorMode ||
		!slices.Equal(existing.GroupBy, previous.GroupBy)
	if err := validateAlertModel(ctx, db, ds, existing.SourceID, existing, queryChanged); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidAlertConfiguration, err)
	}

	groupByChanged := !slices.Equal(existing.GroupBy, previous.GroupBy)
	enqueued := 0
	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		var err error
		if before, enqueued, err = writeAlertUpdate(ctx, tx, log, user, existing, previous.DestinationIDs, req.DestinationIDs, groupByChanged); err != nil {
			return err
		}
		after, err = tx.GetAlert(ctx, alertID)
		return err
	})
	if err != nil {
		if errors.Is(err, ErrInvalidAlertConfiguration) || errors.Is(err, ErrDestinationForbidden) {
			return nil, nil, err
		}
		if models.IsNotFound(err) {
			return nil, nil, ErrAlertNotFound
		}
		log.Error("failed to update alert", "alert_id", alertID, "error", err)
		return nil, nil, fmt.Errorf("failed to update alert: %w", err)
	}
	if enqueued > 0 {
		// The runner polls the store, so the drain sends the resolves now
		// instead of at its interval. A failure only delays them.
		if err := db.KickJob(ctx, string(notify.DeliverKind), notify.DrainKey); err != nil {
			log.Warn("failed to kick the delivery drain; it runs at its interval", "error", err)
		}
	}
	return before, after, nil
}

func applyAlertUpdates(alert *models.Alert, req *models.UpdateAlertRequest) error {
	if req.Name != nil {
		alert.Name = *req.Name
	}
	if req.Description != nil {
		alert.Description = *req.Description
	}

	if err := applyQueryTypeUpdate(alert, req); err != nil {
		return err
	}
	if err := applyThresholdUpdates(alert, req); err != nil {
		return err
	}
	applyMetadataUpdates(alert, req)

	return nil
}

func applyQueryTypeUpdate(alert *models.Alert, req *models.UpdateAlertRequest) error {
	if req.QueryLanguage != nil {
		alert.QueryLanguage = *req.QueryLanguage
	}
	if req.EditorMode != nil {
		alert.EditorMode = *req.EditorMode
	}
	if req.Query != nil {
		alert.Query = *req.Query
	}
	if req.ConditionJSON != nil {
		alert.ConditionJSON = *req.ConditionJSON
	}
	return nil
}

func applyThresholdUpdates(alert *models.Alert, req *models.UpdateAlertRequest) error {
	if req.LookbackSeconds != nil {
		if *req.LookbackSeconds <= 0 {
			return fmt.Errorf("%w: lookback_seconds must be greater than zero", ErrInvalidAlertConfiguration)
		}
		alert.LookbackSeconds = *req.LookbackSeconds
	}
	if req.ThresholdOperator != nil {
		if _, ok := validOperators[*req.ThresholdOperator]; !ok {
			return fmt.Errorf("%w: invalid threshold_operator %q", ErrInvalidAlertConfiguration, *req.ThresholdOperator)
		}
		alert.ThresholdOperator = *req.ThresholdOperator
	}
	if req.ThresholdValue != nil {
		alert.ThresholdValue = *req.ThresholdValue
	}
	if req.FrequencySeconds != nil {
		if err := validateFrequency(*req.FrequencySeconds); err != nil {
			return err
		}
		alert.FrequencySeconds = *req.FrequencySeconds
	}
	if req.Severity != nil {
		if _, ok := validSeverities[*req.Severity]; !ok {
			return fmt.Errorf("%w: invalid severity %q", ErrInvalidAlertConfiguration, *req.Severity)
		}
		alert.Severity = *req.Severity
	}
	return nil
}

func applyMetadataUpdates(alert *models.Alert, req *models.UpdateAlertRequest) {
	if req.Labels != nil {
		alert.Labels = sanitizeStringMap(*req.Labels)
	}
	if req.Annotations != nil {
		alert.Annotations = sanitizeStringMap(*req.Annotations)
	}
	if req.GeneratorURL != nil {
		alert.GeneratorURL = strings.TrimSpace(*req.GeneratorURL)
	}
	if req.GroupBy != nil {
		alert.GroupBy = *req.GroupBy
	}
	if req.NoDataPolicy != nil {
		alert.NoDataPolicy = *req.NoDataPolicy
	}
	if req.NotifyOnResolve != nil {
		alert.NotifyOnResolve = *req.NotifyOnResolve
	}
	if req.IsActive != nil {
		alert.IsActive = *req.IsActive
	}
}

// writeAlertUpdate writes an edited rule in the caller's transaction and
// returns the rule it replaced, read under the row lock, and the number of
// notifications it enqueued. The job row is written before the alert row, the
// order in which an evaluation's commit locks them, so an edit cannot
// deadlock with it. A group_by change closes the open episodes, enqueues
// their resolved notifications and drops every instance; it runs before
// requested destinations replace the rule's, so a resolve reaches the
// destinations that its firing reached.
func writeAlertUpdate(ctx context.Context, tx store.StoreOps, log *slog.Logger, user *models.User, alert *models.Alert, current []models.DestinationID, requested *[]models.DestinationID, groupByChanged bool) (*models.Alert, int, error) {
	if err := jobs.Upsert(ctx, tx, alerts.EvaluationJob(alert)); err != nil {
		return nil, 0, err
	}
	before, err := tx.GetAlertForUpdate(ctx, alert.ID)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.UpdateAlert(ctx, alert); err != nil {
		return nil, 0, err
	}
	enqueued := 0
	if groupByChanged {
		if enqueued, err = alerts.CloseForRuleChange(ctx, tx, log, alert); err != nil {
			return nil, 0, err
		}
	}
	if requested != nil {
		ids, err := resolveAlertDestinations(ctx, tx, user, *requested, current)
		if err != nil {
			return nil, 0, err
		}
		if err := tx.SetAlertDestinations(ctx, alert.ID, ids); err != nil {
			return nil, 0, err
		}
	}
	return before, enqueued, nil
}

// validateFrequency applies the floor of the rule's job schedule. A stored rule
// below the floor keeps its value until an edit sets a new one.
func validateFrequency(seconds int) error {
	if seconds < alerts.MinFrequencySeconds {
		return fmt.Errorf("%w: frequency_seconds must be at least %d", ErrInvalidAlertConfiguration, alerts.MinFrequencySeconds)
	}
	return nil
}

// DeleteAlert removes an alert rule and its job, and returns the rule it
// deleted, read under the row lock of the delete.
func DeleteAlert(ctx context.Context, db store.Store, log *slog.Logger, alertID models.AlertID) (*models.Alert, error) {
	var deleted *models.Alert
	// Job row first, as in UpdateAlert.
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := alerts.DeleteEvaluationJob(ctx, tx, alertID); err != nil {
			return err
		}
		var err error
		if deleted, err = tx.GetAlertForUpdate(ctx, alertID); err != nil {
			return err
		}
		return tx.DeleteAlert(ctx, alertID)
	})
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrAlertNotFound
		}
		log.Error("failed to delete alert", "alert_id", alertID, "error", err)
		return nil, fmt.Errorf("failed to delete alert: %w", err)
	}
	log.Info("alert deleted", "alert_id", alertID)
	return deleted, nil
}

// ListAlertsBySource returns all alerts for a source.
func ListAlertsBySource(ctx context.Context, db store.StoreOps, sourceID models.SourceID) ([]*models.Alert, error) {
	list, err := db.ListAlertsBySource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}
	return list, AttachAlertInstanceCounts(ctx, db, list)
}

// ListAlertsForUser returns alerts the user can see (cross-source).
func ListAlertsForUser(ctx context.Context, db store.StoreOps, userID models.UserID) ([]*models.Alert, error) {
	list, err := db.ListAlertsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}
	return list, AttachAlertInstanceCounts(ctx, db, list)
}

// AttachAlertInstanceCounts sets InstanceCounts on every alert, zero counts
// included, with one query for the whole list.
func AttachAlertInstanceCounts(ctx context.Context, db store.StoreOps, list []*models.Alert) error {
	ids := make([]models.AlertID, len(list))
	for i, alert := range list {
		ids[i] = alert.ID
	}
	counts, err := db.CountAlertInstances(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to count alert instances: %w", err)
	}
	for _, alert := range list {
		alertCounts := counts[alert.ID]
		alert.InstanceCounts = &alertCounts
	}
	return nil
}

// MaxAlertInstancesListed bounds GET /alerts/:alertID/instances.
const MaxAlertInstancesListed = 200

// ListAlertInstances returns the alert's instances, firing first, at most
// MaxAlertInstancesListed.
func ListAlertInstances(ctx context.Context, db store.StoreOps, alertID models.AlertID) ([]*models.AlertInstance, error) {
	instances, err := db.ListAlertInstances(ctx, alertID)
	if err != nil {
		return nil, fmt.Errorf("failed to list alert instances: %w", err)
	}
	slices.SortStableFunc(instances, func(a, b *models.AlertInstance) int {
		return instanceStateRank(a.State) - instanceStateRank(b.State)
	})
	if len(instances) > MaxAlertInstancesListed {
		instances = instances[:MaxAlertInstancesListed]
	}
	return instances, nil
}

func instanceStateRank(state models.AlertInstanceState) int {
	switch state {
	case models.AlertInstanceStateFiring:
		return 0
	case models.AlertInstanceStateNoData:
		return 1
	default:
		return 2
	}
}

// UserCanEditAlert returns true if the user is the creator or a global admin.
// Legacy alerts (CreatedBy == nil) are editable only by global admins.
func UserCanEditAlert(alert *models.Alert, user *models.User) bool {
	if alert == nil || user == nil {
		return false
	}
	if user.Role == models.UserRoleAdmin {
		return true
	}
	return alert.CreatedBy != nil && *alert.CreatedBy == user.ID
}

// ListAlertHistory retrieves a limited set of alert history entries.
func ListAlertHistory(ctx context.Context, db store.StoreOps, alertID models.AlertID, limit int) ([]*models.AlertHistoryEntry, error) {
	history, err := db.ListAlertHistory(ctx, alertID, limit)
	if err != nil {
		if models.IsNotFound(err) {
			return []*models.AlertHistoryEntry{}, nil
		}
		return nil, fmt.Errorf("failed to list alert history: %w", err)
	}
	refs := make([]string, len(history))
	for i, entry := range history {
		refs[i] = alerts.DeliveryRef(alertID, entry.ID)
	}
	deliveries, err := db.ListDeliverySummaries(ctx, alerts.DeliveryProducer, refs)
	if err != nil {
		return nil, fmt.Errorf("failed to list alert deliveries: %w", err)
	}
	for i, entry := range history {
		entry.Deliveries = deliveries[refs[i]]
	}
	return history, nil
}

// ResolveAlert manually resolves the open episode of one group, or of every
// group when groupKey is nil. It sends nothing; the alerts manager does that
// when it runs.
func ResolveAlert(ctx context.Context, db store.StoreOps, log *slog.Logger, alertID models.AlertID, groupKey *string, message string) error {
	var err error
	if groupKey != nil {
		err = db.ResolveAlertGroup(ctx, alertID, *groupKey, message, time.Now().UTC())
	} else {
		var entry *models.AlertHistoryEntry
		if entry, err = db.GetLatestUnresolvedAlertHistory(ctx, alertID); err == nil {
			err = db.ResolveAlertHistory(ctx, entry.ID, message)
		}
	}
	if err != nil {
		if models.IsNotFound(err) {
			return ErrAlertNotFound
		}
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}
	log.Info("alert history resolved", "alert_id", alertID)
	return nil
}

// testAlertQuery executes a test query to validate alert configuration and
// show performance metrics. It does not authorize; callers use
// TestAlertQueryForPrincipal.
func testAlertQuery(ctx context.Context, db store.StoreOps, ds *datasource.Service, sourceID models.SourceID, req *models.TestAlertQueryRequest) (*models.TestAlertQueryResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("test query request is required")
	}
	if req.LookbackSeconds == 0 {
		req.LookbackSeconds = 300
	}
	if req.LookbackSeconds < 0 {
		return nil, fmt.Errorf("lookback_seconds must be greater than zero")
	}

	if ds == nil {
		return nil, fmt.Errorf("datasource service is required")
	}

	// Load source to verify access
	source, err := db.GetSource(ctx, sourceID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, fmt.Errorf("source not found")
		}
		return nil, fmt.Errorf("failed to load source: %w", err)
	}

	tempAlert := unsavedAlert(sourceID, req, 60)
	if err := validateAlertModel(ctx, db, ds, sourceID, tempAlert, true); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAlertConfiguration, err)
	}

	// Execute query with timing
	timeout := models.DefaultQueryTimeoutSeconds
	startTime := time.Now()
	result, err := ds.EvaluateAlert(ctx, sourceID, datasource.AlertQueryRequest{
		At:              time.Now().UTC(),
		Language:        tempAlert.QueryLanguage,
		Query:           tempAlert.Query,
		LookbackSeconds: tempAlert.LookbackSeconds,
		QueryTimeout:    &timeout,
	})
	executionTime := time.Since(startTime)

	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}

	if len(tempAlert.GroupBy) > 0 {
		return groupedTestResponse(tempAlert, source.MetaTSField, result, executionTime)
	}

	// No sample is not an error, so surface a warning, otherwise the user
	// can't tell "waiting for data" from a real zero. An extraction failure on
	// a non-empty result is still a genuine error.
	samples, err := alerts.SamplesFromResult(result, nil, tempAlert.QueryLanguage)
	if err != nil {
		return nil, fmt.Errorf("failed to extract numeric value from result: %w", err)
	}
	var value float64
	var warnings []string
	var thresholdMet bool
	noData := len(samples) == 0
	if noData {
		thresholdMet = noRowsThresholdMet(tempAlert)
		warnings = append(warnings, noRowsWarning(tempAlert.NoDataPolicy))
	} else {
		value = samples[0].Value
		thresholdMet = util.CompareAlertThreshold(value, req.ThresholdValue, req.ThresholdOperator)
	}

	// Generate additional warnings
	warnings = append(warnings, generateQueryWarnings(tempAlert, source.MetaTSField, executionTime, result, false)...)

	return &models.TestAlertQueryResponse{
		Value:           value,
		ThresholdMet:    thresholdMet,
		ExecutionTimeMs: executionTime.Milliseconds(),
		RowsReturned:    len(result.Logs),
		Warnings:        warnings,
		NoData:          noData,
	}, nil
}

// unsavedAlert is the rule that the test and backtest routes run.
func unsavedAlert(sourceID models.SourceID, req *models.TestAlertQueryRequest, frequencySeconds int) *models.Alert {
	return &models.Alert{
		SourceID:          sourceID,
		Name:              "test-alert",
		QueryLanguage:     req.QueryLanguage,
		EditorMode:        req.EditorMode,
		Query:             req.Query,
		ConditionJSON:     req.ConditionJSON,
		LookbackSeconds:   req.LookbackSeconds,
		ThresholdOperator: req.ThresholdOperator,
		ThresholdValue:    req.ThresholdValue,
		FrequencySeconds:  frequencySeconds,
		Severity:          models.AlertSeverityWarning,
		GroupBy:           req.GroupBy,
		NoDataPolicy:      req.NoDataPolicy,
	}
}

// backtestAlert replays an unsaved rule over the range_seconds that end now.
// It does not authorize; callers use BacktestAlertForPrincipal. A query that
// cannot be run for a past instant returns *datasource.ReplayError.
func backtestAlert(ctx context.Context, db store.StoreOps, ds *datasource.Service, sourceID models.SourceID, req *models.BacktestAlertRequest) (*models.BacktestResult, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: backtest request is required", ErrInvalidAlertConfiguration)
	}
	if req.RangeSeconds < models.MinBacktestRangeSeconds || req.RangeSeconds > models.MaxBacktestRangeSeconds {
		return nil, fmt.Errorf("%w: range_seconds must be between %d and %d", ErrInvalidAlertConfiguration, models.MinBacktestRangeSeconds, models.MaxBacktestRangeSeconds)
	}
	if err := validateFrequency(req.FrequencySeconds); err != nil {
		return nil, err
	}
	if req.FrequencySeconds > req.RangeSeconds {
		return nil, fmt.Errorf("%w: frequency_seconds must not exceed range_seconds", ErrInvalidAlertConfiguration)
	}
	if req.LookbackSeconds <= 0 {
		return nil, fmt.Errorf("%w: lookback_seconds must be greater than zero", ErrInvalidAlertConfiguration)
	}
	alert := unsavedAlert(sourceID, &req.TestAlertQueryRequest, req.FrequencySeconds)
	if err := validateAlertModel(ctx, db, ds, sourceID, alert, true); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAlertConfiguration, err)
	}
	source, err := db.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load source: %w", err)
	}
	if err := datasource.CheckAlertReplayable(source, alert.QueryLanguage, alert.Query); err != nil {
		if _, ok := errors.AsType[*datasource.ReplayError](err); ok {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalidAlertConfiguration, err)
	}

	rule := alerts.BacktestRule{
		Rule: alerts.Rule{
			Operator:  alert.ThresholdOperator,
			Threshold: alert.ThresholdValue,
			NoData:    alert.NoDataPolicy,
		},
		GroupBy:          alert.GroupBy,
		Language:         alert.QueryLanguage,
		FrequencySeconds: alert.FrequencySeconds,
		LookbackSeconds:  alert.LookbackSeconds,
		NotifyOnResolve:  req.NotifyOnResolve == nil || *req.NotifyOnResolve,
	}
	timeout := models.DefaultQueryTimeoutSeconds
	query := func(ctx context.Context, at time.Time) (*models.QueryResult, error) {
		return ds.EvaluateAlert(ctx, sourceID, datasource.AlertQueryRequest{
			At:              at,
			Language:        alert.QueryLanguage,
			Query:           alert.Query,
			LookbackSeconds: alert.LookbackSeconds,
			QueryTimeout:    &timeout,
		})
	}
	return alerts.Backtest(ctx, rule, time.Now().UTC().Truncate(time.Second), req.RangeSeconds, query)
}

// groupedTestResponse reads the samples the scheduler would read. A result
// the scheduler rejects, such as a duplicate group, is a 400 here.
func groupedTestResponse(alert *models.Alert, timestampColumn string, result *models.QueryResult, executionTime time.Duration) (*models.TestAlertQueryResponse, error) {
	samples, err := alerts.SamplesFromResult(result, alert.GroupBy, alert.QueryLanguage)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAlertConfiguration, err)
	}
	resp := &models.TestAlertQueryResponse{
		ExecutionTimeMs: executionTime.Milliseconds(),
		RowsReturned:    len(result.Logs),
		Samples:         make([]models.AlertSample, 0, min(len(samples), models.MaxTestAlertSamples)),
		GroupsTotal:     len(samples),
	}
	for i, sample := range samples {
		met := util.CompareAlertThreshold(sample.Value, alert.ThresholdValue, alert.ThresholdOperator)
		resp.ThresholdMet = resp.ThresholdMet || met
		if i == 0 {
			resp.Value = sample.Value
		}
		if i < models.MaxTestAlertSamples {
			resp.Samples = append(resp.Samples, models.AlertSample{Group: sample.Group, Value: sample.Value, ThresholdMet: met})
		}
	}
	if len(samples) == 0 {
		resp.NoData = true
		resp.ThresholdMet = noRowsThresholdMet(alert)
		resp.Warnings = append(resp.Warnings, noRowsWarning(alert.NoDataPolicy))
	}
	resp.Warnings = append(resp.Warnings, generateQueryWarnings(alert, timestampColumn, executionTime, result, true)...)
	return resp, nil
}

// noRowsThresholdMet reports whether a result without rows meets the
// threshold: only the zero policy compares 0, the others never fire on it.
func noRowsThresholdMet(alert *models.Alert) bool {
	return alert.NoDataPolicy == models.AlertNoDataZero && util.CompareAlertThreshold(0, alert.ThresholdValue, alert.ThresholdOperator)
}

func noRowsWarning(policy models.AlertNoDataPolicy) string {
	switch policy {
	case models.AlertNoDataNoData:
		return "Query returned no rows. The alert will report no data."
	case models.AlertNoDataKeep:
		return "Query returned no rows. The alert will keep its current state."
	default:
		return "Query returned no rows. The alert will be evaluated when matching data exists. Using value 0 for threshold comparison."
	}
}

// generateQueryWarnings flags slow or unbounded queries. A grouped rule reads
// every row, so only an ungrouped one is warned about returning several.
func generateQueryWarnings(alert *models.Alert, timestampColumn string, executionTime time.Duration, result *models.QueryResult, grouped bool) []string {
	var warnings []string

	// Warn if query took more than 5 seconds
	if executionTime > 5*time.Second {
		warnings = append(warnings, fmt.Sprintf("Query execution took %v. Consider optimizing the query for faster evaluation.", executionTime.Round(time.Millisecond)))
	} else if executionTime > 2*time.Second {
		warnings = append(warnings, fmt.Sprintf("Query took %v. This is acceptable but consider optimization if possible.", executionTime.Round(time.Millisecond)))
	}

	if lacksTimeFilter(alert, timestampColumn) {
		warnings = append(warnings, "Query appears to lack a time-based filter. Consider adding a time window (e.g., WHERE {{__time_filter}}) to improve performance and relevance.")
	}

	if grouped {
		return warnings
	}

	// Warn if result set is unexpectedly large
	switch {
	case result.Stats.Truncated && result.Stats.TruncatedReason == "byte_limit":
		warnings = append(warnings, fmt.Sprintf("Results were cut off by the response size limit after %d rows. Alert queries should typically return a single numeric value (e.g., count, sum, avg).", len(result.Logs)))
	case result.Stats.Truncated:
		warnings = append(warnings, fmt.Sprintf("Results were capped at %d buffered rows. Alert queries should typically return a single numeric value (e.g., count, sum, avg).", len(result.Logs)))
	case len(result.Logs) > 1:
		warnings = append(warnings, fmt.Sprintf("Query returned %d rows. Alert queries should typically return a single numeric value (e.g., count, sum, avg).", len(result.Logs)))
	}

	return warnings
}

// lacksTimeFilter reports whether a ClickHouse SQL alert query appears to read
// every row: it uses no time macro, and its WHERE clause does not name the
// source's timestamp column. LogsQL and LogQL need no check, because their
// providers bound the query to the lookback that ends at the evaluation instant.
func lacksTimeFilter(alert *models.Alert, timestampColumn string) bool {
	if alert.QueryLanguage != models.QueryLanguageClickHouseSQL {
		return false
	}
	if slices.ContainsFunc(template.ReferencedNames(alert.Query), isTimeMacro) {
		return false
	}
	// strings.Cut on "where" also finds PREWHERE.
	_, where, found := strings.Cut(strings.ToLower(alert.Query), "where")
	if !found || timestampColumn == "" {
		return true
	}
	column := regexp.MustCompile(`(^|[^a-z0-9_])` + regexp.QuoteMeta(strings.ToLower(timestampColumn)) + `($|[^a-z0-9_])`)
	return !column.MatchString(where)
}

func isTimeMacro(name string) bool {
	return name == "__time_filter" || name == "__from" || name == "__to"
}
