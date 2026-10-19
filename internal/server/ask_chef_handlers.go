package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/askchef"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/mcp"
	"github.com/mr-karan/logchef/pkg/models"
)

// askChefTurnsPath streams one Ask Chef turn over Server-Sent Events.
const askChefTurnsPath = "/ask-chef/turns"

// newAskChef returns the Ask Chef agent, or nil when Ask Chef is disabled.
// Its tools share the HTTP API's admission caps.
func (s *Server) newAskChef(chat askchef.ChatModel) *askchef.Agent {
	if chat == nil || !s.config.AI.AskChefEnabled() {
		return nil
	}
	tools := mcp.NewToolServer(mcp.Deps{
		DB:          s.sqlite,
		Datasources: s.datasources,
		Config:      s.config,
		Version:     s.version,
		Log:         s.log.With("component", "ask_chef_tools"),
		Admit:       s.admitMCPQuery,
	})
	return askchef.New(askchef.Opts{
		Tools: tools,
		Chat:  chat,
		Model: s.config.AI.AskChefModel,
		Log:   s.log.With("component", "ask_chef"),
	})
}

// handleAskChefTurn answers one question with Ask Chef. It validates the
// request, checks team and source access with the user's assistant principal
// and allows one turn per user, all before the stream starts, so those
// failures are normal JSON errors. The stream carries tool_start, tool_end,
// text, done and error events.
//
// POST /api/v1/ask-chef/turns
func (s *Server) handleAskChefTurn(c fiber.Ctx) error {
	if s.askChef == nil {
		return SendErrorWithType(c, fiber.StatusNotFound, "Ask Chef is not enabled", models.NotFoundErrorType)
	}
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		return SendErrorWithType(c, fiber.StatusUnauthorized, "User context not found", models.AuthenticationErrorType)
	}
	// A cross-origin JSON POST needs a CORS preflight, so a form on another
	// site cannot start a turn with the user's cookie.
	if mediaType, _, _ := strings.Cut(c.Get(fiber.HeaderContentType), ";"); !strings.EqualFold(strings.TrimSpace(mediaType), fiber.MIMEApplicationJSON) {
		return SendErrorWithType(c, fiber.StatusUnsupportedMediaType, "Content-Type must be application/json", models.ValidationErrorType)
	}
	req, err := askchef.ParseRequest(c.Body())
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	ctx := c.Context()
	p := access.AssistantPrincipal(user)
	_, err = access.AuthorizeTeamSource(ctx, s.sqlite, p, req.TeamID, req.SourceID, models.TokenScopeLogsRead)
	switch {
	case err == nil:
	case errors.Is(err, access.ErrNotTeamMember):
		return SendErrorWithType(c, fiber.StatusForbidden, "Team membership required", models.AuthorizationErrorType)
	case errors.Is(err, access.ErrSourceNotInTeam):
		return SendErrorWithType(c, fiber.StatusForbidden, "Team does not have access to this source", models.AuthorizationErrorType)
	default:
		s.log.Error("error checking ask chef access", "error", err, "user_id", user.ID, "team_id", req.TeamID, "source_id", req.SourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify team source access", models.GeneralErrorType)
	}

	var alert *askchef.AlertContext
	if ref := req.View.Alert; ref != nil {
		alert, err = askchef.LoadAlertContext(ctx, s.sqlite, s.log, p, req.SourceID, *ref, s.config.Alerts.HistoryLimit)
		switch {
		case err == nil:
		case errors.Is(err, core.ErrAlertNotFound):
			return SendErrorWithType(c, fiber.StatusNotFound, "Alert not found", models.NotFoundErrorType)
		case errors.Is(err, askchef.ErrAlertEntryNotFound):
			return SendErrorWithType(c, fiber.StatusNotFound, "Alert history entry not found", models.NotFoundErrorType)
		case errors.Is(err, askchef.ErrAlertNotInSource):
			return SendErrorWithType(c, fiber.StatusBadRequest, "Alert does not belong to this source", models.ValidationErrorType)
		default:
			s.log.Error("error loading ask chef alert context", "error", err, "user_id", user.ID, "alert_id", ref.AlertID)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load the alert", models.GeneralErrorType)
		}
	}

	if _, busy := s.askChefTurns.LoadOrStore(user.ID, struct{}{}); busy {
		s.log.Info("ask chef turn refused: another turn is running", "user_id", user.ID)
		return SendErrorWithType(c, fiber.StatusConflict, "Ask Chef is already answering another question", models.ConflictErrorType)
	}

	c.Status(fiber.StatusOK)
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")
	c.RequestCtx().Response.ImmediateHeaderFlush = true

	// The fiber ctx is not valid inside the stream writer.
	agent := s.askChef
	turn := askchef.Turn{Principal: p, Request: req, Alert: alert}
	c.RequestCtx().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer s.askChefTurns.Delete(user.ID)
		agent.Run(ctx, turn, func(ev askchef.Event) bool { return writeSSEEvent(w, ev) })
	})
	return nil
}

// writeSSEEvent writes and flushes one event. It returns false when the
// client is gone.
func writeSSEEvent(w *bufio.Writer, ev askchef.Event) bool {
	data, err := json.Marshal(ev)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name(), data); err != nil {
		return false
	}
	return w.Flush() == nil
}
