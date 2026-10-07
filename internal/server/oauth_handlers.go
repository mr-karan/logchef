package server

import (
	"encoding/json"
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/oauth"
	"github.com/mr-karan/logchef/pkg/models"
)

// handleOAuthMetadata serves the RFC 8414 authorization server metadata.
func (s *Server) handleOAuthMetadata(c fiber.Ctx) error {
	return sendPublicJSON(c, s.oauth.Metadata())
}

// handleMCPResourceMetadata serves the RFC 9728 metadata for /mcp.
func (s *Server) handleMCPResourceMetadata(c fiber.Ctx) error {
	return sendPublicJSON(c, s.oauth.MCPResourceMetadata())
}

// sendPublicJSON writes a bare JSON document (no API envelope) that any origin
// may read, as browser-based OAuth clients fetch metadata cross-origin.
func sendPublicJSON(c fiber.Ctx, body any) error {
	c.Set(fiber.HeaderAccessControlAllowOrigin, "*")
	c.Set(fiber.HeaderCacheControl, "public, max-age=300")
	return c.JSON(body)
}

// handleGetOAuthRequest returns a pending authorization request for the
// consent screen. Session only.
func (s *Server) handleGetOAuthRequest(c fiber.Ctx) error {
	req, err := s.oauth.ConsentRequest(c.RequestCtx(), models.OAuthAuthRequestID(c.Params("requestID")))
	if errors.Is(err, oauth.ErrRequestNotFound) || errors.Is(err, oauth.ErrRequestDecided) {
		return SendErrorWithType(c, fiber.StatusNotFound, oauth.ErrRequestNotFound.Error(), models.NotFoundErrorType)
	}
	if err != nil {
		s.log.Error("failed to load OAuth authorization request", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Failed to load authorization request")
	}
	user := c.Locals("user").(*models.User)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"id":             req.ID,
		"client":         req.Client,
		"instance":       req.Instance,
		"resource":       req.Resource,
		"resource_kind":  req.ResourceKind,
		"scopes":         req.Scopes,
		"offline_access": req.OfflineAccess,
		"redirect_uri":   req.RedirectURI,
		"expires_at":     req.ExpiresAt,
		"user":           fiber.Map{"id": user.ID, "email": user.Email, "full_name": user.FullName},
	})
}

type oauthDecisionRequest struct {
	Approve *bool `json:"approve"`
}

// handleOAuthDecision records the session user's approval or denial and
// returns the client redirect. Session only, same origin, JSON body.
func (s *Server) handleOAuthDecision(c fiber.Ctx) error {
	var body oauthDecisionRequest
	if err := json.Unmarshal(c.Body(), &body); err != nil || body.Approve == nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, `Body must be {"approve": true} or {"approve": false}`, models.ValidationErrorType)
	}
	user := c.Locals("user").(*models.User)
	redirectURL, err := s.oauth.Decide(c.RequestCtx(), models.OAuthAuthRequestID(c.Params("requestID")), user, *body.Approve)
	switch {
	case errors.Is(err, oauth.ErrRequestNotFound):
		return SendErrorWithType(c, fiber.StatusNotFound, err.Error(), models.NotFoundErrorType)
	case errors.Is(err, oauth.ErrRequestDecided):
		return SendErrorWithType(c, fiber.StatusConflict, err.Error(), models.ConflictErrorType)
	case err != nil:
		s.log.Error("failed to record OAuth decision", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to record decision")
	}
	s.log.Info("OAuth authorization decided", "user_id", user.ID, "approved", *body.Approve)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"redirect_url": redirectURL})
}

// handleListConnectedApps lists the session user's active OAuth grants.
func (s *Server) handleListConnectedApps(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	apps, err := s.oauth.ConnectedApps(c.RequestCtx(), user.ID)
	if err != nil {
		s.log.Error("failed to list connected apps", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to list connected apps")
	}
	return SendSuccess(c, fiber.StatusOK, apps)
}

// handleRevokeConnectedApp revokes one of the session user's OAuth grants.
func (s *Server) handleRevokeConnectedApp(c fiber.Ctx) error {
	id, err := parsePositiveIntParam(c, "grantID")
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	user := c.Locals("user").(*models.User)
	err = s.oauth.RevokeConnectedApp(c.RequestCtx(), user.ID, models.OAuthGrantID(id))
	if errors.Is(err, oauth.ErrGrantNotFound) {
		return SendErrorWithType(c, fiber.StatusNotFound, err.Error(), models.NotFoundErrorType)
	}
	if err != nil {
		s.log.Error("failed to revoke connected app", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to revoke connected app")
	}
	s.log.Info("connected app revoked", "user_id", user.ID, "grant_id", id)
	return c.SendStatus(fiber.StatusNoContent)
}
