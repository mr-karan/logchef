package victorialogs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

const defaultHealthTimeout = 5 * time.Second
const defaultValidationTimeout = 8 * time.Second

type Provider struct {
	client  *http.Client
	log     *slog.Logger
	mu      sync.RWMutex
	sources map[models.SourceID]models.VictoriaLogsConnectionInfo
	health  map[models.SourceID]models.SourceHealth
}

func NewProvider(log *slog.Logger) *Provider {
	return &Provider{
		client:  &http.Client{},
		log:     log.With("component", "victorialogs_provider"),
		sources: make(map[models.SourceID]models.VictoriaLogsConnectionInfo),
		health:  make(map[models.SourceID]models.SourceHealth),
	}
}

func (p *Provider) Type() models.SourceType {
	return models.SourceTypeVictoriaLogs
}

func (p *Provider) Capabilities() []datasource.Capability {
	return []datasource.Capability{
		datasource.CapabilitySchemaInspection,
		datasource.CapabilityHistogram,
		datasource.CapabilityFieldValues,
		datasource.CapabilitySourceInspection,
	}
}

func (p *Provider) SupportedQueryLanguages() []models.QueryLanguage {
	return []models.QueryLanguage{
		models.QueryLanguageLogchefQL,
		models.QueryLanguageLogsQL,
	}
}

func (p *Provider) SupportedSavedQueryEditorModes() []models.SavedQueryEditorMode {
	return []models.SavedQueryEditorMode{
		models.SavedQueryEditorModeBuilder,
		models.SavedQueryEditorModeNative,
	}
}

func (p *Provider) SupportedAlertEditorModes() []models.AlertEditorMode {
	return []models.AlertEditorMode{
		models.AlertEditorModeCondition,
		models.AlertEditorModeNative,
	}
}

func (p *Provider) PrepareSource(ctx context.Context, req *models.CreateSourceRequest) (*models.Source, error) {
	if req == nil {
		return nil, fmt.Errorf("create source request is required")
	}

	if err := datasource.ValidateCommonSourceFields(req.Name, req.Description, req.TTLDays); err != nil {
		return nil, err
	}

	conn, err := p.connectionFromConfig(req.Connection)
	if err != nil {
		return nil, err
	}
	if err := validateVictoriaLogsConnectionConfig("connection.", conn); err != nil {
		return nil, err
	}

	metaTSField := strings.TrimSpace(req.MetaTSField)
	if metaTSField == "" {
		metaTSField = "_time"
	}
	if !datasource.IsValidIdentifier(metaTSField) {
		return nil, &datasource.ValidationError{Field: "meta_ts_field", Message: "meta timestamp field contains invalid characters"}
	}

	metaSeverityField := strings.TrimSpace(req.MetaSeverityField)
	if metaSeverityField != "" && !datasource.IsValidIdentifier(metaSeverityField) {
		return nil, &datasource.ValidationError{Field: "meta_severity_field", Message: "meta severity field contains invalid characters"}
	}

	source := &models.Source{
		Name:              req.Name,
		MetaIsAutoCreated: false,
		SourceType:        models.SourceTypeVictoriaLogs,
		MetaTSField:       metaTSField,
		MetaSeverityField: metaSeverityField,
		ConnectionConfig:  req.Connection,
		Description:       req.Description,
		TTLDays:           req.TTLDays,
		Timestamps: models.Timestamps{
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	if err := source.SyncConnectionConfig(); err != nil {
		return nil, err
	}

	if err := p.validateConnectionAccess(ctx, conn); err != nil {
		return nil, err
	}

	return source, nil
}

func (p *Provider) ValidateConnection(ctx context.Context, req *models.ValidateConnectionRequest) (*models.ConnectionValidationResult, error) {
	if req == nil {
		return nil, fmt.Errorf("validate connection request is required")
	}

	conn, err := p.connectionFromConfig(req.Connection)
	if err != nil {
		return nil, err
	}
	if err := validateVictoriaLogsConnectionConfig("", conn); err != nil {
		return nil, err
	}

	if err := p.validateConnectionAccess(ctx, conn); err != nil {
		return nil, err
	}

	message := "Connection successful. VictoriaLogs query access is working."
	if strings.TrimSpace(conn.Tenant.AccountID) != "" || strings.TrimSpace(conn.Scope.Query) != "" {
		message = "Connection successful. Credentials, tenant scope, and immutable filters validated."
	}
	return &models.ConnectionValidationResult{Message: message}, nil
}

func (p *Provider) UpdateSource(ctx context.Context, source *models.Source, req *models.UpdateSourceRequest) (*datasource.SourceUpdateResult, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}
	if req == nil {
		return nil, fmt.Errorf("update source request is required")
	}

	changed, err := datasource.ApplyCommonSourceUpdates(source, req)
	if err != nil {
		return nil, err
	}

	connectionChanged := req.HasConnectionChanges()
	if connectionChanged {
		conn, err := p.connectionFromConfig(req.Connection)
		if err != nil {
			return nil, err
		}
		if err := validateVictoriaLogsConnectionConfig("connection.", conn); err != nil {
			return nil, err
		}
		if err := p.validateConnectionAccess(ctx, conn); err != nil {
			return nil, err
		}

		source.ConnectionConfig = req.Connection
		changed = true
	}

	if err := source.SyncConnectionConfig(); err != nil {
		return nil, err
	}

	return &datasource.SourceUpdateResult{
		Source:       source,
		Changed:      changed,
		Reinitialize: connectionChanged,
	}, nil
}

func (p *Provider) InitializeSource(ctx context.Context, source *models.Source) error {
	if source == nil {
		return fmt.Errorf("source is required")
	}

	conn, err := source.VictoriaLogsConnection()
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.sources[source.ID] = conn
	p.mu.Unlock()

	healthy, healthErr := p.checkHealth(ctx, source.ID, conn)
	p.updateHealth(source.ID, healthy, healthErr)
	if healthErr != nil {
		return healthErr
	}

	return nil
}

func (p *Provider) RemoveSource(sourceID models.SourceID) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.sources, sourceID)
	delete(p.health, sourceID)
	return nil
}

func (p *Provider) CheckSourceConnectionStatus(ctx context.Context, source *models.Source) bool {
	if source == nil {
		return false
	}

	conn, err := p.connectionForSource(source)
	if err != nil {
		p.updateHealth(source.ID, false, err)
		return false
	}

	healthy, healthErr := p.checkHealth(ctx, source.ID, conn)
	p.updateHealth(source.ID, healthy, healthErr)
	return healthy
}

func (p *Provider) GetSourceHealth(ctx context.Context, sourceID models.SourceID) models.SourceHealth {
	p.mu.RLock()
	conn, ok := p.sources[sourceID]
	health, hasHealth := p.health[sourceID]
	p.mu.RUnlock()

	if !ok {
		if hasHealth {
			return health
		}
		return models.SourceHealth{
			SourceID:    sourceID,
			Status:      models.HealthStatusUnhealthy,
			Error:       "victorialogs source not initialized",
			LastChecked: time.Now(),
		}
	}

	healthy, err := p.checkHealth(ctx, sourceID, conn)
	p.updateHealth(sourceID, healthy, err)

	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health[sourceID]
}

func (p *Provider) connectionFromConfig(raw json.RawMessage) (models.VictoriaLogsConnectionInfo, error) {
	var conn models.VictoriaLogsConnectionInfo
	if len(raw) == 0 {
		return conn, &datasource.ValidationError{Field: "connection", Message: "connection is required"}
	}
	if err := json.Unmarshal(raw, &conn); err != nil {
		return conn, &datasource.ValidationError{Field: "connection", Message: "invalid victorialogs connection payload", Err: err}
	}
	return conn, nil
}

func (p *Provider) connectionForSource(source *models.Source) (models.VictoriaLogsConnectionInfo, error) {
	p.mu.RLock()
	conn, ok := p.sources[source.ID]
	p.mu.RUnlock()
	if ok {
		return conn, nil
	}

	conn, err := source.VictoriaLogsConnection()
	if err != nil {
		return models.VictoriaLogsConnectionInfo{}, err
	}

	p.mu.Lock()
	p.sources[source.ID] = conn
	p.mu.Unlock()
	return conn, nil
}

func (p *Provider) checkHealth(ctx context.Context, sourceID models.SourceID, conn models.VictoriaLogsConnectionInfo) (bool, error) {
	if strings.TrimSpace(conn.BaseURL) == "" {
		return false, fmt.Errorf("victorialogs base_url is required")
	}

	healthCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		healthCtx, cancel = context.WithTimeout(ctx, defaultHealthTimeout)
		defer cancel()
	}

	healthURL, err := url.JoinPath(conn.BaseURL, "/health")
	if err != nil {
		return false, fmt.Errorf("invalid victorialogs base_url: %w", err)
	}

	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false, fmt.Errorf("create victorialogs health request: %w", err)
	}

	applyHeaders(req, conn)

	resp, err := p.client.Do(req)
	if err != nil {
		p.log.Warn("victorialogs health check failed", "source_id", sourceID, "error", err)
		return false, fmt.Errorf("victorialogs health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("victorialogs health check returned status %d", resp.StatusCode)
		p.log.Warn("victorialogs health check returned non-success status", "source_id", sourceID, "status_code", resp.StatusCode)
		return false, err
	}

	return true, nil
}

func validateVictoriaLogsConnectionConfig(fieldPrefix string, conn models.VictoriaLogsConnectionInfo) error {
	if err := datasource.ValidateVictoriaLogsConnection(fieldPrefix, conn.BaseURL); err != nil {
		return err
	}

	authMode := strings.ToLower(strings.TrimSpace(conn.Auth.Mode))
	switch authMode {
	case "", "none":
	case "basic":
		if strings.TrimSpace(conn.Auth.Username) == "" {
			return &datasource.ValidationError{Field: fieldPrefix + "auth.username", Message: "username is required for basic auth"}
		}
		if strings.TrimSpace(conn.Auth.Password) == "" {
			return &datasource.ValidationError{Field: fieldPrefix + "auth.password", Message: "password is required for basic auth"}
		}
	case "bearer":
		if strings.TrimSpace(conn.Auth.Token) == "" {
			return &datasource.ValidationError{Field: fieldPrefix + "auth.token", Message: "token is required for bearer auth"}
		}
	default:
		return &datasource.ValidationError{Field: fieldPrefix + "auth.mode", Message: "auth.mode must be one of none, basic, or bearer"}
	}

	accountID := strings.TrimSpace(conn.Tenant.AccountID)
	projectID := strings.TrimSpace(conn.Tenant.ProjectID)
	if (accountID == "") != (projectID == "") {
		return &datasource.ValidationError{
			Field:   fieldPrefix + "tenant",
			Message: "account_id and project_id must be provided together",
		}
	}

	return nil
}

func (p *Provider) validateConnectionAccess(ctx context.Context, conn models.VictoriaLogsConnectionInfo) error {
	if _, err := p.checkHealth(ctx, 0, conn); err != nil {
		return &datasource.ValidationError{Field: "connection.base_url", Message: "Failed to reach the VictoriaLogs server", Err: err}
	}
	if err := p.validateQueryAccess(ctx, conn); err != nil {
		return err
	}
	return nil
}

func (p *Provider) validateQueryAccess(ctx context.Context, conn models.VictoriaLogsConnectionInfo) error {
	validationCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		validationCtx, cancel = context.WithTimeout(ctx, defaultValidationTimeout)
		defer cancel()
	}

	endpoint, err := joinBaseURL(conn.BaseURL, "/select/logsql/field_names")
	if err != nil {
		return &datasource.ValidationError{Field: "connection.base_url", Message: "invalid VictoriaLogs base URL", Err: err}
	}

	now := time.Now().UTC()
	form := url.Values{}
	form.Set("query", "*")
	form.Set("start", formatAPITime(now.Add(-5*time.Minute)))
	form.Set("end", formatAPITime(now))
	form.Set("ignore_pipes", "1")
	applyScopeFilters(form, conn)

	req, err := http.NewRequestWithContext(validationCtx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return &datasource.ValidationError{Field: "connection.base_url", Message: "failed to create VictoriaLogs validation request", Err: err}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	applyHeaders(req, conn)

	resp, err := p.client.Do(req)
	if err != nil {
		return &datasource.ValidationError{Field: "connection.base_url", Message: "failed to call the VictoriaLogs query API", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	detail := sanitizeVictoriaLogsValidationBody(body)
	switch resp.StatusCode {
	case http.StatusBadRequest:
		return &datasource.ValidationError{
			Field:   "connection.scope",
			Message: fmt.Sprintf("VictoriaLogs rejected the tenant or immutable scope configuration%s", detail),
		}
	case http.StatusUnauthorized:
		return &datasource.ValidationError{
			Field:   "connection.auth",
			Message: fmt.Sprintf("VictoriaLogs rejected the provided credentials or token%s", detail),
		}
	case http.StatusForbidden:
		return &datasource.ValidationError{
			Field:   "connection.tenant",
			Message: fmt.Sprintf("VictoriaLogs denied access for the provided tenant or credentials%s", detail),
		}
	case http.StatusNotFound:
		return &datasource.ValidationError{
			Field:   "connection.base_url",
			Message: "VictoriaLogs query endpoint was not found. Check the base URL and any path prefix.",
		}
	default:
		return &datasource.ValidationError{
			Field:   "connection",
			Message: fmt.Sprintf("VictoriaLogs returned status %d%s", resp.StatusCode, detail),
		}
	}
}

func sanitizeVictoriaLogsValidationBody(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, "\n", " ")
	trimmed = strings.ReplaceAll(trimmed, "\t", " ")
	trimmed = strings.Join(strings.Fields(trimmed), " ")
	if len(trimmed) > 240 {
		trimmed = trimmed[:240] + "..."
	}
	return ": " + trimmed
}

func (p *Provider) updateHealth(sourceID models.SourceID, healthy bool, err error) {
	status := models.HealthStatusHealthy
	errMsg := ""
	if !healthy {
		status = models.HealthStatusUnhealthy
		if err != nil {
			errMsg = err.Error()
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.health[sourceID] = models.SourceHealth{
		SourceID:    sourceID,
		Status:      status,
		Error:       errMsg,
		LastChecked: time.Now(),
	}
}

func applyHeaders(req *http.Request, conn models.VictoriaLogsConnectionInfo) {
	switch strings.ToLower(strings.TrimSpace(conn.Auth.Mode)) {
	case "basic":
		req.SetBasicAuth(conn.Auth.Username, conn.Auth.Password)
	case "bearer":
		if strings.TrimSpace(conn.Auth.Token) != "" {
			req.Header.Set("Authorization", "Bearer "+conn.Auth.Token)
		}
	default:
		if strings.TrimSpace(conn.Auth.Token) != "" {
			req.Header.Set("Authorization", "Bearer "+conn.Auth.Token)
		} else if strings.TrimSpace(conn.Auth.Username) != "" {
			req.SetBasicAuth(conn.Auth.Username, conn.Auth.Password)
		}
	}

	if strings.TrimSpace(conn.Tenant.AccountID) != "" {
		req.Header.Set("AccountID", conn.Tenant.AccountID)
	}
	if strings.TrimSpace(conn.Tenant.ProjectID) != "" {
		req.Header.Set("ProjectID", conn.Tenant.ProjectID)
	}

	for key, value := range conn.Headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
}
