package server

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// SystemSettingResponse represents a setting in API responses.
type SystemSettingResponse struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	ValueType   string `json:"value_type"`
	Category    string `json:"category"`
	Description string `json:"description,omitempty"`
	IsSensitive bool   `json:"is_sensitive"`
	MaskedValue string `json:"masked_value,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// UpdateSettingRequest represents a request to update a setting.
type UpdateSettingRequest struct {
	Value       string `json:"value"`
	ValueType   string `json:"value_type"`
	Category    string `json:"category"`
	Description string `json:"description"`
	IsSensitive bool   `json:"is_sensitive"`
}

// SettingsByCategoryResponse groups settings by category.
type SettingsByCategoryResponse struct {
	Category string                  `json:"category"`
	Settings []SystemSettingResponse `json:"settings"`
}

// handleListSettings returns all system settings grouped by category.
// GET /api/v1/admin/settings
func (s *Server) handleListSettings(c *fiber.Ctx) error {
	settings, err := s.sqlite.ListSettings(c.Context())
	if err != nil {
		s.log.Error("failed to list settings", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "failed to retrieve settings")
	}

	// Group settings by category
	categoriesMap := make(map[string][]SystemSettingResponse)
	for i := range settings {
		response := s.settingToResponse(settings[i])
		categoriesMap[settings[i].Category] = append(categoriesMap[settings[i].Category], response)
	}

	// Convert map to slice for ordered response
	var result []SettingsByCategoryResponse
	for category, items := range categoriesMap {
		result = append(result, SettingsByCategoryResponse{
			Category: category,
			Settings: items,
		})
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

// handleListSettingsByCategory returns settings for a specific category.
// GET /api/v1/admin/settings/category/:category
func (s *Server) handleListSettingsByCategory(c *fiber.Ctx) error {
	category := c.Params("category")
	if category == "" {
		return SendError(c, fiber.StatusBadRequest, "category parameter is required")
	}

	settings, err := s.sqlite.ListSettingsByCategory(c.Context(), category)
	if err != nil {
		s.log.Error("failed to list settings by category", "category", category, "error", err)
		return SendError(c, fiber.StatusInternalServerError, "failed to retrieve settings")
	}

	var response []SystemSettingResponse
	for i := range settings {
		response = append(response, s.settingToResponse(settings[i]))
	}

	return SendSuccess(c, fiber.StatusOK, response)
}

// handleGetSetting returns a specific setting by key.
// GET /api/v1/admin/settings/:key
func (s *Server) handleGetSetting(c *fiber.Ctx) error {
	key := c.Params("key")
	if key == "" {
		return SendError(c, fiber.StatusBadRequest, "key parameter is required")
	}

	value, err := s.sqlite.GetSetting(c.Context(), key)
	if err != nil {
		s.log.Error("failed to get setting", "key", key, "error", err)
		return SendError(c, fiber.StatusNotFound, "setting not found")
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"key": key, "value": value})
}

// handleUpdateSetting updates or creates a setting.
// PUT /api/v1/admin/settings/:key
func (s *Server) handleUpdateSetting(c *fiber.Ctx) error {
	// Get user from context
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context despite requireAuth middleware")
		return SendError(c, fiber.StatusInternalServerError, "Error retrieving user context")
	}

	key := c.Params("key")
	if key == "" {
		return SendError(c, fiber.StatusBadRequest, "key parameter is required")
	}

	var req UpdateSettingRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Validate the setting value based on its type
	if err := s.validateSettingValue(req.Value, req.ValueType); err != nil {
		return SendError(c, fiber.StatusBadRequest, fmt.Sprintf("invalid value: %v", err))
	}

	// Validate category
	validCategories := map[string]bool{"alerts": true, "ai": true, "auth": true, "server": true}
	if !validCategories[req.Category] {
		return SendError(c, fiber.StatusBadRequest, "invalid category (must be: alerts, ai, auth, or server)")
	}

	// Additional validation for specific settings
	if err := s.validateSpecificSetting(key, req.Value); err != nil {
		return SendError(c, fiber.StatusBadRequest, fmt.Sprintf("validation failed: %v", err))
	}

	// Upsert the setting
	if err := s.sqlite.UpsertSetting(c.Context(), key, req.Value, req.ValueType, req.Category, req.Description, req.IsSensitive); err != nil {
		s.log.Error("failed to update setting", "key", key, "error", err)
		return SendError(c, fiber.StatusInternalServerError, "failed to update setting")
	}

	s.log.Info("setting updated", "key", key, "user", user.Email)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "setting updated successfully", "key": key})
}

// handleDeleteSetting deletes a setting.
// DELETE /api/v1/admin/settings/:key
func (s *Server) handleDeleteSetting(c *fiber.Ctx) error {
	// Get user from context
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context despite requireAuth middleware")
		return SendError(c, fiber.StatusInternalServerError, "Error retrieving user context")
	}

	key := c.Params("key")
	if key == "" {
		return SendError(c, fiber.StatusBadRequest, "key parameter is required")
	}

	if err := s.sqlite.DeleteSetting(c.Context(), key); err != nil {
		s.log.Error("failed to delete setting", "key", key, "error", err)
		return SendError(c, fiber.StatusInternalServerError, "failed to delete setting")
	}

	s.log.Info("setting deleted", "key", key, "user", user.Email)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "setting deleted successfully"})
}

// settingToResponse converts a database setting to API response format.
func (s *Server) settingToResponse(setting sqlc.SystemSetting) SystemSettingResponse {
	response := SystemSettingResponse{
		Key:         setting.Key,
		Value:       setting.Value,
		ValueType:   setting.ValueType,
		Category:    setting.Category,
		IsSensitive: setting.IsSensitive == 1,
		CreatedAt:   setting.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   setting.UpdatedAt.Format(time.RFC3339),
	}

	if setting.Description.Valid {
		response.Description = setting.Description.String
	}

	// Mask sensitive values in responses
	if response.IsSensitive && response.Value != "" {
		response.MaskedValue = "********"
	}

	return response
}

// validateSettingValue validates a setting value based on its type.
func (s *Server) validateSettingValue(value, valueType string) error {
	switch valueType {
	case "boolean":
		_, err := strconv.ParseBool(value)
		return err
	case "number":
		_, err := strconv.ParseFloat(value, 64)
		return err
	case "duration":
		_, err := time.ParseDuration(value)
		return err
	case "string":
		return nil // Strings are always valid
	default:
		return fmt.Errorf("invalid value_type: %s (must be: string, number, boolean, or duration)", valueType)
	}
}

// validateSpecificSetting performs additional validation for specific settings.
func (s *Server) validateSpecificSetting(key, value string) error {
	validator, ok := specificSettingValidators[key]
	if !ok {
		return nil
	}
	return validator(value)
}

var specificSettingValidators = map[string]func(string) error{
	"alerts.external_url":          validateOptionalURL,
	"alerts.frontend_url":          validateOptionalURL,
	"server.frontend_url":          validateOptionalURL,
	"ai.base_url":                  validateOptionalURL,
	"alerts.smtp_port":             validateNonNegativeInt,
	"alerts.history_limit":         validatePositiveInt,
	"auth.max_concurrent_sessions": validatePositiveInt,
	"ai.max_tokens":                validatePositiveInt,
	"alerts.smtp_security":         validateSMTPSecurity,
	"alerts.smtp_from":             validateEmailAddress,
	"alerts.smtp_reply_to":         validateEmailAddress,
	"ai.temperature":               validateTemperature,
}

func validateOptionalURL(value string) error {
	if value == "" {
		return nil
	}
	parsedURL, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid URL format")
	}
	if parsedURL.Scheme != "" && parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("URL must use http or https scheme")
	}
	return nil
}

func validateNonNegativeInt(value string) error {
	intVal, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("must be a valid integer")
	}
	if intVal < 0 {
		return fmt.Errorf("must be 0 or greater")
	}
	return nil
}

func validatePositiveInt(value string) error {
	intVal, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("must be a valid integer")
	}
	if intVal <= 0 {
		return fmt.Errorf("must be greater than 0")
	}
	return nil
}

func validateSMTPSecurity(value string) error {
	if value == "" {
		return nil
	}
	security := strings.ToLower(value)
	if security != "none" && security != "starttls" && security != "tls" {
		return fmt.Errorf("smtp_security must be none, starttls, or tls")
	}
	return nil
}

func validateEmailAddress(value string) error {
	if value != "" && !strings.Contains(value, "@") {
		return fmt.Errorf("must be a valid email address")
	}
	return nil
}

func validateTemperature(value string) error {
	floatVal, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("must be a valid number")
	}
	if floatVal < 0.0 || floatVal > 1.0 {
		return fmt.Errorf("must be between 0.0 and 1.0")
	}
	return nil
}

// TestEmailRequest represents a request to send a test email.
type TestEmailRequest struct {
	RecipientEmail string `json:"recipient_email"`
}

// TestWebhookRequest represents a request to send a test webhook.
type TestWebhookRequest struct {
	WebhookURL string `json:"webhook_url"`
}

func (s *Server) loadSMTPConfig(ctx context.Context) alerts.EmailSenderOptions {
	return alerts.EmailSenderOptions{
		Host:          s.sqlite.GetSettingWithDefault(ctx, "alerts.smtp_host", ""),
		Port:          s.sqlite.GetIntSetting(ctx, "alerts.smtp_port", 587),
		Username:      s.sqlite.GetSettingWithDefault(ctx, "alerts.smtp_username", ""),
		Password:      s.sqlite.GetSettingWithDefault(ctx, "alerts.smtp_password", ""),
		From:          s.sqlite.GetSettingWithDefault(ctx, "alerts.smtp_from", ""),
		ReplyTo:       s.sqlite.GetSettingWithDefault(ctx, "alerts.smtp_reply_to", ""),
		Security:      s.sqlite.GetSettingWithDefault(ctx, "alerts.smtp_security", "starttls"),
		Timeout:       s.sqlite.GetDurationSetting(ctx, "alerts.request_timeout", 5*time.Second),
		SkipTLSVerify: s.sqlite.GetBoolSetting(ctx, "alerts.tls_insecure_skip_verify", false),
		Logger:        s.log,
	}
}

// handleTestEmail sends a test email to verify SMTP configuration.
// POST /api/v1/admin/settings/test-email
func (s *Server) handleTestEmail(c *fiber.Ctx) error {
	// Get current user for default recipient and audit log
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context despite requireAuth middleware")
		return SendError(c, fiber.StatusInternalServerError, "Error retrieving user context")
	}

	var req TestEmailRequest
	if err := c.BodyParser(&req); err != nil {
		// Allow empty body - will use current user's email
		req = TestEmailRequest{}
	}

	// Default to current user's email if not provided
	recipientEmail := strings.TrimSpace(req.RecipientEmail)
	if recipientEmail == "" {
		recipientEmail = user.Email
	}

	// Validate email format
	if !strings.Contains(recipientEmail, "@") {
		return SendError(c, fiber.StatusBadRequest, "Invalid recipient email address")
	}

	// Load SMTP config from DB
	smtpConfig := s.loadSMTPConfig(c.Context())

	// Validate SMTP is configured
	if smtpConfig.Host == "" || smtpConfig.Port == 0 || smtpConfig.From == "" {
		return SendError(c, fiber.StatusBadRequest, "SMTP is not configured. Please configure smtp_host, smtp_port, and smtp_from in settings.")
	}

	// Create email sender
	sender := alerts.NewEmailSender(smtpConfig)

	// Build test notification
	notification := alerts.AlertNotification{
		AlertName:       "Test Alert",
		Description:     "This is a test notification to verify your SMTP configuration.",
		Status:          models.AlertStatusTriggered,
		Severity:        models.AlertSeverityInfo,
		TeamName:        "Test Team",
		SourceName:      "Test Source",
		Value:           1.0,
		ThresholdOp:     models.AlertThresholdGreaterThan,
		ThresholdValue:  0.0,
		FrequencySecs:   60,
		LookbackSecs:    300,
		Query:           "SELECT 1",
		TriggeredAt:     time.Now(),
		Message:         "Test notification - please ignore",
		RecipientEmails: []string{recipientEmail},
	}

	// Send test email
	if err := sender.Send(c.Context(), notification); err != nil {
		s.log.Error("failed to send test email", "error", err, "recipient", recipientEmail, "user", user.Email)
		return SendError(c, fiber.StatusBadGateway, fmt.Sprintf("Failed to send test email: %v", err))
	}

	s.log.Info("test email sent successfully", "recipient", recipientEmail, "user", user.Email)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"message":   "Test email sent successfully",
		"recipient": recipientEmail,
	})
}

// handleTestWebhook sends a test webhook to verify webhook configuration.
// POST /api/v1/admin/settings/test-webhook
func (s *Server) handleTestWebhook(c *fiber.Ctx) error {
	// Get current user for audit log
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context despite requireAuth middleware")
		return SendError(c, fiber.StatusInternalServerError, "Error retrieving user context")
	}

	var req TestWebhookRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	webhookURL := strings.TrimSpace(req.WebhookURL)
	if webhookURL == "" {
		return SendError(c, fiber.StatusBadRequest, "Webhook URL is required")
	}

	// Validate webhook URL format
	parsedURL, err := url.Parse(webhookURL)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid webhook URL format")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return SendError(c, fiber.StatusBadRequest, "Webhook URL must use http or https scheme")
	}

	sender := alerts.NewWebhookSender(alerts.WebhookSenderOptions{
		Timeout:       s.sqlite.GetDurationSetting(c.Context(), "alerts.request_timeout", 5*time.Second),
		SkipTLSVerify: s.sqlite.GetBoolSetting(c.Context(), "alerts.tls_insecure_skip_verify", false),
		Logger:        s.log,
	})

	// Build test notification
	notification := alerts.AlertNotification{
		AlertName:      "Test Alert",
		Description:    "This is a test notification to verify your webhook configuration.",
		Status:         models.AlertStatusTriggered,
		Severity:       models.AlertSeverityInfo,
		TeamName:       "Test Team",
		SourceName:     "Test Source",
		Value:          1.0,
		ThresholdOp:    models.AlertThresholdGreaterThan,
		ThresholdValue: 0.0,
		FrequencySecs:  60,
		LookbackSecs:   300,
		Query:          "SELECT 1",
		TriggeredAt:    time.Now(),
		Message:        "Test notification - please ignore",
		WebhookURLs:    []string{webhookURL},
	}

	// Send test webhook
	if err := sender.Send(c.Context(), notification); err != nil {
		s.log.Error("failed to send test webhook", "error", err, "url", webhookURL, "user", user.Email)
		return SendError(c, fiber.StatusBadGateway, fmt.Sprintf("Failed to send test webhook: %v", err))
	}

	s.log.Info("test webhook sent successfully", "url", webhookURL, "user", user.Email)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"message": "Test webhook sent successfully",
		"url":     webhookURL,
	})
}
