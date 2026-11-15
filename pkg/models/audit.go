package models

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// AuditTimePrecision is the precision at which every store keeps occurred_at.
const AuditTimePrecision = time.Millisecond

// AuditOutcome says whether the audited action took effect.
type AuditOutcome string

const (
	AuditOutcomeSuccess AuditOutcome = "success"
	AuditOutcomeFailure AuditOutcome = "failure"
)

// AuditActorType says who acted.
type AuditActorType string

const (
	AuditActorUser           AuditActorType = "user"
	AuditActorServiceAccount AuditActorType = "service_account"
	AuditActorSystem         AuditActorType = "system"
	AuditActorAnonymous      AuditActorType = "anonymous"
)

// AuditAuthMethod says how the actor authenticated.
type AuditAuthMethod string

const (
	AuditAuthSession      AuditAuthMethod = "session"
	AuditAuthAPIToken     AuditAuthMethod = "api_token"
	AuditAuthOAuthAPI     AuditAuthMethod = "oauth_api"
	AuditAuthOAuthMCP     AuditAuthMethod = "oauth_mcp"
	AuditAuthAssistant    AuditAuthMethod = "assistant"
	AuditAuthProvisioning AuditAuthMethod = "provisioning"
	AuditAuthStartup      AuditAuthMethod = "startup"
	AuditAuthScheduler    AuditAuthMethod = "scheduler"
	AuditAuthNone         AuditAuthMethod = "none"
)

// AuditEvent is one row of audit_events and the wire format of the audit API.
// Changes and Detail are JSON objects built by internal/audit, which redacts
// them; the store persists them as given and writes {} for an empty value.
type AuditEvent struct {
	ID           string          `json:"id"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Action       string          `json:"action"`
	Outcome      AuditOutcome    `json:"outcome"`
	Reason       string          `json:"reason"`
	ActorType    AuditActorType  `json:"actor_type"`
	ActorID      *UserID         `json:"actor_id"`
	ActorLabel   string          `json:"actor_label"`
	AuthMethod   AuditAuthMethod `json:"auth_method"`
	CredentialID string          `json:"credential_id"`
	ClientID     string          `json:"client_id"`
	TargetType   string          `json:"target_type"`
	TargetID     string          `json:"target_id"`
	TargetLabel  string          `json:"target_label"`
	TeamID       *TeamID         `json:"team_id"`
	SourceID     *SourceID       `json:"source_id"`
	IP           string          `json:"ip"`
	UserAgent    string          `json:"user_agent"`
	HTTPMethod   string          `json:"http_method"`
	Route        string          `json:"route"`
	Status       int             `json:"status"`
	Changes      json.RawMessage `json:"changes"`
	Detail       json.RawMessage `json:"detail"`
}

// AuditCursor is the keyset position of the last event of a page.
type AuditCursor struct {
	OccurredAt time.Time
	ID         string
}

// AuditFilter selects audit events. A zero value in a field means no filter
// on it. From is inclusive and To exclusive. Events are ordered by
// occurred_at then id, newest first unless Ascending; Cursor returns the
// events after that position in the same order.
type AuditFilter struct {
	From         time.Time
	To           time.Time
	Action       string
	ActionPrefix string
	ActorID      *UserID
	AuthMethod   AuditAuthMethod
	CredentialID string
	TargetType   string
	TargetID     string
	TeamID       *TeamID
	SourceID     *SourceID
	Outcome      AuditOutcome
	Ascending    bool
	Limit        int
	Cursor       *AuditCursor
}

// Validate rejects an event that a store must not persist: one without a
// required field, with a status that is neither 0 nor an HTTP status, or
// whose changes or detail is not a JSON object.
func (e *AuditEvent) Validate() error {
	switch {
	case e.ID == "":
		return errors.New("audit event: id is required")
	case e.OccurredAt.IsZero():
		return errors.New("audit event: occurred_at is required")
	case e.Action == "":
		return errors.New("audit event: action is required")
	case e.Outcome == "":
		return errors.New("audit event: outcome is required")
	case e.ActorType == "":
		return errors.New("audit event: actor_type is required")
	case e.AuthMethod == "":
		return errors.New("audit event: auth_method is required")
	case e.Status != 0 && (e.Status < 100 || e.Status > 599):
		return fmt.Errorf("audit event: status %d is not an HTTP status", e.Status)
	}
	for name, raw := range map[string]json.RawMessage{"changes": e.Changes, "detail": e.Detail} {
		if len(raw) > 0 && (!json.Valid(raw) || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{"))) {
			return fmt.Errorf("audit event: %s must be a JSON object", name)
		}
	}
	return nil
}

// AuditJSONObject returns raw, or {} when raw is empty.
func AuditJSONObject(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	return raw
}

// Validate rejects a filter that cannot select a page.
func (f AuditFilter) Validate() error {
	if f.Limit <= 0 {
		return errors.New("audit filter: limit must be positive")
	}
	if f.Cursor != nil && (f.Cursor.ID == "" || f.Cursor.OccurredAt.IsZero()) {
		return errors.New("audit filter: cursor needs occurred_at and id")
	}
	return nil
}
