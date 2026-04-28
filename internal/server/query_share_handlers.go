package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

func (s *Server) handleCreateQueryShare(c *fiber.Ctx) error {
	teamID, err := core.ParseTeamID(c.Params("teamID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team ID format", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(c.Params("sourceID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}
	user := c.Locals("user").(*models.User)
	if user == nil {
		return SendErrorWithType(c, fiber.StatusUnauthorized, "User context not found", models.AuthenticationErrorType)
	}

	var req models.CreateQueryShareRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	payloadBytes := []byte(req.Payload)
	if len(payloadBytes) == 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest, "payload is required", models.ValidationErrorType)
	}
	if len(payloadBytes) > s.config.Shares.MaxQueryTextBytes {
		return SendErrorWithType(c, fiber.StatusBadRequest,
			fmt.Sprintf("Shared query payload cannot exceed %d bytes", s.config.Shares.MaxQueryTextBytes),
			models.ValidationErrorType)
	}
	if !json.Valid(payloadBytes) {
		return SendErrorWithType(c, fiber.StatusBadRequest, "payload must be valid JSON", models.ValidationErrorType)
	}

	var payload models.QuerySharePayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid share payload", models.ValidationErrorType)
	}
	if payload.Mode != "logchefql" && payload.Mode != "sql" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "payload.mode must be logchefql or sql", models.ValidationErrorType)
	}
	if payload.Mode == "sql" && strings.TrimSpace(payload.Query) == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "payload.query is required", models.ValidationErrorType)
	}
	if len(payload.Query) > s.config.Shares.MaxQueryTextBytes {
		return SendErrorWithType(c, fiber.StatusBadRequest,
			fmt.Sprintf("Shared query text cannot exceed %d bytes", s.config.Shares.MaxQueryTextBytes),
			models.ValidationErrorType)
	}
	if payload.Limit < 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest, "payload.limit cannot be negative", models.ValidationErrorType)
	}

	ttl := s.config.Shares.DefaultTTL
	if req.ExpiresInSeconds > 0 {
		requested := time.Duration(req.ExpiresInSeconds) * time.Second
		if requested < ttl {
			ttl = requested
		}
	}

	token, err := newShareToken()
	if err != nil {
		s.log.Error("failed to generate query share token", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create share link", models.GeneralErrorType)
	}

	now := time.Now().UTC()
	share := &models.QueryShare{
		Token:     token,
		TeamID:    teamID,
		SourceID:  sourceID,
		CreatedBy: user.ID,
		Payload:   append([]byte(nil), payloadBytes...),
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	if err := s.sqlite.CreateQueryShare(c.Context(), share); err != nil {
		s.log.Error("failed to create query share", "error", err, "team_id", teamID, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create share link", models.GeneralErrorType)
	}

	return SendSuccess(c, fiber.StatusCreated, queryShareResponse(share, buildQueryShareURL(c, s.config.Server.FrontendURL, token)))
}

func (s *Server) handleGetQueryShare(c *fiber.Ctx) error {
	token := strings.TrimSpace(c.Params("token"))
	if token == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Share token is required", models.ValidationErrorType)
	}
	user := c.Locals("user").(*models.User)
	if user == nil {
		return SendErrorWithType(c, fiber.StatusUnauthorized, "User context not found", models.AuthenticationErrorType)
	}

	share, err := s.sqlite.GetQueryShare(c.Context(), token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Share link not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get query share", "error", err, "token", token)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get share link", models.GeneralErrorType)
	}
	if time.Now().UTC().After(share.ExpiresAt) {
		_ = s.sqlite.DeleteQueryShare(c.Context(), token)
		return SendErrorWithType(c, fiber.StatusGone, "Share link has expired", models.NotFoundErrorType)
	}

	if user.Role != models.UserRoleAdmin {
		hasAccess, err := core.UserHasAccessToTeamSource(c.Context(), s.sqlite, s.log, user.ID, share.TeamID, share.SourceID)
		if err != nil {
			s.log.Error("failed to check query share access", "error", err, "token", token, "user_id", user.ID)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to check share access", models.GeneralErrorType)
		}
		if !hasAccess {
			return SendErrorWithType(c, fiber.StatusForbidden, "You do not have access to this shared query", models.AuthorizationErrorType)
		}
	}

	if err := s.sqlite.TouchQueryShare(c.Context(), token, time.Now().UTC()); err != nil {
		s.log.Warn("failed to touch query share", "error", err, "token", token)
	}

	return SendSuccess(c, fiber.StatusOK, queryShareResponse(share, buildQueryShareURL(c, s.config.Server.FrontendURL, token)))
}

func (s *Server) handleDeleteQueryShare(c *fiber.Ctx) error {
	token := strings.TrimSpace(c.Params("token"))
	if token == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Share token is required", models.ValidationErrorType)
	}
	user := c.Locals("user").(*models.User)
	if user == nil {
		return SendErrorWithType(c, fiber.StatusUnauthorized, "User context not found", models.AuthenticationErrorType)
	}

	share, err := s.sqlite.GetQueryShare(c.Context(), token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Share link not found", models.NotFoundErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get share link", models.GeneralErrorType)
	}
	if user.Role != models.UserRoleAdmin && share.CreatedBy != user.ID {
		return SendErrorWithType(c, fiber.StatusForbidden, "Only the creator or an admin can delete this share link", models.AuthorizationErrorType)
	}
	if err := s.sqlite.DeleteQueryShare(c.Context(), token); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Share link not found", models.NotFoundErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete share link", models.GeneralErrorType)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func newShareToken() (string, error) {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func queryShareResponse(share *models.QueryShare, shareURL string) models.QueryShareResponse {
	return models.QueryShareResponse{
		Token:     share.Token,
		ShareURL:  shareURL,
		TeamID:    share.TeamID,
		SourceID:  share.SourceID,
		Payload:   share.Payload,
		ExpiresAt: share.ExpiresAt,
		CreatedAt: share.CreatedAt,
		CreatedBy: share.CreatedBy,
	}
}

func buildQueryShareURL(c *fiber.Ctx, frontendURL, token string) string {
	base := strings.TrimRight(frontendURL, "/")
	if base == "" {
		base = strings.TrimRight(c.BaseURL(), "/")
	}
	if base == "" {
		return "/logs/explore?share=" + url.QueryEscape(token)
	}
	return base + "/logs/explore?share=" + url.QueryEscape(token)
}
