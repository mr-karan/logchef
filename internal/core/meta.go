package core

import (
	"time"

	"github.com/mr-karan/logchef/internal/config"
)

// DashboardCacheMeta advertises the server's dashboard result-cache policy so
// the frontend can resolve the effective per-dashboard TTL the SAME way the
// server does (see internal/server/dashcache.go). The client must snap relative
// ranges to this TTL bucket before pre-translating panel queries, so it needs
// the policy up front. Durations are whole seconds to avoid sub-second rounding
// becoming a source of client/server disagreement.
type DashboardCacheMeta struct {
	Enabled           bool `json:"enabled"`
	DefaultTTLSeconds int  `json:"default_ttl_seconds"`
	MaxTTLSeconds     int  `json:"max_ttl_seconds"`
}

// DemoLoginCredentials contains local credentials that a deliberately
// configured public demo may advertise on its login page.
type DemoLoginCredentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// MetaResponse represents the server metadata response
type MetaResponse struct {
	Version              string                `json:"version"`
	HTTPServerTimeout    string                `json:"http_server_timeout"`
	OIDCIssuer           string                `json:"oidc_issuer,omitempty"`
	CLIClientID          string                `json:"cli_client_id,omitempty"`
	MaxQueryLimit        int                   `json:"max_query_limit"`
	MaxQueryTimeoutSecs  int                   `json:"max_query_timeout_seconds"`
	DefaultPreviewLimit  int                   `json:"default_preview_limit"`
	MaxPreviewLimit      int                   `json:"max_preview_limit"`
	MaxExportRows        int                   `json:"max_export_rows"`
	AlertsEnabled        bool                  `json:"alerts_enabled"`
	LocalAuthEnabled     bool                  `json:"local_auth_enabled"`
	OIDCEnabled          bool                  `json:"oidc_enabled"`
	DemoReadOnly         bool                  `json:"demo_read_only"`
	DemoLoginCredentials *DemoLoginCredentials `json:"demo_login_credentials,omitempty"`
	DashboardCache       DashboardCacheMeta    `json:"dashboard_cache"`
}

// BuildMeta builds the public server metadata. oidcIssuer is empty when OIDC
// login is not configured.
func BuildMeta(cfg *config.Config, version, oidcIssuer string, oidcEnabled bool) MetaResponse {
	meta := MetaResponse{
		Version:             version,
		HTTPServerTimeout:   cfg.Server.HTTPServerTimeout.String(),
		MaxQueryLimit:       cfg.Query.MaxPreviewLimit,
		MaxQueryTimeoutSecs: cfg.Query.MaxTimeoutSeconds,
		DefaultPreviewLimit: cfg.Query.DefaultPreviewLimit,
		MaxPreviewLimit:     cfg.Query.MaxPreviewLimit,
		MaxExportRows:       cfg.Export.MaxRows,
		AlertsEnabled:       cfg.Alerts.Enabled,
		LocalAuthEnabled:    cfg.Auth.Local.Enabled,
		OIDCEnabled:         oidcEnabled,
		DemoReadOnly:        cfg.Demo.ReadOnly,
		DashboardCache: DashboardCacheMeta{
			Enabled:           cfg.DashboardCache.Enabled,
			DefaultTTLSeconds: int(cfg.DashboardCache.DefaultTTL / time.Second),
			MaxTTLSeconds:     int(cfg.DashboardCache.MaxTTL / time.Second),
		},
	}

	if oidcEnabled {
		meta.OIDCIssuer = oidcIssuer
		meta.CLIClientID = cfg.OIDC.CLIClientID
	}

	localAuth := cfg.Auth.Local
	if cfg.Demo.ReadOnly && cfg.Demo.ShowLoginCredentials && localAuth.Enabled &&
		localAuth.AdminEmail != "" && localAuth.AdminPassword != "" {
		meta.DemoLoginCredentials = &DemoLoginCredentials{
			Email:    localAuth.AdminEmail,
			Password: localAuth.AdminPassword,
		}
	}
	return meta
}
