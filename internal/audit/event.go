// Package audit records configuration changes and security events (RFC 0009).
// It owns the action registry, the redacted field snapshots, and the Recorder
// that writes each event to the metadata store and to the log stream.
package audit

import (
	"context"

	"github.com/mr-karan/logchef/pkg/models"
)

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

type requestKey struct{}

// WithRequest carries the request metadata to code that records outside the
// audit middleware, such as the OAuth endpoints.
func WithRequest(ctx context.Context, r Request) context.Context {
	return context.WithValue(ctx, requestKey{}, r)
}

// RequestFrom returns the metadata that WithRequest stored. It is empty for
// a context that no HTTP request carried.
func RequestFrom(ctx context.Context) Request {
	r, _ := ctx.Value(requestKey{}).(Request)
	return r
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
