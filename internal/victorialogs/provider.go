package victorialogs

import (
	"context"
	"encoding/json"
	"fmt"
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

type Provider struct {
	client  *http.Client
	log     *slog.Logger
	mu      sync.RWMutex
	sources map[models.SourceID]models.VictoriaLogsConnectionInfo
	health  map[models.SourceID]models.SourceHealth
}

func NewProvider(log *slog.Logger) *Provider {
	return &Provider{
		client: &http.Client{
			Timeout: defaultHealthTimeout,
		},
		log:     log.With("component", "victorialogs_provider"),
		sources: make(map[models.SourceID]models.VictoriaLogsConnectionInfo),
		health:  make(map[models.SourceID]models.SourceHealth),
	}
}

func (p *Provider) Type() models.SourceType {
	return models.SourceTypeVictoriaLogs
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
	if err := datasource.ValidateVictoriaLogsConnection("connection.", conn.BaseURL); err != nil {
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

	if _, err := p.checkHealth(ctx, 0, conn); err != nil {
		return nil, &datasource.ValidationError{Field: "connection", Message: "Failed to connect to VictoriaLogs", Err: err}
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
	if err := datasource.ValidateVictoriaLogsConnection("", conn.BaseURL); err != nil {
		return nil, err
	}

	if _, err := p.checkHealth(ctx, 0, conn); err != nil {
		return nil, &datasource.ValidationError{Field: "connection", Message: "Failed to connect to VictoriaLogs", Err: err}
	}

	return &models.ConnectionValidationResult{Message: "Connection successful"}, nil
}

func (p *Provider) QueryLogs(context.Context, *models.Source, datasource.QueryRequest) (*models.QueryResult, error) {
	return nil, fmt.Errorf("victorialogs query execution is not implemented yet: %w", datasource.ErrOperationNotSupported)
}

func (p *Provider) GetSourceSchema(context.Context, *models.Source) ([]models.ColumnInfo, error) {
	return nil, fmt.Errorf("victorialogs schema inspection is not implemented yet: %w", datasource.ErrOperationNotSupported)
}

func (p *Provider) Histogram(context.Context, *models.Source, datasource.HistogramRequest) (*datasource.HistogramResult, error) {
	return nil, fmt.Errorf("victorialogs histogram is not implemented yet: %w", datasource.ErrOperationNotSupported)
}

func (p *Provider) LogContext(context.Context, *models.Source, datasource.LogContextRequest) (*datasource.LogContextResult, error) {
	return nil, fmt.Errorf("victorialogs log context is not implemented yet: %w", datasource.ErrOperationNotSupported)
}

func (p *Provider) EvaluateAlert(context.Context, *models.Source, datasource.AlertQueryRequest) (*models.QueryResult, error) {
	return nil, fmt.Errorf("victorialogs alert evaluation is not implemented yet: %w", datasource.ErrOperationNotSupported)
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

	healthURL, err := url.JoinPath(conn.BaseURL, "/health")
	if err != nil {
		return false, fmt.Errorf("invalid victorialogs base_url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
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
