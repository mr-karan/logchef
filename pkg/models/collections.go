package models

import "time"

// CollectionRole captures membership roles within a collection.
type CollectionRole string

const (
	// CollectionRoleOwner can edit, invite, remove members, delete the collection.
	CollectionRoleOwner CollectionRole = "owner"
	// CollectionRoleEditor can edit the saved queries curated in the collection
	// and add/remove items, but cannot manage members or delete the collection.
	CollectionRoleEditor CollectionRole = "editor"
	// CollectionRoleMember can read items and run queries they have source access to.
	CollectionRoleMember CollectionRole = "member"
)

// Collection groups saved queries across teams. Each user has a single
// auto-created personal collection (is_personal = true); other collections
// are shared with explicit member invites and with whole teams.
// MemberCount counts direct membership rows (owners included); TeamCount
// counts team shares. Users who see the collection only through a team share
// are not part of MemberCount.
type Collection struct {
	ID          int            `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	IsPersonal  bool           `json:"is_personal"`
	CreatedBy   *UserID        `json:"created_by,omitempty"`
	CallerRole  CollectionRole `json:"caller_role"`
	MemberCount int            `json:"member_count"`
	TeamCount   int            `json:"team_count"`
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

// CollectionTeam is one team share. Every current member of the team gets the
// collection Member role; a direct membership row takes precedence.
type CollectionTeam struct {
	CollectionID int       `json:"collection_id"`
	TeamID       TeamID    `json:"team_id"`
	TeamName     string    `json:"team_name"`
	AddedBy      *UserID   `json:"added_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// CollectionLinkKind is what a link between a collection and a saved query
// grants. Authority over a saved query belongs to its creator and to global
// admins; only they create, convert or remove a managed link.
type CollectionLinkKind string

const (
	// CollectionLinkPin is a bookmark. It lists the query for participants who
	// can already see it and grants nothing.
	CollectionLinkPin CollectionLinkKind = "pin"
	// CollectionLinkManaged delegates edit on the saved query to the owners
	// and editors of a shared collection.
	CollectionLinkManaged CollectionLinkKind = "managed"
)

// Valid reports whether k is one of the two link kinds.
func (k CollectionLinkKind) Valid() bool {
	return k == CollectionLinkPin || k == CollectionLinkManaged
}

// Other returns the kind a link of kind k does not have.
func (k CollectionLinkKind) Other() CollectionLinkKind {
	if k == CollectionLinkManaged {
		return CollectionLinkPin
	}
	return CollectionLinkManaged
}

// CollectionItemLink is one link row, as the item routes return it.
type CollectionItemLink struct {
	CollectionID int                `json:"collection_id"`
	SavedQueryID int                `json:"saved_query_id"`
	Kind         CollectionLinkKind `json:"kind"`
	AddedBy      *UserID            `json:"added_by"`
	SortOrder    int                `json:"sort_order"`
	CreatedAt    time.Time          `json:"created_at"`
}

// CollectionItem is a saved query included in a collection. Runnable indicates
// whether the requesting user has source access to actually run it; non-runnable
// items still surface in listings (with a lock icon on the UI side) so members
// can see the curated list even if some queries are out of reach.
type CollectionItem struct {
	CollectionID int                `json:"collection_id"`
	Kind         CollectionLinkKind `json:"kind"`
	SortOrder    int                `json:"sort_order"`
	AddedBy      *UserID            `json:"added_by,omitempty"`
	ItemAddedAt  time.Time          `json:"item_added_at"`
	Query        SavedQuery         `json:"query"`
	Runnable     bool               `json:"runnable"`
}

// CollectionLinkReportRow is one pin in a shared collection. Right after the
// kind migration, the report is exactly the links that lost delegation.
// Editors are the owners and editors of the collection, who get edit on the
// query when the link is converted.
type CollectionLinkReportRow struct {
	CollectionID      int                `json:"collection_id"`
	CollectionName    string             `json:"collection_name"`
	SavedQueryID      int                `json:"saved_query_id"`
	SavedQueryName    string             `json:"saved_query_name"`
	QueryCreatedBy    *UserID            `json:"query_created_by"`
	QueryCreatorEmail string             `json:"query_creator_email,omitempty"`
	AddedBy           *UserID            `json:"added_by"`
	AddedByEmail      string             `json:"added_by_email,omitempty"`
	CreatedAt         time.Time          `json:"created_at"`
	Editors           []CollectionMember `json:"editors"`
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

// AddCollectionTeamRequest is the JSON body for POST /api/v1/collections/:id/teams.
type AddCollectionTeamRequest struct {
	TeamID TeamID `json:"team_id" validate:"required"`
}

// AddCollectionItemRequest is the JSON body for POST /api/v1/collections/:id/items.
// An omitted Kind means a pin, so a client from before link kinds cannot
// create a delegation by accident.
type AddCollectionItemRequest struct {
	SavedQueryID int                `json:"saved_query_id" validate:"required"`
	SortOrder    int                `json:"sort_order"`
	Kind         CollectionLinkKind `json:"kind"`
}

// UpdateCollectionItemRequest is the JSON body for
// PUT /api/v1/collections/:id/items/:queryID. Kind is required.
type UpdateCollectionItemRequest struct {
	Kind CollectionLinkKind `json:"kind"`
}
