package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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
	switch key {
	case "alerts.alertmanager_url", "alerts.external_url", "alerts.frontend_url", "server.frontend_url", "ai.base_url":
		if value != "" {
			parsedURL, err := url.Parse(value)
			if err != nil {
				return fmt.Errorf("invalid URL format")
			}
			if parsedURL.Scheme != "" && parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
				return fmt.Errorf("URL must use http or https scheme")
			}
		}
	case "alerts.history_limit", "auth.max_concurrent_sessions", "ai.max_tokens":
		intVal, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("must be a valid integer")
		}
		if intVal <= 0 {
			return fmt.Errorf("must be greater than 0")
		}
	case "ai.temperature":
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("must be a valid number")
		}
		if floatVal < 0.0 || floatVal > 1.0 {
			return fmt.Errorf("must be between 0.0 and 1.0")
		}
	}
	return nil
}

// TestAlertmanagerRequest represents a request to test Alertmanager connection.
type TestAlertmanagerRequest struct {
	URL                   string `json:"url"`
	TLSInsecureSkipVerify bool   `json:"tls_insecure_skip_verify"`
	Timeout               string `json:"timeout,omitempty"` // Duration string (e.g., "5s")
}

// handleTestAlertmanagerConnection tests connectivity to an Alertmanager instance.
// POST /api/v1/admin/settings/test-alertmanager
func (s *Server) handleTestAlertmanagerConnection(c *fiber.Ctx) error {
	var req TestAlertmanagerRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.URL == "" {
		return SendError(c, fiber.StatusBadRequest, "alertmanager URL is required")
	}

	// Validate URL format
	parsedURL, err := url.Parse(req.URL)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, fmt.Sprintf("invalid URL format: %v", err))
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return SendError(c, fiber.StatusBadRequest, "URL must use http or https scheme")
	}

	// Parse timeout
	timeout := 5 * time.Second
	if req.Timeout != "" {
		parsedTimeout, err := time.ParseDuration(req.Timeout)
		if err != nil {
			return SendError(c, fiber.StatusBadRequest, fmt.Sprintf("invalid timeout format: %v", err))
		}
		timeout = parsedTimeout
	}

	// Create a temporary Alertmanager client for testing
	client, err := alerts.NewAlertmanagerClient(alerts.ClientOptions{
		BaseURL:       req.URL,
		Timeout:       timeout,
		SkipTLSVerify: req.TLSInsecureSkipVerify,
		Logger:        s.log,
	})
	if err != nil {
		s.log.Error("failed to create alertmanager client for testing", "error", err, "url", req.URL)
		return SendError(c, fiber.StatusBadRequest, fmt.Sprintf("failed to create alertmanager client: %v", err))
	}

	// Perform health check
	if err := client.HealthCheck(c.Context()); err != nil {
		s.log.Warn("alertmanager health check failed", "error", err, "url", req.URL)
		return SendError(c, fiber.StatusBadGateway, fmt.Sprintf("Alertmanager health check failed: %v", err))
	}

	s.log.Info("alertmanager health check successful", "url", req.URL)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"message": "Successfully connected to Alertmanager",
		"url":     req.URL,
	})
}

// GetAlertmanagerRoutingRequest contains the optional parameters for fetching routing config.
type GetAlertmanagerRoutingRequest struct {
	URL                   string `json:"url,omitempty"`      // Optional: override the configured URL
	TLSInsecureSkipVerify bool   `json:"tls_skip_verify"`    // Skip TLS verification
	Timeout               string `json:"timeout,omitempty"`  // Duration string (e.g., "5s")
	Username              string `json:"username,omitempty"` // Basic auth username
	Password              string `json:"password,omitempty"` // Basic auth password
}

// handleGetAlertmanagerRouting fetches the routing configuration from Alertmanager.
// POST /api/v1/admin/settings/alertmanager-routing
func (s *Server) handleGetAlertmanagerRouting(c *fiber.Ctx) error {
	var req GetAlertmanagerRoutingRequest
	if err := c.BodyParser(&req); err != nil {
		// Body is optional, so ignore parse errors
		req = GetAlertmanagerRoutingRequest{}
	}

	// Use configured URL if not provided in request
	alertmanagerURL := req.URL
	if alertmanagerURL == "" {
		alertmanagerURL = s.config.Alerts.AlertmanagerURL
	}

	if alertmanagerURL == "" {
		return SendError(c, fiber.StatusBadRequest, "Alertmanager URL is not configured. Please configure it in Settings > Alerts.")
	}

	// Validate URL format
	parsedURL, err := url.Parse(alertmanagerURL)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, fmt.Sprintf("invalid URL format: %v", err))
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return SendError(c, fiber.StatusBadRequest, "URL must use http or https scheme")
	}

	// Parse timeout
	timeout := 10 * time.Second
	if req.Timeout != "" {
		parsedTimeout, err := time.ParseDuration(req.Timeout)
		if err != nil {
			return SendError(c, fiber.StatusBadRequest, fmt.Sprintf("invalid timeout format: %v", err))
		}
		timeout = parsedTimeout
	}

	// Set up additional headers for basic auth if provided
	var headers http.Header
	if req.Username != "" && req.Password != "" {
		headers = make(http.Header)
		headers.Set("Authorization", "Basic "+basicAuth(req.Username, req.Password))
	}

	// Use TLS skip verify from request or config
	tlsSkipVerify := req.TLSInsecureSkipVerify || s.config.Alerts.TLSInsecureSkipVerify

	// Create a temporary Alertmanager client
	client, err := alerts.NewAlertmanagerClient(alerts.ClientOptions{
		BaseURL:           alertmanagerURL,
		Timeout:           timeout,
		SkipTLSVerify:     tlsSkipVerify,
		Logger:            s.log,
		AdditionalHeaders: headers,
	})
	if err != nil {
		s.log.Error("failed to create alertmanager client", "error", err, "url", alertmanagerURL)
		return SendError(c, fiber.StatusBadRequest, fmt.Sprintf("failed to create alertmanager client: %v", err))
	}

	// Fetch routing configuration
	routingInfo, err := client.GetRoutingConfig(c.Context())
	if err != nil {
		s.log.Warn("failed to fetch alertmanager routing config", "error", err, "url", alertmanagerURL)
		return SendError(c, fiber.StatusBadGateway, fmt.Sprintf("Failed to fetch Alertmanager routing: %v", err))
	}

	s.log.Info("fetched alertmanager routing config", "url", alertmanagerURL, "receivers", len(routingInfo.Receivers))
	return SendSuccess(c, fiber.StatusOK, routingInfo)
}

// basicAuth encodes credentials for HTTP Basic Authentication.
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
