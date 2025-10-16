package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// GetRoom fetches a room by identifier.
func (db *DB) GetRoom(ctx context.Context, roomID models.RoomID) (*models.Room, error) {
	row := db.db.QueryRowContext(ctx, selectRoomBase+" WHERE id = ?", int64(roomID))

	var (
		id          int64
		teamID      int64
		name        string
		description sql.NullString
		createdAt   time.Time
		updatedAt   time.Time
	)

	if err := row.Scan(&id, &teamID, &name, &description, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan room: %w", err)
	}

	return &models.Room{
		ID:          models.RoomID(id),
		TeamID:      models.TeamID(teamID),
		Name:        name,
		Description: description.String,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

// ListRoomsByTeam returns rooms owned by a team.
func (db *DB) ListRoomsByTeam(ctx context.Context, teamID models.TeamID) ([]*models.Room, error) {
	rows, err := db.db.QueryContext(ctx, selectRoomBase+" WHERE team_id = ? ORDER BY name", int64(teamID))
	if err != nil {
		return nil, fmt.Errorf("failed to list rooms: %w", err)
	}
	defer rows.Close()

	var rooms []*models.Room
	for rows.Next() {
		var (
			id          int64
			team        int64
			name        string
			description sql.NullString
			createdAt   time.Time
			updatedAt   time.Time
		)
		if err := rows.Scan(&id, &team, &name, &description, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan room: %w", err)
		}
		rooms = append(rooms, &models.Room{
			ID:          models.RoomID(id),
			TeamID:      models.TeamID(team),
			Name:        name,
			Description: description.String,
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rooms: %w", err)
	}
	return rooms, nil
}

// GetRoomSummary returns aggregate information for a room.
func (db *DB) GetRoomSummary(ctx context.Context, roomID models.RoomID) (*models.RoomSummary, error) {
	row := db.db.QueryRowContext(ctx, selectRoomBase+" WHERE id = ?", int64(roomID))
	var (
		id          int64
		teamID      int64
		name        string
		description sql.NullString
		createdAt   time.Time
		updatedAt   time.Time
	)
	if err := row.Scan(&id, &teamID, &name, &description, &createdAt, &updatedAt); err != nil {
		return nil, fmt.Errorf("failed to load room %d: %w", roomID, err)
	}

	var memberCount int
	if err := db.db.QueryRowContext(ctx, countRoomMembersQuery, int64(roomID)).Scan(&memberCount); err != nil {
		return nil, fmt.Errorf("failed to count room members for %d: %w", roomID, err)
	}

	channelRows, err := db.db.QueryContext(ctx, listRoomChannelTypesQuery, int64(roomID))
	if err != nil {
		return nil, fmt.Errorf("failed to list room channel types for %d: %w", roomID, err)
	}
	defer channelRows.Close()
	var channelTypes []models.RoomChannelType
	for channelRows.Next() {
		var channelType string
		if err := channelRows.Scan(&channelType); err != nil {
			return nil, fmt.Errorf("failed to scan room channel type: %w", err)
		}
		channelTypes = append(channelTypes, models.RoomChannelType(channelType))
	}
	if err := channelRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating room channel types: %w", err)
	}

	return &models.RoomSummary{
		ID:           models.RoomID(id),
		Name:         name,
		Description:  description.String,
		MemberCount:  memberCount,
		ChannelTypes: channelTypes,
	}, nil
}

// CreateRoom persists a new room for a team.
func (db *DB) CreateRoom(ctx context.Context, room *models.Room) error {
	if room == nil {
		return fmt.Errorf("room payload is required")
	}
	row := db.db.QueryRowContext(ctx, `INSERT INTO rooms (team_id, name, description)
VALUES (?, ?, ?)
RETURNING id, created_at, updated_at`, int64(room.TeamID), room.Name, nullableString(room.Description))

	var (
		id        int64
		createdAt time.Time
		updatedAt time.Time
	)
	if err := row.Scan(&id, &createdAt, &updatedAt); err != nil {
		return fmt.Errorf("failed to insert room: %w", err)
	}
	room.ID = models.RoomID(id)
	room.CreatedAt = createdAt
	room.UpdatedAt = updatedAt
	return nil
}

// UpdateRoom updates mutable fields for a room.
func (db *DB) UpdateRoom(ctx context.Context, room *models.Room) error {
	if room == nil {
		return fmt.Errorf("room payload is required")
	}
	res, err := db.db.ExecContext(ctx, `UPDATE rooms
SET name = ?, description = ?, updated_at = datetime('now')
WHERE id = ?`, room.Name, nullableString(room.Description), int64(room.ID))
	if err != nil {
		return fmt.Errorf("failed to update room: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteRoom removes a room entry.
func (db *DB) DeleteRoom(ctx context.Context, roomID models.RoomID) error {
	res, err := db.db.ExecContext(ctx, `DELETE FROM rooms WHERE id = ?`, int64(roomID))
	if err != nil {
		return fmt.Errorf("failed to delete room: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AddRoomMember upserts a member into a room.
func (db *DB) AddRoomMember(ctx context.Context, roomID models.RoomID, userID models.UserID, role string) error {
	if role == "" {
		role = "member"
	}
	if _, err := db.db.ExecContext(ctx, `INSERT INTO room_members (room_id, user_id, role)
VALUES (?, ?, ?)
ON CONFLICT(room_id, user_id) DO UPDATE SET role = excluded.role, added_at = datetime('now')`,
		int64(roomID), int64(userID), role); err != nil {
		return fmt.Errorf("failed to add room member: %w", err)
	}
	return nil
}

// RemoveRoomMember removes a member from a room.
func (db *DB) RemoveRoomMember(ctx context.Context, roomID models.RoomID, userID models.UserID) error {
	res, err := db.db.ExecContext(ctx, `DELETE FROM room_members WHERE room_id = ? AND user_id = ?`, int64(roomID), int64(userID))
	if err != nil {
		return fmt.Errorf("failed to remove room member: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListRoomMembers returns the members and their metadata for a room.
func (db *DB) ListRoomMembers(ctx context.Context, roomID models.RoomID) ([]*models.RoomMemberDetail, error) {
	rows, err := db.db.QueryContext(ctx, `SELECT rm.room_id, rm.user_id, u.full_name, u.email, rm.role, rm.added_at
FROM room_members rm
JOIN users u ON u.id = rm.user_id
WHERE rm.room_id = ?
ORDER BY u.email`, int64(roomID))
	if err != nil {
		return nil, fmt.Errorf("failed to list room members: %w", err)
	}

	defer rows.Close()

	var members []*models.RoomMemberDetail
	for rows.Next() {
		var (
			room    int64
			user    int64
			name    sql.NullString
			email   sql.NullString
			role    string
			addedAt time.Time
		)
		if err := rows.Scan(&room, &user, &name, &email, &role, &addedAt); err != nil {
			return nil, fmt.Errorf("failed to scan room member: %w", err)
		}
		members = append(members, &models.RoomMemberDetail{
			RoomID:  models.RoomID(room),
			UserID:  models.UserID(user),
			Name:    name.String,
			Email:   email.String,
			Role:    role,
			AddedAt: addedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating room members: %w", err)
	}
	return members, nil
}

// ListRoomMemberEmails returns email addresses for all members of a room.
func (db *DB) ListRoomMemberEmails(ctx context.Context, roomID models.RoomID) ([]string, error) {
	rows, err := db.db.QueryContext(ctx, `SELECT u.email
FROM room_members rm
JOIN users u ON u.id = rm.user_id
WHERE rm.room_id = ?
ORDER BY u.email`, int64(roomID))
	if err != nil {
		return nil, fmt.Errorf("failed to list room member emails: %w", err)
	}
	defer rows.Close()

	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, fmt.Errorf("failed to scan room member email: %w", err)
		}
		if email != "" {
			emails = append(emails, email)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating room member emails: %w", err)
	}
	return emails, nil
}

// ListEnabledRoomChannels returns the non-email channels configured for a room.
func (db *DB) ListEnabledRoomChannels(ctx context.Context, roomID models.RoomID) ([]*models.RoomChannel, error) {
	rows, err := db.db.QueryContext(ctx, `SELECT id, room_id, type, name, config_json, enabled, created_at, updated_at
FROM room_channels
WHERE room_id = ? AND enabled = 1
ORDER BY created_at ASC`, int64(roomID))
	if err != nil {
		return nil, fmt.Errorf("failed to list room channels: %w", err)
	}
	defer rows.Close()

	var channels []*models.RoomChannel
	for rows.Next() {
		var (
			id        int64
			room      int64
			typ       string
			name      sql.NullString
			configRaw string
			enabled   int64
			createdAt time.Time
			updatedAt time.Time
		)
		if err := rows.Scan(&id, &room, &typ, &name, &configRaw, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan room channel: %w", err)
		}
		var config map[string]any
		if err := json.Unmarshal([]byte(configRaw), &config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal room channel config: %w", err)
		}
		channels = append(channels, &models.RoomChannel{
			ID:        id,
			RoomID:    models.RoomID(room),
			Type:      models.RoomChannelType(typ),
			Name:      name.String,
			Config:    config,
			Enabled:   enabled == 1,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating room channels: %w", err)
	}
	return channels, nil
}

// ListRoomChannels returns all channels (including disabled ones) for a room.
func (db *DB) ListRoomChannels(ctx context.Context, roomID models.RoomID) ([]*models.RoomChannel, error) {
	rows, err := db.db.QueryContext(ctx, `SELECT id, room_id, type, name, config_json, enabled, created_at, updated_at
FROM room_channels
WHERE room_id = ?
ORDER BY created_at ASC`, int64(roomID))
	if err != nil {
		return nil, fmt.Errorf("failed to list room channels: %w", err)
	}
	defer rows.Close()

	var channels []*models.RoomChannel
	for rows.Next() {
		var (
			id        int64
			room      int64
			typ       string
			name      sql.NullString
			configRaw string
			enabled   int64
			createdAt time.Time
			updatedAt time.Time
		)
		if err := rows.Scan(&id, &room, &typ, &name, &configRaw, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan room channel: %w", err)
		}
		var config map[string]any
		if err := json.Unmarshal([]byte(configRaw), &config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal room channel config: %w", err)
		}
		channels = append(channels, &models.RoomChannel{
			ID:        id,
			RoomID:    models.RoomID(room),
			Type:      models.RoomChannelType(typ),
			Name:      name.String,
			Config:    config,
			Enabled:   enabled == 1,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating room channels: %w", err)
	}
	return channels, nil
}

// CreateRoomChannel stores a new channel configuration for a room.
func (db *DB) CreateRoomChannel(ctx context.Context, channel *models.RoomChannel) error {
	if channel == nil {
		return fmt.Errorf("channel payload is required")
	}
	configJSON, err := json.Marshal(channel.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal room channel config: %w", err)
	}
	row := db.db.QueryRowContext(ctx, `INSERT INTO room_channels (room_id, type, name, config_json, enabled)
VALUES (?, ?, ?, ?, ?)
RETURNING id, created_at, updated_at`, int64(channel.RoomID), string(channel.Type), nullableString(channel.Name), string(configJSON), boolToInt(channel.Enabled))

	var (
		id        int64
		createdAt time.Time
		updatedAt time.Time
	)
	if err := row.Scan(&id, &createdAt, &updatedAt); err != nil {
		return fmt.Errorf("failed to insert room channel: %w", err)
	}
	channel.ID = id
	channel.CreatedAt = createdAt
	channel.UpdatedAt = updatedAt
	return nil
}

// UpdateRoomChannel updates a channel definition.
func (db *DB) UpdateRoomChannel(ctx context.Context, channel *models.RoomChannel) error {
	if channel == nil {
		return fmt.Errorf("channel payload is required")
	}
	configJSON, err := json.Marshal(channel.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal room channel config: %w", err)
	}
	res, err := db.db.ExecContext(ctx, `UPDATE room_channels
SET name = ?, config_json = ?, enabled = ?, updated_at = datetime('now')
WHERE id = ?`, nullableString(channel.Name), string(configJSON), boolToInt(channel.Enabled), channel.ID)
	if err != nil {
		return fmt.Errorf("failed to update room channel: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteRoomChannel removes a channel record.
func (db *DB) DeleteRoomChannel(ctx context.Context, channelID int64) error {
	res, err := db.db.ExecContext(ctx, `DELETE FROM room_channels WHERE id = ?`, channelID)
	if err != nil {
		return fmt.Errorf("failed to delete room channel: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetRoomChannel returns a channel by identifier.
func (db *DB) GetRoomChannel(ctx context.Context, channelID int64) (*models.RoomChannel, error) {
	row := db.db.QueryRowContext(ctx, `SELECT id, room_id, type, name, config_json, enabled, created_at, updated_at FROM room_channels WHERE id = ?`, channelID)

	var (
		id        int64
		roomID    int64
		typ       string
		name      sql.NullString
		configRaw string
		enabled   int64
		createdAt time.Time
		updatedAt time.Time
	)

	if err := row.Scan(&id, &roomID, &typ, &name, &configRaw, &enabled, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to scan room channel: %w", err)
	}

	var config map[string]any
	if err := json.Unmarshal([]byte(configRaw), &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal room channel config: %w", err)
	}

	return &models.RoomChannel{
		ID:        id,
		RoomID:    models.RoomID(roomID),
		Type:      models.RoomChannelType(typ),
		Name:      name.String,
		Config:    config,
		Enabled:   enabled == 1,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}
