package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrRoomNotFound indicates a room could not be located.
	ErrRoomNotFound = errors.New("room not found")
	// ErrInvalidRoomConfiguration indicates validation failed for a room payload.
	ErrInvalidRoomConfiguration = errors.New("invalid room configuration")
)

func validateRoomName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

func validateRoomChannel(channel *models.RoomChannel) error {
	if channel == nil {
		return fmt.Errorf("channel payload required")
	}
	switch channel.Type {
	case models.RoomChannelSlack, models.RoomChannelWebhook:
		url, _ := channel.Config["url"].(string)
		if strings.TrimSpace(url) == "" {
			return fmt.Errorf("channel url is required")
		}
	default:
		return fmt.Errorf("unsupported channel type %q", channel.Type)
	}
	return nil
}

// ListRooms returns summaries for all rooms owned by a team.
func ListRooms(ctx context.Context, db *sqlite.DB, teamID models.TeamID) ([]models.RoomSummary, error) {
	roomRecords, err := db.ListRoomsByTeam(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("failed to list rooms: %w", err)
	}
	summaries := make([]models.RoomSummary, 0, len(roomRecords))
	for _, room := range roomRecords {
		summary, err := db.GetRoomSummary(ctx, room.ID)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, *summary)
	}
	return summaries, nil
}

// CreateRoom registers a new room for the team.
func CreateRoom(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, req *models.CreateRoomRequest) (*models.Room, error) {
	if req == nil {
		return nil, ErrInvalidRoomConfiguration
	}
	if err := validateRoomName(req.Name); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRoomConfiguration, err)
	}

	room := &models.Room{
		TeamID:      teamID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
	}
	if err := db.CreateRoom(ctx, room); err != nil {
		log.Error("failed to create room", "team_id", teamID, "error", err)
		return nil, fmt.Errorf("failed to create room: %w", err)
	}
	return room, nil
}

// UpdateRoom modifies an existing room definition.
func UpdateRoom(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, roomID models.RoomID, req *models.UpdateRoomRequest) (*models.Room, error) {
	if req == nil {
		return nil, ErrInvalidRoomConfiguration
	}
	room, err := db.GetRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("failed to load room: %w", err)
	}
	if room.TeamID != teamID {
		return nil, fmt.Errorf("room does not belong to team")
	}

	if req.Name != nil {
		if err := validateRoomName(*req.Name); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidRoomConfiguration, err)
		}
		room.Name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		room.Description = strings.TrimSpace(*req.Description)
	}

	if err := db.UpdateRoom(ctx, room); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		log.Error("failed to update room", "room_id", roomID, "error", err)
		return nil, fmt.Errorf("failed to update room: %w", err)
	}
	return room, nil
}

// DeleteRoom removes a room record.
func DeleteRoom(ctx context.Context, db *sqlite.DB, teamID models.TeamID, roomID models.RoomID) error {
	room, err := db.GetRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return fmt.Errorf("failed to load room: %w", err)
	}
	if room.TeamID != teamID {
		return fmt.Errorf("room does not belong to team")
	}
	if err := db.DeleteRoom(ctx, roomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return fmt.Errorf("failed to delete room: %w", err)
	}
	return nil
}

// ListRoomMembers returns detailed members for a room.
func ListRoomMembers(ctx context.Context, db *sqlite.DB, teamID models.TeamID, roomID models.RoomID) ([]*models.RoomMemberDetail, error) {
	room, err := db.GetRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("failed to load room: %w", err)
	}
	if room.TeamID != teamID {
		return nil, fmt.Errorf("room does not belong to team")
	}
	return db.ListRoomMembers(ctx, roomID)
}

// AddRoomMember associates a user with a room.
func AddRoomMember(ctx context.Context, db *sqlite.DB, teamID models.TeamID, roomID models.RoomID, req *models.AddRoomMemberRequest) error {
	if req == nil {
		return fmt.Errorf("member payload required")
	}
	room, err := db.GetRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return fmt.Errorf("failed to load room: %w", err)
	}
	if room.TeamID != teamID {
		return fmt.Errorf("room does not belong to team")
	}
	if err := db.AddRoomMember(ctx, roomID, req.UserID, req.Role); err != nil {
		return fmt.Errorf("failed to add room member: %w", err)
	}
	return nil
}

// RemoveRoomMember removes a user from the room.
func RemoveRoomMember(ctx context.Context, db *sqlite.DB, teamID models.TeamID, roomID models.RoomID, userID models.UserID) error {
	room, err := db.GetRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return fmt.Errorf("failed to load room: %w", err)
	}
	if room.TeamID != teamID {
		return fmt.Errorf("room does not belong to team")
	}
	if err := db.RemoveRoomMember(ctx, roomID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return fmt.Errorf("failed to remove room member: %w", err)
	}
	return nil
}

// ListRoomChannels returns channel configurations for a room.
func ListRoomChannels(ctx context.Context, db *sqlite.DB, teamID models.TeamID, roomID models.RoomID) ([]*models.RoomChannel, error) {
	room, err := db.GetRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("failed to load room: %w", err)
	}
	if room.TeamID != teamID {
		return nil, fmt.Errorf("room does not belong to team")
	}
	return db.ListRoomChannels(ctx, roomID)
}

// CreateRoomChannel creates a channel configuration.
func CreateRoomChannel(ctx context.Context, db *sqlite.DB, teamID models.TeamID, roomID models.RoomID, req *models.CreateRoomChannelRequest) (*models.RoomChannel, error) {
	if req == nil {
		return nil, ErrInvalidRoomConfiguration
	}
	room, err := db.GetRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("failed to load room: %w", err)
	}
	if room.TeamID != teamID {
		return nil, fmt.Errorf("room does not belong to team")
	}
	channel := &models.RoomChannel{
		RoomID:  roomID,
		Type:    req.Type,
		Name:    strings.TrimSpace(req.Name),
		Config:  req.Config,
		Enabled: req.Enabled,
	}
	if channel.Config == nil {
		channel.Config = map[string]any{}
	}
	if err := validateRoomChannel(channel); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRoomConfiguration, err)
	}
	if err := db.CreateRoomChannel(ctx, channel); err != nil {
		return nil, fmt.Errorf("failed to create room channel: %w", err)
	}
	return channel, nil
}

// UpdateRoomChannel updates an existing channel configuration.
func UpdateRoomChannel(ctx context.Context, db *sqlite.DB, teamID models.TeamID, roomID models.RoomID, channelID int64, req *models.UpdateRoomChannelRequest) (*models.RoomChannel, error) {
	if req == nil {
		return nil, ErrInvalidRoomConfiguration
	}
	room, err := db.GetRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("failed to load room: %w", err)
	}
	if room.TeamID != teamID {
		return nil, fmt.Errorf("room does not belong to team")
	}
	existing, err := db.GetRoomChannel(ctx, channelID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("failed to load room channel: %w", err)
	}
	if existing.RoomID != roomID {
		return nil, fmt.Errorf("channel does not belong to room")
	}
	if req.Name != nil {
		existing.Name = strings.TrimSpace(*req.Name)
	}
	if req.Config != nil {
		existing.Config = *req.Config
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if existing.Config == nil {
		existing.Config = map[string]any{}
	}
	if err := validateRoomChannel(existing); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRoomConfiguration, err)
	}
	if err := db.UpdateRoomChannel(ctx, existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("failed to update room channel: %w", err)
	}
	return existing, nil
}

// DeleteRoomChannel removes a channel config.
func DeleteRoomChannel(ctx context.Context, db *sqlite.DB, teamID models.TeamID, roomID models.RoomID, channelID int64) error {
	room, err := db.GetRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return fmt.Errorf("failed to load room: %w", err)
	}
	if room.TeamID != teamID {
		return fmt.Errorf("room does not belong to team")
	}
	channel, err := db.GetRoomChannel(ctx, channelID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return fmt.Errorf("failed to load room channel: %w", err)
	}
	if channel.RoomID != roomID {
		return fmt.Errorf("channel does not belong to room")
	}
	if err := db.DeleteRoomChannel(ctx, channelID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return fmt.Errorf("failed to delete room channel: %w", err)
	}
	return nil
}
