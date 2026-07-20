package server

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

// --- Meta Handlers ---

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

// MetaResponse represents the server metadata response
type MetaResponse struct {
	Version             string             `json:"version"`
	HTTPServerTimeout   string             `json:"http_server_timeout"`
	OIDCIssuer          string             `json:"oidc_issuer,omitempty"`
	CLIClientID         string             `json:"cli_client_id,omitempty"`
	MaxQueryLimit       int                `json:"max_query_limit"`
	MaxQueryTimeoutSecs int                `json:"max_query_timeout_seconds"`
	DefaultPreviewLimit int                `json:"default_preview_limit"`
	MaxPreviewLimit     int                `json:"max_preview_limit"`
	MaxExportRows       int                `json:"max_export_rows"`
	AlertsEnabled       bool               `json:"alerts_enabled"`
	LocalAuthEnabled    bool               `json:"local_auth_enabled"`
	OIDCEnabled         bool               `json:"oidc_enabled"`
	DashboardCache      DashboardCacheMeta `json:"dashboard_cache"`
}

// handleGetMeta returns server metadata including version and configuration
// URL: GET /api/v1/meta
// Public endpoint - no authentication required
// @Summary Get server metadata
// @Description Returns server metadata including version and configuration information
// @Tags meta
// @Accept json
// @Produce json
// @Success 200 {object} MetaResponse "Server metadata"
// @Router /meta [get]
func (s *Server) handleGetMeta(c *fiber.Ctx) error {
	meta := MetaResponse{
		Version:             s.version,
		HTTPServerTimeout:   s.config.Server.HTTPServerTimeout.String(),
		MaxQueryLimit:       s.config.Query.MaxPreviewLimit,
		MaxQueryTimeoutSecs: s.config.Query.MaxTimeoutSeconds,
		DefaultPreviewLimit: s.config.Query.DefaultPreviewLimit,
		MaxPreviewLimit:     s.config.Query.MaxPreviewLimit,
		MaxExportRows:       s.config.Export.MaxRows,
		AlertsEnabled:       s.config.Alerts.Enabled,
		LocalAuthEnabled:    s.config.Auth.Local.Enabled,
		OIDCEnabled:         s.oidcProvider != nil,
		DashboardCache: DashboardCacheMeta{
			Enabled:           s.config.DashboardCache.Enabled,
			DefaultTTLSeconds: int(s.config.DashboardCache.DefaultTTL / time.Second),
			MaxTTLSeconds:     int(s.config.DashboardCache.MaxTTL / time.Second),
		},
	}

	if s.oidcProvider != nil {
		meta.OIDCIssuer = s.oidcProvider.GetIssuer()
		meta.CLIClientID = s.config.OIDC.CLIClientID
	}

	return SendSuccess(c, fiber.StatusOK, meta)
}
