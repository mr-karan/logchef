package models

import "time"

type RoomID int64

// Room represents a reusable notification group scoped to a team.
type Room struct {
	ID          RoomID   `json:"id"`
	TeamID      TeamID   `json:"team_id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// RoomMember connects a user to a room.
type RoomMember struct {
	RoomID  RoomID   `json:"room_id"`
	UserID  UserID   `json:"user_id"`
	Role    string   `json:"role"`
	AddedAt time.Time `json:"added_at"`
}

type RoomMemberDetail struct {
	RoomID  RoomID   `json:"room_id"`
	UserID  UserID   `json:"user_id"`
	Name    string   `json:"name"`
	Email   string   `json:"email"`
	Role    string   `json:"role"`
	AddedAt time.Time `json:"added_at"`
}

type RoomChannelType string

const (
	RoomChannelSlack   RoomChannelType = "slack"
	RoomChannelWebhook RoomChannelType = "webhook"
)

// RoomChannel defines a non-email delivery target for a room.
type RoomChannel struct {
	ID        int64            `json:"id"`
	RoomID    RoomID           `json:"room_id"`
	Type      RoomChannelType  `json:"type"`
	Name      string           `json:"name,omitempty"`
	Config    map[string]any   `json:"config"`
	Enabled   bool             `json:"enabled"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// RoomSummary is a lightweight representation suitable for alerts.
type RoomSummary struct {
	ID           RoomID            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	MemberCount  int               `json:"member_count"`
	ChannelTypes []RoomChannelType `json:"channel_types"`
}

// AlertHistoryRoomSnapshot captures the room state at trigger time.
type AlertHistoryRoomSnapshot struct {
	RoomID       RoomID            `json:"room_id"`
	Name         string            `json:"name"`
	ChannelTypes []RoomChannelType `json:"channel_types"`
	MemberEmails []string          `json:"member_emails,omitempty"`
}

type CreateRoomRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type UpdateRoomRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type AddRoomMemberRequest struct {
	UserID UserID `json:"user_id"`
	Role   string `json:"role"`
}

type CreateRoomChannelRequest struct {
	Type    RoomChannelType `json:"type"`
	Name    string          `json:"name"`
	Config  map[string]any  `json:"config"`
	Enabled bool            `json:"enabled"`
}

type UpdateRoomChannelRequest struct {
	Name    *string          `json:"name"`
	Config  *map[string]any  `json:"config"`
	Enabled *bool            `json:"enabled"`
}
