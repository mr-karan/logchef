package server

import (
	"cmp"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
)

// --- Meta Handlers ---

// handleGetMeta returns server metadata including version and configuration
// URL: GET /api/v1/meta
// Public endpoint - no authentication required
// @Summary Get server metadata
// @Description Returns server metadata including version and configuration information
// @Tags meta
// @Accept json
// @Produce json
// @Success 200 {object} core.MetaResponse "Server metadata"
// @Router /meta [get]
func (s *Server) handleGetMeta(c fiber.Ctx) error {
	// Runtime metadata can intentionally include public demo credentials. Never
	// let a browser or intermediary retain them after opt-out or rotation.
	c.Set(fiber.HeaderCacheControl, "no-store, private")

	var oidcIssuer string
	if s.oidcProvider != nil {
		oidcIssuer = s.oidcProvider.GetIssuer()
	}
	meta := metaResponse{
		MetaResponse: core.BuildMeta(s.config, s.version, oidcIssuer, s.oidcProvider != nil),
		UIURL:        uiURL(&s.config.Server),
	}
	if s.oauth != nil {
		meta.OAuthIssuer = s.oauth.Issuer()
	}
	return SendSuccess(c, fiber.StatusOK, meta)
}

// metaResponse adds the HTTP-only fields to the shared metadata. A CLI that
// sees oauth_issuer logs in through Logchef OAuth, and builds browser links
// (for example the log explorer) on ui_url instead of the API URL it uses.
type metaResponse struct {
	core.MetaResponse
	OAuthIssuer string `json:"oauth_issuer,omitempty"`
	UIURL       string `json:"ui_url,omitempty"`
}

// uiURL is the base URL of the web UI: server.frontend_url when set (it may
// carry a base path), else server.browser_url, else server.public_url. It is
// empty when none is set.
func uiURL(cfg *config.ServerConfig) string {
	return cmp.Or(strings.TrimSuffix(cfg.FrontendURL, "/"), cfg.BrowserURL, cfg.PublicURL)
}
