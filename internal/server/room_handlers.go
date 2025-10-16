package server

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

func (s *Server) handleListRooms(c *fiber.Ctx) error {
	teamID, err := s.parseTeamIDParam(c)
	if err != nil {
		return err
	}

	rooms, err := core.ListRooms(c.Context(), s.sqlite, teamID)
	if err != nil {
		s.log.Error("failed to list rooms", "team_id", teamID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list rooms", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, rooms)
}

func (s *Server) handleCreateRoom(c *fiber.Ctx) error {
	teamID, err := s.parseTeamIDParam(c)
	if err != nil {
		return err
	}

	var req models.CreateRoomRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	room, err := core.CreateRoom(c.Context(), s.sqlite, s.log, teamID, &req)
	if err != nil {
		if errors.Is(err, core.ErrInvalidRoomConfiguration) {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
		s.log.Error("failed to create room", "team_id", teamID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create room", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, room)
}

func (s *Server) handleUpdateRoom(c *fiber.Ctx) error {
	teamID, err := s.parseTeamIDParam(c)
	if err != nil {
		return err
	}
	roomID, err := s.parseRoomIDParam(c)
	if err != nil {
		return err
	}

	var req models.UpdateRoomRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	room, err := core.UpdateRoom(c.Context(), s.sqlite, s.log, teamID, roomID, &req)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrInvalidRoomConfiguration):
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		case errors.Is(err, core.ErrRoomNotFound):
			return SendErrorWithType(c, fiber.StatusNotFound, "Room not found", models.NotFoundErrorType)
		default:
			s.log.Error("failed to update room", "room_id", roomID, "error", err)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to update room", models.GeneralErrorType)
		}
	}
	return SendSuccess(c, fiber.StatusOK, room)
}

func (s *Server) handleDeleteRoom(c *fiber.Ctx) error {
	teamID, err := s.parseTeamIDParam(c)
	if err != nil {
		return err
	}
	roomID, err := s.parseRoomIDParam(c)
	if err != nil {
		return err
	}

	if err := core.DeleteRoom(c.Context(), s.sqlite, teamID, roomID); err != nil {
		if errors.Is(err, core.ErrRoomNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Room not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to delete room", "room_id", roomID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete room", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Room deleted"})
}

func (s *Server) handleListRoomMembers(c *fiber.Ctx) error {
	teamID, err := s.parseTeamIDParam(c)
	if err != nil {
		return err
	}
	roomID, err := s.parseRoomIDParam(c)
	if err != nil {
		return err
	}

	members, err := core.ListRoomMembers(c.Context(), s.sqlite, teamID, roomID)
	if err != nil {
		if errors.Is(err, core.ErrRoomNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Room not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to list room members", "room_id", roomID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list room members", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, members)
}

func (s *Server) handleAddRoomMember(c *fiber.Ctx) error {
	teamID, err := s.parseTeamIDParam(c)
	if err != nil {
		return err
	}
	roomID, err := s.parseRoomIDParam(c)
	if err != nil {
		return err
	}

	var req models.AddRoomMemberRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if req.UserID == 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest, "user_id is required", models.ValidationErrorType)
	}

	if err := core.AddRoomMember(c.Context(), s.sqlite, teamID, roomID, &req); err != nil {
		if errors.Is(err, core.ErrRoomNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Room not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to add room member", "room_id", roomID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to add room member", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Member added"})
}

func (s *Server) handleRemoveRoomMember(c *fiber.Ctx) error {
	teamID, err := s.parseTeamIDParam(c)
	if err != nil {
		return err
	}
	roomID, err := s.parseRoomIDParam(c)
	if err != nil {
		return err
	}
	userIDStr := c.Params("userID")
	userID, err := core.ParseUserID(userIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid user ID", models.ValidationErrorType)
	}

	if err := core.RemoveRoomMember(c.Context(), s.sqlite, teamID, roomID, userID); err != nil {
		if errors.Is(err, core.ErrRoomNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Room or member not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to remove room member", "room_id", roomID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to remove room member", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Member removed"})
}

func (s *Server) handleListRoomChannels(c *fiber.Ctx) error {
	teamID, err := s.parseTeamIDParam(c)
	if err != nil {
		return err
	}
	roomID, err := s.parseRoomIDParam(c)
	if err != nil {
		return err
	}

	channels, err := core.ListRoomChannels(c.Context(), s.sqlite, teamID, roomID)
	if err != nil {
		if errors.Is(err, core.ErrRoomNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Room not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to list room channels", "room_id", roomID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list room channels", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, channels)
}

func (s *Server) handleCreateRoomChannel(c *fiber.Ctx) error {
	teamID, err := s.parseTeamIDParam(c)
	if err != nil {
		return err
	}
	roomID, err := s.parseRoomIDParam(c)
	if err != nil {
		return err
	}

	var req models.CreateRoomChannelRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	channel, err := core.CreateRoomChannel(c.Context(), s.sqlite, teamID, roomID, &req)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrInvalidRoomConfiguration):
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		case errors.Is(err, core.ErrRoomNotFound):
			return SendErrorWithType(c, fiber.StatusNotFound, "Room not found", models.NotFoundErrorType)
		default:
			s.log.Error("failed to create room channel", "room_id", roomID, "error", err)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create room channel", models.GeneralErrorType)
		}
	}
	return SendSuccess(c, fiber.StatusCreated, channel)
}

func (s *Server) handleUpdateRoomChannel(c *fiber.Ctx) error {
	teamID, err := s.parseTeamIDParam(c)
	if err != nil {
		return err
	}
	roomID, err := s.parseRoomIDParam(c)
	if err != nil {
		return err
	}
	channelID, err := s.parseChannelIDParam(c)
	if err != nil {
		return err
	}

	var req models.UpdateRoomChannelRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	channel, err := core.UpdateRoomChannel(c.Context(), s.sqlite, teamID, roomID, channelID, &req)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrInvalidRoomConfiguration):
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		case errors.Is(err, core.ErrRoomNotFound):
			return SendErrorWithType(c, fiber.StatusNotFound, "Room or channel not found", models.NotFoundErrorType)
		default:
			s.log.Error("failed to update room channel", "channel_id", channelID, "error", err)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to update room channel", models.GeneralErrorType)
		}
	}
	return SendSuccess(c, fiber.StatusOK, channel)
}

func (s *Server) handleDeleteRoomChannel(c *fiber.Ctx) error {
	teamID, err := s.parseTeamIDParam(c)
	if err != nil {
		return err
	}
	roomID, err := s.parseRoomIDParam(c)
	if err != nil {
		return err
	}
	channelID, err := s.parseChannelIDParam(c)
	if err != nil {
		return err
	}

	if err := core.DeleteRoomChannel(c.Context(), s.sqlite, teamID, roomID, channelID); err != nil {
		if errors.Is(err, core.ErrRoomNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Room or channel not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to delete room channel", "channel_id", channelID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete room channel", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Channel deleted"})
}

func (s *Server) parseTeamIDParam(c *fiber.Ctx) (models.TeamID, error) {
	teamIDStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return 0, SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team_id parameter", models.ValidationErrorType)
	}
	return teamID, nil
}

func (s *Server) parseRoomIDParam(c *fiber.Ctx) (models.RoomID, error) {
	roomIDStr := c.Params("roomID")
	if roomIDStr == "" {
		return 0, SendErrorWithType(c, fiber.StatusBadRequest, "roomID is required", models.ValidationErrorType)
	}
	id, err := strconv.ParseInt(roomIDStr, 10, 64)
	if err != nil || id <= 0 {
		return 0, SendErrorWithType(c, fiber.StatusBadRequest, "Invalid room ID", models.ValidationErrorType)
	}
	return models.RoomID(id), nil
}

func (s *Server) parseChannelIDParam(c *fiber.Ctx) (int64, error) {
	channelIDStr := c.Params("channelID")
	if channelIDStr == "" {
		return 0, SendErrorWithType(c, fiber.StatusBadRequest, "channelID is required", models.ValidationErrorType)
	}
	id, err := strconv.ParseInt(channelIDStr, 10, 64)
	if err != nil || id <= 0 {
		return 0, SendErrorWithType(c, fiber.StatusBadRequest, "Invalid channel ID", models.ValidationErrorType)
	}
	return id, nil
}
