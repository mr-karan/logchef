package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/store"
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
	if checkQuery {
		source, err := db.GetSource(ctx, sourceID)
		if err != nil {
			return fmt.Errorf("failed to load source: %w", err)
		}
		return datasource.ValidateAlertQuery(source, alert.QueryLanguage, alert.Query)
	}
	return nil
}

// normalizeGroupingAndPolicy checks group_by against the editor mode and
// defaults an absent no_data_policy to zero.
func normalizeGroupingAndPolicy(alert *models.Alert) error {
	var err error
	if alert.GroupBy, err = normalizeGroupBy(alert.GroupBy); err != nil {
		return err
	}
	if len(alert.GroupBy) > 0 && alert.EditorMode != models.AlertEditorModeNative {
		return fmt.Errorf("group_by needs a native query; a LogchefQL condition is evaluated ungrouped")
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
// name any group of the new result. Destinations the editor adds must be
// usable by the editor; the ones the rule has stay.
func UpdateAlert(ctx context.Context, db store.Store, ds *datasource.Service, log *slog.Logger, user *models.User, alertID models.AlertID, req *models.UpdateAlertRequest) (*models.Alert, error) {
	if req == nil {
		return nil, ErrInvalidAlertConfiguration
	}

	existing, err := db.GetAlert(ctx, alertID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrAlertNotFound
		}
		log.Error("failed to load alert for update", "alert_id", alertID, "error", err)
		return nil, fmt.Errorf("failed to load alert: %w", err)
	}

	previous := *existing
	if err := applyAlertUpdates(existing, req); err != nil {
		return nil, err
	}
	// An unchanged query skips the SQL check so owners can still pause, rename
	// or delete an alert whose stored query the parser rejects. Evaluation
	// still refuses to run it.
	queryChanged := existing.Query != previous.Query || existing.QueryLanguage != previous.QueryLanguage || existing.EditorMode != previous.EditorMode
	if err := validateAlertModel(ctx, db, ds, existing.SourceID, existing, queryChanged); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAlertConfiguration, err)
	}

	groupByChanged := !slices.Equal(existing.GroupBy, previous.GroupBy)
	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		return writeAlertUpdate(ctx, tx, user, existing, previous.DestinationIDs, req.DestinationIDs, groupByChanged)
	})
	if err != nil {
		if errors.Is(err, ErrInvalidAlertConfiguration) || errors.Is(err, ErrDestinationForbidden) {
			return nil, err
		}
		if models.IsNotFound(err) {
			return nil, ErrAlertNotFound
		}
		log.Error("failed to update alert", "alert_id", alertID, "error", err)
		return nil, fmt.Errorf("failed to update alert: %w", err)
	}

	updated, err := db.GetAlert(ctx, alertID)
	if err != nil {
		log.Warn("alert updated but fetching updated record failed", "alert_id", alertID, "error", err)
		return existing, nil
	}
	return updated, nil
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

// writeAlertUpdate writes an edited rule in the caller's transaction. The job
// row is written before the alert row, the order in which an evaluation's
// commit locks them, so an edit cannot deadlock with it. Requested
// destinations replace the rule's; a group_by change closes the open
// episodes and drops every instance.
func writeAlertUpdate(ctx context.Context, tx store.StoreOps, user *models.User, alert *models.Alert, current []models.DestinationID, requested *[]models.DestinationID, groupByChanged bool) error {
	if err := jobs.Upsert(ctx, tx, alerts.EvaluationJob(alert)); err != nil {
		return err
	}
	if err := tx.UpdateAlert(ctx, alert); err != nil {
		return err
	}
	if requested != nil {
		ids, err := resolveAlertDestinations(ctx, tx, user, *requested, current)
		if err != nil {
			return err
		}
		if err := tx.SetAlertDestinations(ctx, alert.ID, ids); err != nil {
			return err
		}
	}
	if !groupByChanged {
		return nil
	}
	open, err := tx.ListOpenAlertHistory(ctx, alert.ID)
	if err != nil {
		return err
	}
	if len(open) > 0 {
		if err := tx.ResolveAlertHistory(ctx, open[0].ID, "rule changed"); err != nil {
			return err
		}
	}
	// The no_data and keep policies also track ok groups, which have no
	// episode to close.
	return tx.DeleteAlertInstances(ctx, alert.ID)
}

// validateFrequency applies the floor of the rule's job schedule. A stored rule
// below the floor keeps its value until an edit sets a new one.
func validateFrequency(seconds int) error {
	if seconds < alerts.MinFrequencySeconds {
		return fmt.Errorf("%w: frequency_seconds must be at least %d", ErrInvalidAlertConfiguration, alerts.MinFrequencySeconds)
	}
	return nil
}

// DeleteAlert removes an alert rule and its job.
func DeleteAlert(ctx context.Context, db store.Store, log *slog.Logger, alertID models.AlertID) error {
	// Job row first, as in UpdateAlert.
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := alerts.DeleteEvaluationJob(ctx, tx, alertID); err != nil {
			return err
		}
		return tx.DeleteAlert(ctx, alertID)
	})
	if err != nil {
		if models.IsNotFound(err) {
			return ErrAlertNotFound
		}
		log.Error("failed to delete alert", "alert_id", alertID, "error", err)
		return fmt.Errorf("failed to delete alert: %w", err)
	}
	log.Info("alert deleted", "alert_id", alertID)
	return nil
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
	return history, nil
}

// ResolveAlert manually resolves the open episode of one group, or of every
// group when groupKey is nil. It sends nothing; the alerts manager does that
// when it runs.
func ResolveAlert(ctx context.Context, db store.StoreOps, log *slog.Logger, alertID models.AlertID, groupKey *string, message string) error {
	var err error
	if groupKey != nil {
		err = db.ResolveAlertGroup(ctx, alertID, *groupKey, message)
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
	if _, err := db.GetSource(ctx, sourceID); err != nil {
		if models.IsNotFound(err) {
			return nil, fmt.Errorf("source not found")
		}
		return nil, fmt.Errorf("failed to load source: %w", err)
	}

	tempAlert := &models.Alert{
		SourceID:          sourceID,
		Name:              "test-alert",
		QueryLanguage:     req.QueryLanguage,
		EditorMode:        req.EditorMode,
		Query:             req.Query,
		ConditionJSON:     req.ConditionJSON,
		LookbackSeconds:   req.LookbackSeconds,
		ThresholdOperator: req.ThresholdOperator,
		ThresholdValue:    req.ThresholdValue,
		FrequencySeconds:  60,
		Severity:          models.AlertSeverityWarning,
		GroupBy:           req.GroupBy,
		NoDataPolicy:      req.NoDataPolicy,
	}
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
		return groupedTestResponse(tempAlert, result, executionTime)
	}

	// Extract numeric value from result. No matching rows is not an error —
	// ExtractFirstNumeric returns (0, nil) for an empty result — so check the
	// row count independently and surface a warning, otherwise the user can't
	// tell "waiting for data" from a real zero. An extraction failure on a
	// non-empty result is still a genuine error.
	value, err := util.ExtractFirstNumeric(result)
	var warnings []string
	var thresholdMet bool
	noData := len(result.Logs) == 0
	switch {
	case noData:
		value = 0
		thresholdMet = noRowsThresholdMet(tempAlert)
		warnings = append(warnings, noRowsWarning(tempAlert.NoDataPolicy))
	case err != nil:
		return nil, fmt.Errorf("failed to extract numeric value from result: %w", err)
	default:
		thresholdMet = util.CompareAlertThreshold(value, req.ThresholdValue, req.ThresholdOperator)
	}

	// Generate additional warnings
	warnings = append(warnings, generateQueryWarnings(tempAlert.Query, executionTime, result, false)...)

	return &models.TestAlertQueryResponse{
		Value:           value,
		ThresholdMet:    thresholdMet,
		ExecutionTimeMs: executionTime.Milliseconds(),
		RowsReturned:    len(result.Logs),
		Warnings:        warnings,
		NoData:          noData,
	}, nil
}

// groupedTestResponse reads the samples the scheduler would read. A result
// the scheduler rejects, such as a duplicate group, is a 400 here.
func groupedTestResponse(alert *models.Alert, result *models.QueryResult, executionTime time.Duration) (*models.TestAlertQueryResponse, error) {
	samples, err := alerts.SamplesFromResult(result, alert.GroupBy)
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
	resp.Warnings = append(resp.Warnings, generateQueryWarnings(alert.Query, executionTime, result, true)...)
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
func generateQueryWarnings(query string, executionTime time.Duration, result *models.QueryResult, grouped bool) []string {
	var warnings []string
	queryLower := strings.ToLower(query)

	// Warn if query took more than 5 seconds
	if executionTime > 5*time.Second {
		warnings = append(warnings, fmt.Sprintf("Query execution took %v. Consider optimizing the query for faster evaluation.", executionTime.Round(time.Millisecond)))
	} else if executionTime > 2*time.Second {
		warnings = append(warnings, fmt.Sprintf("Query took %v. This is acceptable but consider optimization if possible.", executionTime.Round(time.Millisecond)))
	}

	// Warn if query doesn't have a time filter (for SQL queries)
	if !strings.Contains(queryLower, "where") && !strings.Contains(queryLower, "interval") && !strings.Contains(queryLower, "now()") {
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
