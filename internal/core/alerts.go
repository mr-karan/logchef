package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

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

var validQueryTypes = map[models.AlertQueryType]struct{}{
	models.AlertQueryTypeSQL:          {},
	models.AlertQueryTypeLogCondition: {},
}

var validChannelTypes = map[models.AlertChannelType]struct{}{
	models.AlertChannelEmail:   {},
	models.AlertChannelSlack:   {},
	models.AlertChannelWebhook: {},
}

// validateAlertChannels ensures the provided notification configuration is supported.
func validateAlertChannels(channels []models.AlertChannel) error {
	if len(channels) == 0 {
		return fmt.Errorf("at least one notification channel is required")
	}
	for idx, ch := range channels {
		if strings.TrimSpace(ch.Target) == "" {
			return fmt.Errorf("channel target is required (index %d)", idx)
		}
		if _, ok := validChannelTypes[ch.Type]; !ok {
			return fmt.Errorf("unsupported channel type %q", ch.Type)
		}
	}
	return nil
}

func validateAlertRequest(req *models.CreateAlertRequest) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if _, ok := validQueryTypes[req.QueryType]; !ok {
		return fmt.Errorf("invalid query_type %q", req.QueryType)
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
	if req.LookbackSeconds <= 0 {
		return fmt.Errorf("lookback_seconds must be greater than zero")
	}
	if _, ok := validSeverities[req.Severity]; !ok {
		return fmt.Errorf("invalid severity %q", req.Severity)
	}
	if err := validateAlertChannels(req.Channels); err != nil {
		return err
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

	alert := &models.Alert{
		TeamID:            teamID,
		SourceID:          sourceID,
		Name:              strings.TrimSpace(req.Name),
		Description:       strings.TrimSpace(req.Description),
		QueryType:         req.QueryType,
		Query:             strings.TrimSpace(req.Query),
		LookbackSeconds:   req.LookbackSeconds,
		ThresholdOperator: req.ThresholdOperator,
		ThresholdValue:    req.ThresholdValue,
		FrequencySeconds:  req.FrequencySeconds,
		Severity:          req.Severity,
		Channels:          req.Channels,
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
	if req.QueryType != nil {
		if _, ok := validQueryTypes[*req.QueryType]; !ok {
			return nil, fmt.Errorf("%w: invalid query_type %q", ErrInvalidAlertConfiguration, *req.QueryType)
		}
		existing.QueryType = *req.QueryType
	}
	if req.Query != nil {
		if strings.TrimSpace(*req.Query) == "" {
			return nil, fmt.Errorf("%w: query is required", ErrInvalidAlertConfiguration)
		}
		existing.Query = strings.TrimSpace(*req.Query)
	}
	if req.LookbackSeconds != nil {
		if *req.LookbackSeconds <= 0 {
			return nil, fmt.Errorf("%w: lookback_seconds must be greater than zero", ErrInvalidAlertConfiguration)
		}
		existing.LookbackSeconds = *req.LookbackSeconds
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
	if req.Channels != nil {
		if err := validateAlertChannels(*req.Channels); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidAlertConfiguration, err)
		}
		existing.Channels = *req.Channels
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
