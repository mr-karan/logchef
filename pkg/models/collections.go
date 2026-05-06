package models

import "time"

// CollectionRole captures membership roles within a collection.
type CollectionRole string

const (
	// CollectionRoleOwner can edit, invite, remove members, delete the collection.
	CollectionRoleOwner CollectionRole = "owner"
	// CollectionRoleMember can read items and run queries they have source access to.
	CollectionRoleMember CollectionRole = "member"
)

// Collection groups saved queries across teams. Each user has a single
// auto-created personal collection (is_personal = true); other collections
// are shared with explicit member invites.
type Collection struct {
	ID          int            `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	IsPersonal  bool           `json:"is_personal"`
	CreatedBy   *UserID        `json:"created_by,omitempty"`
	CallerRole  CollectionRole `json:"caller_role"`
	MemberCount int            `json:"member_count"`
	ItemCount   int            `json:"item_count"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// CollectionMember is one membership row.
type CollectionMember struct {
	CollectionID int            `json:"collection_id"`
	UserID       UserID         `json:"user_id"`
	Role         CollectionRole `json:"role"`
	AddedBy      *UserID        `json:"added_by,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	Email        string         `json:"email,omitempty"`
	FullName     string         `json:"full_name,omitempty"`
}

// CollectionItem is a saved query included in a collection. Runnable indicates
// whether the requesting user has source access to actually run it; non-runnable
// items still surface in listings (with a lock icon on the UI side) so members
// can see the curated list even if some queries are out of reach.
type CollectionItem struct {
	CollectionID int        `json:"collection_id"`
	SortOrder    int        `json:"sort_order"`
	AddedBy      *UserID    `json:"added_by,omitempty"`
	ItemAddedAt  time.Time  `json:"item_added_at"`
	Query        SavedQuery `json:"query"`
	Runnable     bool       `json:"runnable"`
}

// CreateCollectionRequest is the JSON body for POST /api/v1/collections.
type CreateCollectionRequest struct {
	Name        string `json:"name" validate:"required"`
	Description string `json:"description"`
}

// UpdateCollectionRequest is the JSON body for PUT /api/v1/collections/:id.
type UpdateCollectionRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AddCollectionMemberRequest is the JSON body for POST /api/v1/collections/:id/members.
type AddCollectionMemberRequest struct {
	UserID UserID         `json:"user_id" validate:"required"`
	Role   CollectionRole `json:"role" validate:"required"`
}

// AddCollectionItemRequest is the JSON body for POST /api/v1/collections/:id/items.
type AddCollectionItemRequest struct {
	SavedQueryID int `json:"saved_query_id" validate:"required"`
	SortOrder    int `json:"sort_order"`
}
