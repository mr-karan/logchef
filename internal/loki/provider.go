// Package loki is the datasource provider for Grafana Loki (RFC 0011).
package loki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	defaultValidationTimeout = 8 * time.Second
	// dialTimeout and responseHeaderTimeout match the VictoriaLogs transport,
	// so a black-holed endpoint fails fast without a whole-request deadline.
	dialTimeout           = 5 * time.Second
	responseHeaderTimeout = 30 * time.Second
	maxRedirects          = 3
	// validationProbeWindow is how far back the validation probes read.
	validationProbeWindow = "5m"
)

type Provider struct {
	client *http.Client
	log    *slog.Logger
	mu     sync.RWMutex
	// sources caches each initialized source's connection.
	sources map[models.SourceID]models.LokiConnectionInfo
	// opLocks serialises initialize, remove and status checks per source, so a
	// connection is never cached for a source that a concurrent remove deleted.
	opLocks       keyedMutex
	health        map[models.SourceID]bool
	healthCtx     context.Context //nolint:containedctx // The provider owns cancellation of checks that outlive requests.
	cancelHealth  context.CancelFunc
	healthWG      sync.WaitGroup
	healthStarted bool
	closed        bool
}

// keyedMutex hands out one mutex per source ID. The key space is the set of
// source IDs, so the mutexes are never reclaimed.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[models.SourceID]chan struct{}
}

func (k *keyedMutex) lockContext(ctx context.Context, id models.SourceID) (func(), error) {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = make(map[models.SourceID]chan struct{})
	}
	lock, ok := k.locks[id]
	if !ok {
		lock = make(chan struct{}, 1)
		k.locks[id] = lock
	}
	k.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case lock <- struct{}{}:
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func NewProvider(log *slog.Logger) *Provider {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext
	transport.ResponseHeaderTimeout = responseHeaderTimeout

	healthCtx, cancelHealth := context.WithCancel(context.Background())
	return &Provider{
		client: &http.Client{
			Transport:     transport,
			CheckRedirect: sameHostRedirect,
		},
		log:          log.With("component", "loki_provider"),
		sources:      make(map[models.SourceID]models.LokiConnectionInfo),
		health:       make(map[models.SourceID]bool),
		healthCtx:    healthCtx,
		cancelHealth: cancelHealth,
	}
}

// sameHostRedirect follows a redirect only to the host and scheme of the
// original request, so a gateway cannot send the source's credentials and
// custom headers to another server.
func sameHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	first := via[0].URL
	if req.URL.Host != first.Host || req.URL.Scheme != first.Scheme {
		return fmt.Errorf("refusing a redirect from %s to another host", first.Host)
	}
	return nil
}

func (p *Provider) Type() models.SourceType {
	return models.SourceTypeLoki
}

// Describe lists only what works. Later units of RFC 0011 add the
// histogram, field values, live tail, log context and alerts.
func (p *Provider) Describe() datasource.Descriptor {
	return datasource.Descriptor{
		Capabilities: []datasource.Capability{
			datasource.CapabilitySchemaInspection,
			datasource.CapabilitySourceInspection,
		},
		QueryLanguages:        []models.QueryLanguage{models.QueryLanguageLogchefQL, models.QueryLanguageLogQL},
		SavedQueryEditorModes: []models.SavedQueryEditorMode{models.SavedQueryEditorModeBuilder, models.SavedQueryEditorModeNative},
		AlertEditorModes:      []models.AlertEditorMode{},
		// The generator must know which fields are stream labels.
		LogchefQLNeedsColumns: true,
	}
}

func (p *Provider) PrepareSource(ctx context.Context, req *models.CreateSourceRequest) (*models.Source, error) {
	if req == nil {
		return nil, fmt.Errorf("create source request is required")
	}
	if err := datasource.ValidateCommonSourceFields(req.Name, req.Description, req.TTLDays); err != nil {
		return nil, err
	}
	conn, err := connectionFromConfig(req.Connection, "connection.")
	if err != nil {
		return nil, err
	}

	metaTSField := strings.TrimSpace(req.MetaTSField)
	if metaTSField == "" {
		metaTSField = "_time"
	}
	if !datasource.IsValidIdentifier(metaTSField) {
		return nil, &datasource.ValidationError{Field: "meta_ts_field", Message: "meta timestamp field contains invalid characters"}
	}
	// detected_level is the structured metadata key that Loki adds when
	// discover_log_levels is on, which is its default.
	metaSeverityField := strings.TrimSpace(req.MetaSeverityField)
	if metaSeverityField == "" {
		metaSeverityField = "detected_level"
	}
	if !datasource.IsValidIdentifier(metaSeverityField) {
		return nil, &datasource.ValidationError{Field: "meta_severity_field", Message: "meta severity field contains invalid characters"}
	}

	if _, err := p.validateConnectionAccess(ctx, conn); err != nil {
		return nil, err
	}

	now := time.Now()
	source := &models.Source{
		Name:              req.Name,
		SourceType:        models.SourceTypeLoki,
		MetaTSField:       metaTSField,
		MetaSeverityField: metaSeverityField,
		ConnectionConfig:  req.Connection,
		Description:       req.Description,
		TTLDays:           req.TTLDays,
		Timestamps:        models.Timestamps{CreatedAt: now, UpdatedAt: now},
	}
	if err := source.SyncConnectionConfig(); err != nil {
		return nil, err
	}
	return source, nil
}

func (p *Provider) ValidateConnection(ctx context.Context, req *models.ValidateConnectionRequest) (*models.ConnectionValidationResult, error) {
	if req == nil {
		return nil, fmt.Errorf("validate connection request is required")
	}
	conn, err := connectionFromConfig(req.Connection, "")
	if err != nil {
		return nil, err
	}
	version, err := p.validateConnectionAccess(ctx, conn)
	if err != nil {
		return nil, err
	}
	message := fmt.Sprintf("Connection successful. Loki %s accepted the credentials, tenant and selector.", version)
	if old, ok := olderThanMinimum(version); ok && old {
		message += " Logchef needs Loki 3.0 or later: structured metadata and detected fields may be missing."
	}
	return &models.ConnectionValidationResult{Message: message}, nil
}

// olderThanMinimum reports whether a Loki version is below 3.0. ok is false
// for a version that is not a release number, such as a branch build.
func olderThanMinimum(version string) (old, ok bool) {
	major, _, found := strings.Cut(strings.TrimPrefix(version, "v"), ".")
	if !found {
		return false, false
	}
	n, err := strconv.Atoi(major)
	if err != nil {
		return false, false
	}
	return n < 3, true
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
		prev, err := source.LokiConnection()
		if err != nil {
			return nil, err
		}
		conn, err := decodeConnection(req.Connection)
		if err != nil {
			return nil, err
		}
		if err := rejectImmutableChanges(conn, prev); err != nil {
			return nil, err
		}
		// Responses redact secrets, so the edit form sends them blank to mean
		// "keep the stored value".
		mergeRedactedSecrets(&conn, prev)
		if err := validateConnectionConfig("connection.", conn); err != nil {
			return nil, err
		}
		if _, err := p.validateConnectionAccess(ctx, conn); err != nil {
			return nil, err
		}
		merged, err := json.Marshal(conn)
		if err != nil {
			return nil, fmt.Errorf("marshal loki connection config: %w", err)
		}
		source.ConnectionConfig = merged
		changed = true
	}

	if err := source.SyncConnectionConfig(); err != nil {
		return nil, err
	}
	return &datasource.SourceUpdateResult{Source: source, Changed: changed, Reinitialize: connectionChanged}, nil
}

// rejectImmutableChanges keeps the fields of the identity key fixed after
// creation. A different base URL that normalizes to the same one is the same
// endpoint and is allowed.
func rejectImmutableChanges(conn, prev models.LokiConnectionInfo) error {
	immutable := []struct{ field, next, prev string }{
		{"tenant_id", conn.TenantID, prev.TenantID},
		{"selector", conn.Selector, prev.Selector},
		{"principal", conn.Principal, prev.Principal},
	}
	for _, f := range immutable {
		if strings.TrimSpace(f.next) != strings.TrimSpace(f.prev) {
			return &datasource.ValidationError{Field: "connection." + f.field, Message: f.field + " cannot be changed after the source is created"}
		}
	}
	if models.BuildLokiIdentityKey(conn) != models.BuildLokiIdentityKey(prev) {
		return &datasource.ValidationError{Field: "connection.base_url", Message: "base_url cannot be changed after the source is created"}
	}
	return nil
}

// mergeRedactedSecrets fills each credential and header value that the
// request left blank from the stored connection. Only the credential of the
// new auth mode is kept, so switching modes cannot revive a stale secret.
func mergeRedactedSecrets(conn *models.LokiConnectionInfo, prev models.LokiConnectionInfo) {
	keep := func(current, stored string) string {
		if current != "" {
			return current
		}
		return stored
	}
	switch strings.ToLower(strings.TrimSpace(conn.Auth.Mode)) {
	case "basic":
		conn.Auth.Password = keep(conn.Auth.Password, prev.Auth.Password)
		conn.Auth.Token = ""
	case "bearer":
		conn.Auth.Token = keep(conn.Auth.Token, prev.Auth.Token)
		conn.Auth.Password = ""
	default:
		conn.Auth.Username = ""
		conn.Auth.Password = ""
		conn.Auth.Token = ""
	}
	for key, value := range conn.Headers {
		if strings.TrimSpace(value) == "" {
			conn.Headers[key] = prev.Headers[key]
		}
	}
}

func (p *Provider) InitializeSource(ctx context.Context, source *models.Source) error {
	if source == nil {
		return fmt.Errorf("source is required")
	}
	unlock, err := p.opLocks.lockContext(ctx, source.ID)
	if err != nil {
		return err
	}
	defer unlock()

	conn, err := source.LokiConnection()
	if err != nil {
		return err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("loki provider is closed")
	}
	p.sources[source.ID] = conn
	p.health[source.ID] = false
	p.mu.Unlock()

	err = p.checkHealth(ctx, source.ID, conn)
	p.recordHealth(source.ID, err == nil)
	return err
}

func (p *Provider) RemoveSource(sourceID models.SourceID) error {
	unlock, err := p.opLocks.lockContext(context.Background(), sourceID)
	if err != nil {
		return err
	}
	defer unlock()

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
	unlock, err := p.opLocks.lockContext(ctx, source.ID)
	if err != nil {
		return false
	}
	defer unlock()

	conn, err := p.connectionForSource(source)
	if err != nil {
		return false
	}
	connected := p.checkHealth(ctx, source.ID, conn) == nil
	p.recordHealth(source.ID, connected)
	return connected
}

// decodeConnection reads a connection payload without validating it.
func decodeConnection(raw json.RawMessage) (models.LokiConnectionInfo, error) {
	var conn models.LokiConnectionInfo
	if len(raw) == 0 {
		return conn, &datasource.ValidationError{Field: "connection", Message: "connection is required"}
	}
	if err := json.Unmarshal(raw, &conn); err != nil {
		return conn, &datasource.ValidationError{Field: "connection", Message: "invalid loki connection payload", Err: err}
	}
	return conn, nil
}

func connectionFromConfig(raw json.RawMessage, fieldPrefix string) (models.LokiConnectionInfo, error) {
	conn, err := decodeConnection(raw)
	if err != nil {
		return conn, err
	}
	return conn, validateConnectionConfig(fieldPrefix, conn)
}

func validateConnectionConfig(fieldPrefix string, conn models.LokiConnectionInfo) error {
	err := conn.Validate()
	if err == nil {
		return nil
	}
	if connErr, ok := errors.AsType[*models.LokiConnectionError](err); ok {
		return &datasource.ValidationError{Field: fieldPrefix + connErr.Field, Message: connErr.Message}
	}
	return &datasource.ValidationError{Field: fieldPrefix + "connection", Message: err.Error()}
}

func (p *Provider) connectionForSource(source *models.Source) (models.LokiConnectionInfo, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return models.LokiConnectionInfo{}, fmt.Errorf("loki provider is closed")
	}
	conn, ok := p.sources[source.ID]
	p.mu.RUnlock()
	if ok {
		return conn, nil
	}

	conn, err := source.LokiConnection()
	if err != nil {
		return models.LokiConnectionInfo{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return models.LokiConnectionInfo{}, fmt.Errorf("loki provider is closed")
	}
	p.sources[source.ID] = conn
	return conn, nil
}

// validateConnectionAccess proves the version, the credentials and tenant,
// and the selector with three calls under one budget. It returns the version.
func (p *Provider) validateConnectionAccess(ctx context.Context, conn models.LokiConnectionInfo) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultValidationTimeout)
	defer cancel()

	version, err := p.buildVersion(ctx, conn)
	if err != nil {
		return "", validationAccessError(err, "connection.base_url")
	}

	labels := lokiParams{"since": {validationProbeWindow}}
	if _, err := p.get(ctx, conn, "/loki/api/v1/labels", labels, metadataMaxBytes); err != nil {
		return "", validationAccessError(err, "connection.base_url")
	}

	probe := lokiParams{
		"query":     {conn.Selector},
		"limit":     {"1"},
		"since":     {validationProbeWindow},
		"direction": {"backward"},
	}
	if _, err := p.get(ctx, conn, "/loki/api/v1/query_range", probe, metadataMaxBytes); err != nil {
		return "", validationAccessError(err, "connection.selector")
	}
	return version, nil
}

// validationAccessError turns a failed probe into a validation error on the
// field an admin can fix. A Loki 400 names rejectedField.
func validationAccessError(err error, rejectedField string) error {
	if upstream, ok := errors.AsType[*datasource.UpstreamError](err); ok {
		if upstream.Kind == datasource.UpstreamAuth {
			return &datasource.ValidationError{Field: "connection.auth", Message: "Loki rejected the credentials or tenant", Err: err}
		}
		return &datasource.ValidationError{Field: "connection.base_url", Message: "Failed to reach Loki: " + upstream.Message, Err: err}
	}
	if validation, ok := errors.AsType[*datasource.ValidationError](err); ok {
		return &datasource.ValidationError{Field: rejectedField, Message: validation.Message, Err: err}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &datasource.ValidationError{Field: "connection.base_url", Message: "Loki did not answer within 8 seconds", Err: err}
	}
	return &datasource.ValidationError{Field: "connection.base_url", Message: "Failed to reach Loki", Err: err}
}

// buildVersion reads the Loki version from the build information endpoint.
func (p *Provider) buildVersion(ctx context.Context, conn models.LokiConnectionInfo) (string, error) {
	body, err := p.get(ctx, conn, "/loki/api/v1/status/buildinfo", nil, metadataMaxBytes)
	if err != nil {
		return "", err
	}
	var info struct {
		Version string `json:"version"`
	}
	if err := decodeBody(body, &info); err != nil {
		return "", err
	}
	return info.Version, nil
}

// applyHeaders sets the auth, tenant and custom headers of a source. An unset
// auth mode sends no credential.
func applyHeaders(req *http.Request, conn models.LokiConnectionInfo) {
	for key, value := range conn.Headers {
		if strings.TrimSpace(key) == "" || isReservedHeader(key) {
			continue
		}
		req.Header.Set(key, value)
	}
	switch strings.ToLower(strings.TrimSpace(conn.Auth.Mode)) {
	case "basic":
		req.SetBasicAuth(conn.Auth.Username, conn.Auth.Password)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+conn.Auth.Token)
	}
	if tenant := strings.TrimSpace(conn.TenantID); tenant != "" {
		req.Header.Set("X-Scope-OrgID", tenant)
	}
}

// isReservedHeader reports a custom header that would override the auth or
// tenant. Validation rejects them at save time; this skips them on the wire.
func isReservedHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "x-scope-orgid":
		return true
	}
	return false
}
