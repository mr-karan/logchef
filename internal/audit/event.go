// Package audit records configuration changes and security events (RFC 0009).
// It owns the action registry, the redacted field snapshots, and the Recorder
// that writes each event to the metadata store and to the log stream.
package audit

import "github.com/mr-karan/logchef/pkg/models"

// Actor is who acted and how they authenticated.
type Actor struct {
	Type         models.AuditActorType
	ID           *models.UserID
	Label        string
	AuthMethod   models.AuditAuthMethod
	CredentialID string
	ClientID     string
}

// Target is the object an event applies to.
type Target struct {
	Type, ID, Label string
	TeamID          *models.TeamID
	SourceID        *models.SourceID
}

// Request is the HTTP request that caused an event. It is empty for an event
// outside HTTP.
type Request struct {
	IP        string
	UserAgent string
	Method    string
	Route     string
	Status    int
}

// Event is one audited action before it is stored. A nil Before means a
// create, a nil After means a delete; the recorder keeps only the keys whose
// values differ.
type Event struct {
	Action  Action
	Outcome models.AuditOutcome
	Reason  string
	Actor   Actor
	Target  Target
	Request Request
	Before  *Fields
	After   *Fields
	Detail  *Fields
}
