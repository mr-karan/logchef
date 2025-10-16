package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/sqlite"
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

func validateAlertRequest(req *models.CreateAlertRequest) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(req.Query) == "" {
		return fmt.Errorf("query is required")
	}
	if _, ok := validOperators[req.ThresholdOperator]; !ok {
		return fmt.Errorf("invalid threshold_operator %q", req.ThresholdOperator)
	}
	if req.FrequencySeconds <= 0 {
		return fmt.Errorf("frequency_seconds must be greater than zero")
	}
	if _, ok := validSeverities[req.Severity]; !ok {
		return fmt.Errorf("invalid severity %q", req.Severity)
	}
	return nil
}

func validateAlertRooms(ctx context.Context, db *sqlite.DB, teamID models.TeamID, roomIDs []models.RoomID) error {
	if len(roomIDs) == 0 {
		return fmt.Errorf("at least one room is required")
	}
	seen := make(map[models.RoomID]struct{}, len(roomIDs))
	for _, roomID := range roomIDs {
		if roomID == 0 {
			return fmt.Errorf("invalid room id 0")
		}
		if _, exists := seen[roomID]; exists {
			return fmt.Errorf("duplicate room id %d", roomID)
		}
		seen[roomID] = struct{}{}

		room, err := db.GetRoom(ctx, roomID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
				return fmt.Errorf("room %d not found", roomID)
			}
			return fmt.Errorf("failed to validate room %d: %w", roomID, err)
		}
		if room.TeamID != teamID {
			return fmt.Errorf("room %d does not belong to team %d", roomID, teamID)
		}
	}
	return nil
}

// CreateAlert creates a new alert rule for the specified team and source.
func CreateAlert(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, req *models.CreateAlertRequest) (*models.Alert, error) {
	if req == nil {
		return nil, ErrInvalidAlertConfiguration
	}
	if err := validateAlertRequest(req); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAlertConfiguration, err)
	}
	if err := validateAlertRooms(ctx, db, teamID, req.RoomIDs); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAlertConfiguration, err)
	}

	alert := &models.Alert{
		TeamID:            teamID,
		SourceID:          sourceID,
		Name:              strings.TrimSpace(req.Name),
		Description:       strings.TrimSpace(req.Description),
		Query:             strings.TrimSpace(req.Query),
		ThresholdOperator: req.ThresholdOperator,
		ThresholdValue:    req.ThresholdValue,
		FrequencySeconds:  req.FrequencySeconds,
		Severity:          req.Severity,
		RoomIDs:           req.RoomIDs,
		IsActive:          req.IsActive,
	}

	if err := db.CreateAlert(ctx, alert); err != nil {
		log.Error("failed to create alert", "team_id", teamID, "source_id", sourceID, "error", err)
		return nil, fmt.Errorf("failed to create alert: %w", err)
	}
	log.Info("alert created", "alert_id", alert.ID, "team_id", teamID, "source_id", sourceID)
	return alert, nil
}

// GetAlert retrieves a single alert by ID.
func GetAlert(ctx context.Context, db *sqlite.DB, log *slog.Logger, alertID models.AlertID) (*models.Alert, error) {
	alert, err := db.GetAlert(ctx, alertID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAlertNotFound
		}
		log.Error("failed to get alert", "alert_id", alertID, "error", err)
		return nil, fmt.Errorf("failed to get alert: %w", err)
	}
	return alert, nil
}

// UpdateAlert updates an existing alert rule.
func UpdateAlert(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, alertID models.AlertID, req *models.UpdateAlertRequest) (*models.Alert, error) {
	if req == nil {
		return nil, ErrInvalidAlertConfiguration
	}

	existing, err := db.GetAlertForTeamSource(ctx, teamID, sourceID, alertID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
			return nil, ErrAlertNotFound
		}
		log.Error("failed to load alert for update", "alert_id", alertID, "error", err)
		return nil, fmt.Errorf("failed to load alert: %w", err)
	}

	if req.Name != nil {
		existing.Name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		existing.Description = strings.TrimSpace(*req.Description)
	}
	if req.Query != nil {
		if strings.TrimSpace(*req.Query) == "" {
			return nil, fmt.Errorf("%w: query is required", ErrInvalidAlertConfiguration)
		}
		existing.Query = strings.TrimSpace(*req.Query)
	}
	if req.ThresholdOperator != nil {
		if _, ok := validOperators[*req.ThresholdOperator]; !ok {
			return nil, fmt.Errorf("%w: invalid threshold_operator %q", ErrInvalidAlertConfiguration, *req.ThresholdOperator)
		}
		existing.ThresholdOperator = *req.ThresholdOperator
	}
	if req.ThresholdValue != nil {
		existing.ThresholdValue = *req.ThresholdValue
	}
	if req.FrequencySeconds != nil {
		if *req.FrequencySeconds <= 0 {
			return nil, fmt.Errorf("%w: frequency_seconds must be greater than zero", ErrInvalidAlertConfiguration)
		}
		existing.FrequencySeconds = *req.FrequencySeconds
	}
	if req.Severity != nil {
		if _, ok := validSeverities[*req.Severity]; !ok {
			return nil, fmt.Errorf("%w: invalid severity %q", ErrInvalidAlertConfiguration, *req.Severity)
		}
		existing.Severity = *req.Severity
	}
	if req.RoomIDs != nil {
		roomIDs := make([]models.RoomID, len(*req.RoomIDs))
		copy(roomIDs, *req.RoomIDs)
		if err := validateAlertRooms(ctx, db, teamID, roomIDs); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidAlertConfiguration, err)
		}
		existing.RoomIDs = roomIDs
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}

	if err := db.UpdateAlert(ctx, existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
			return nil, ErrAlertNotFound
		}
		log.Error("failed to update alert", "alert_id", alertID, "error", err)
		return nil, fmt.Errorf("failed to update alert: %w", err)
	}

	updated, err := db.GetAlertForTeamSource(ctx, teamID, sourceID, alertID)
	if err != nil {
		log.Warn("alert updated but fetching updated record failed", "alert_id", alertID, "error", err)
		return existing, nil
	}
	return updated, nil
}

// DeleteAlert removes an alert rule.
func DeleteAlert(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, alertID models.AlertID) error {
	if _, err := db.GetAlertForTeamSource(ctx, teamID, sourceID, alertID); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
			return ErrAlertNotFound
		}
		return fmt.Errorf("failed to validate alert ownership: %w", err)
	}
	if err := db.DeleteAlert(ctx, alertID); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
			return ErrAlertNotFound
		}
		log.Error("failed to delete alert", "alert_id", alertID, "error", err)
		return fmt.Errorf("failed to delete alert: %w", err)
	}
	log.Info("alert deleted", "alert_id", alertID, "team_id", teamID, "source_id", sourceID)
	return nil
}

// ListAlertsByTeamSource returns all alerts for a team/source pair.
func ListAlertsByTeamSource(ctx context.Context, db *sqlite.DB, teamID models.TeamID, sourceID models.SourceID) ([]*models.Alert, error) {
	alerts, err := db.ListAlertsByTeamAndSource(ctx, teamID, sourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}
	return alerts, nil
}

// ListAlertHistory retrieves a limited set of alert history entries.
func ListAlertHistory(ctx context.Context, db *sqlite.DB, alertID models.AlertID, limit int) ([]*models.AlertHistoryEntry, error) {
	history, err := db.ListAlertHistory(ctx, alertID, limit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
			return []*models.AlertHistoryEntry{}, nil
		}
		return nil, fmt.Errorf("failed to list alert history: %w", err)
	}
	return history, nil
}

// ResolveAlert manually resolves the most recent triggered history entry.
func ResolveAlert(ctx context.Context, db *sqlite.DB, log *slog.Logger, alertID models.AlertID, message string) error {
	entry, err := db.GetLatestUnresolvedAlertHistory(ctx, alertID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
			return ErrAlertNotFound
		}
		return fmt.Errorf("failed to find unresolved alert history: %w", err)
	}
	if err := db.ResolveAlertHistory(ctx, entry.ID, message); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
			return ErrAlertNotFound
		}
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}
	log.Info("alert history resolved", "alert_id", alertID, "history_id", entry.ID)
	return nil
}

// TestAlertQuery executes a test query to validate alert configuration and show performance metrics.
func TestAlertQuery(ctx context.Context, db *sqlite.DB, ch *clickhouse.Manager, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, req *models.TestAlertQueryRequest) (*models.TestAlertQueryResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("test query request is required")
	}
	if strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}

	// Load source to verify access
	_, err := db.GetSource(ctx, sourceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
			return nil, fmt.Errorf("source not found")
		}
		return nil, fmt.Errorf("failed to load source: %w", err)
	}

	query := req.Query

	// Get ClickHouse connection
	client, err := ch.GetConnection(sourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain ClickHouse connection: %w", err)
	}

	// Execute query with timing
	timeout := models.DefaultQueryTimeoutSeconds
	startTime := time.Now()
	result, err := client.QueryWithTimeout(ctx, query, &timeout)
	executionTime := time.Since(startTime)

	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}

	// Extract numeric value from result
	value, err := extractFirstNumericValue(result)
	if err != nil {
		return nil, fmt.Errorf("failed to extract numeric value from result: %w", err)
	}

	// Check if threshold would be met
	thresholdMet := compareAlertThreshold(value, req.ThresholdValue, req.ThresholdOperator)

	// Generate warnings
	warnings := generateQueryWarnings(query, executionTime, result)

	return &models.TestAlertQueryResponse{
		Value:           value,
		ThresholdMet:    thresholdMet,
		ExecutionTimeMs: executionTime.Milliseconds(),
		RowsReturned:    len(result.Logs),
		Warnings:        warnings,
	}, nil
}

func extractFirstNumericValue(result *models.QueryResult) (float64, error) {
	if result == nil || len(result.Logs) == 0 {
		return 0, fmt.Errorf("query returned no rows")
	}
	row := result.Logs[0]
	if len(result.Columns) == 0 {
		return 0, fmt.Errorf("query returned no columns")
	}

	// Try the first column
	firstColumn := result.Columns[0].Name
	rawValue, ok := row[firstColumn]
	if !ok {
		// Fallback: try any value in the row
		for _, v := range row {
			rawValue = v
			ok = true
			break
		}
	}
	if !ok {
		return 0, fmt.Errorf("unable to locate numeric value in query result")
	}

	// Convert to float64
	switch v := rawValue.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("unable to parse numeric value %q: %w", v, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported result type %T", rawValue)
	}
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
