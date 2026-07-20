package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
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

func sanitizeUserIDs(in []models.UserID) []models.UserID {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[models.UserID]struct{}, len(in))
	out := make([]models.UserID, 0, len(in))
	for _, id := range in {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeWebhookURLs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		urlValue := strings.TrimSpace(raw)
		if urlValue == "" {
			continue
		}
		if _, ok := seen[urlValue]; ok {
			continue
		}
		seen[urlValue] = struct{}{}
		out = append(out, urlValue)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validateRecipientUserIDs(ctx context.Context, db store.StoreOps, recipientIDs []models.UserID) error {
	if len(recipientIDs) == 0 {
		return nil
	}
	// Recipients are users, not team members. Make sure each id resolves.
	for _, id := range recipientIDs {
		user, err := db.GetUser(ctx, id)
		if err != nil || user == nil {
			return fmt.Errorf("user %d not found", id)
		}
	}
	return nil
}

func validateWebhookURLs(urls []string) error {
	for _, raw := range urls {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			return fmt.Errorf("invalid webhook URL %q", raw)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("webhook URL %q must use http or https", raw)
		}
	}
	return nil
}

func validateAlertModel(ctx context.Context, ds *datasource.Service, sourceID models.SourceID, alert *models.Alert) error {
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
	return nil
}

// CreateAlert creates a new alert rule for the specified source, owned by createdBy.
func CreateAlert(ctx context.Context, db store.StoreOps, ds *datasource.Service, log *slog.Logger, sourceID models.SourceID, createdBy models.UserID, req *models.CreateAlertRequest) (*models.Alert, error) {
	if req == nil {
		return nil, ErrInvalidAlertConfiguration
	}
	recipientUserIDs := sanitizeUserIDs(req.RecipientUserIDs)
	if err := validateRecipientUserIDs(ctx, db, recipientUserIDs); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAlertConfiguration, err)
	}
	webhookURLs := sanitizeWebhookURLs(req.WebhookURLs)
	if err := validateWebhookURLs(webhookURLs); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAlertConfiguration, err)
	}
	owner := createdBy
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
		RecipientUserIDs:  recipientUserIDs,
		WebhookURLs:       webhookURLs,
		GeneratorURL:      strings.TrimSpace(req.GeneratorURL),
		IsActive:          req.IsActive,
		CreatedBy:         &owner,
	}
	if err := validateAlertModel(ctx, ds, sourceID, alert); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAlertConfiguration, err)
	}

	if err := db.CreateAlert(ctx, alert); err != nil {
		log.Error("failed to create alert", "source_id", sourceID, "error", err)
		return nil, fmt.Errorf("failed to create alert: %w", err)
	}
	log.Info("alert created", "alert_id", alert.ID, "source_id", sourceID, "created_by", createdBy)
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

// UpdateAlert updates an existing alert rule.
func UpdateAlert(ctx context.Context, db store.StoreOps, ds *datasource.Service, log *slog.Logger, alertID models.AlertID, req *models.UpdateAlertRequest) (*models.Alert, error) {
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

	if err := applyAlertUpdates(existing, req); err != nil {
		return nil, err
	}
	if req.RecipientUserIDs != nil {
		if err := validateRecipientUserIDs(ctx, db, existing.RecipientUserIDs); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidAlertConfiguration, err)
		}
	}
	if req.WebhookURLs != nil {
		if err := validateWebhookURLs(existing.WebhookURLs); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidAlertConfiguration, err)
		}
	}
	if err := validateAlertModel(ctx, ds, existing.SourceID, existing); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAlertConfiguration, err)
	}

	if err := db.UpdateAlert(ctx, existing); err != nil {
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
		if *req.FrequencySeconds <= 0 {
			return fmt.Errorf("%w: frequency_seconds must be greater than zero", ErrInvalidAlertConfiguration)
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
	if req.RecipientUserIDs != nil {
		alert.RecipientUserIDs = sanitizeUserIDs(*req.RecipientUserIDs)
	}
	if req.WebhookURLs != nil {
		alert.WebhookURLs = sanitizeWebhookURLs(*req.WebhookURLs)
	}
	if req.GeneratorURL != nil {
		alert.GeneratorURL = strings.TrimSpace(*req.GeneratorURL)
	}
	if req.IsActive != nil {
		alert.IsActive = *req.IsActive
	}
}

// DeleteAlert removes an alert rule.
func DeleteAlert(ctx context.Context, db store.StoreOps, log *slog.Logger, alertID models.AlertID) error {
	if err := db.DeleteAlert(ctx, alertID); err != nil {
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
	alerts, err := db.ListAlertsBySource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}
	return alerts, nil
}

// ListAlertsForUser returns alerts the user can see (cross-source).
func ListAlertsForUser(ctx context.Context, db store.StoreOps, userID models.UserID) ([]*models.Alert, error) {
	alerts, err := db.ListAlertsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}
	return alerts, nil
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

// ResolveAlert manually resolves the most recent triggered history entry.
func ResolveAlert(ctx context.Context, db store.StoreOps, log *slog.Logger, alertID models.AlertID, message string) error {
	entry, err := db.GetLatestUnresolvedAlertHistory(ctx, alertID)
	if err != nil {
		if models.IsNotFound(err) {
			return ErrAlertNotFound
		}
		return fmt.Errorf("failed to find unresolved alert history: %w", err)
	}
	if err := db.ResolveAlertHistory(ctx, entry.ID, message); err != nil {
		if models.IsNotFound(err) {
			return ErrAlertNotFound
		}
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}
	log.Info("alert history resolved", "alert_id", alertID, "history_id", entry.ID)
	return nil
}

// TestAlertQuery executes a test query to validate alert configuration and show performance metrics.
func TestAlertQuery(ctx context.Context, db store.StoreOps, ds *datasource.Service, sourceID models.SourceID, req *models.TestAlertQueryRequest) (*models.TestAlertQueryResponse, error) {
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
	}
	if err := validateAlertModel(ctx, ds, sourceID, tempAlert); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAlertConfiguration, err)
	}

	// Execute query with timing
	timeout := models.DefaultQueryTimeoutSeconds
	startTime := time.Now()
	result, err := ds.EvaluateAlert(ctx, sourceID, datasource.AlertQueryRequest{
		Language:        tempAlert.QueryLanguage,
		Query:           tempAlert.Query,
		LookbackSeconds: tempAlert.LookbackSeconds,
		QueryTimeout:    &timeout,
	})
	executionTime := time.Since(startTime)

	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}

	// Extract numeric value from result. No matching rows is not an error —
	// ExtractFirstNumeric returns (0, nil) for an empty result — so check the
	// row count independently and surface a warning, otherwise the user can't
	// tell "waiting for data" from a real zero. An extraction failure on a
	// non-empty result is still a genuine error.
	value, err := util.ExtractFirstNumeric(result)
	var warnings []string
	if len(result.Logs) == 0 {
		warnings = append(warnings, "Query returned no rows. The alert will be evaluated when matching data exists. Using value 0 for threshold comparison.")
		value = 0
	} else if err != nil {
		return nil, fmt.Errorf("failed to extract numeric value from result: %w", err)
	}

	// Check if threshold would be met
	thresholdMet := compareAlertThreshold(value, req.ThresholdValue, req.ThresholdOperator)

	// Generate additional warnings
	warnings = append(warnings, generateQueryWarnings(tempAlert.Query, executionTime, result)...)

	return &models.TestAlertQueryResponse{
		Value:           value,
		ThresholdMet:    thresholdMet,
		ExecutionTimeMs: executionTime.Milliseconds(),
		RowsReturned:    len(result.Logs),
		Warnings:        warnings,
	}, nil
}

func compareAlertThreshold(value, threshold float64, operator models.AlertThresholdOperator) bool {
	switch operator {
	case models.AlertThresholdGreaterThan:
		return value > threshold
	case models.AlertThresholdGreaterThanOrEqual:
		return value >= threshold
	case models.AlertThresholdLessThan:
		return value < threshold
	case models.AlertThresholdLessThanOrEqual:
		return value <= threshold
	case models.AlertThresholdEqual:
		return math.Abs(value-threshold) < 1e-9
	case models.AlertThresholdNotEqual:
		return math.Abs(value-threshold) >= 1e-9
	default:
		return false
	}
}

func generateQueryWarnings(query string, executionTime time.Duration, result *models.QueryResult) []string {
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
		warnings = append(warnings, "Query appears to lack a time-based filter. Consider adding a time window (e.g., WHERE timestamp >= now() - INTERVAL 5 MINUTE) to improve performance and relevance.")
	}

	// Warn if result set is unexpectedly large
	if len(result.Logs) > 1 {
		warnings = append(warnings, fmt.Sprintf("Query returned %d rows. Alert queries should typically return a single numeric value (e.g., count, sum, avg).", len(result.Logs)))
	}

	return warnings
}
