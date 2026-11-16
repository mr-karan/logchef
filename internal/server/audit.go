package server

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"
)

// auditStageKey holds the *auditStage a handler fills for the audit
// middleware.
type auditStageKey struct{}

// auditStage is what a handler staged for its route's audit event.
type auditStage struct {
	action        audit.Action
	target        *audit.Target
	before, after *audit.Fields
	fields        bool
	detail        *audit.Fields
	actor         *audit.Actor
	outcome       models.AuditOutcome
	reason        string
	// skip says that the middleware records nothing: the handler stored the
	// event with the change, or the request changed nothing.
	skip bool
}

func stagedAudit(c fiber.Ctx) *auditStage {
	st, _ := c.Locals(auditStageKey{}).(*auditStage)
	return st
}

func auditStageFor(c fiber.Ctx) *auditStage {
	if st := stagedAudit(c); st != nil {
		return st
	}
	st := &auditStage{}
	c.Locals(auditStageKey{}, st)
	return st
}

// auditSet stages the target and its snapshots from the mutation. A nil
// before means a create, a nil after a delete.
func auditSet(c fiber.Ctx, target audit.Target, before, after *audit.Fields) {
	st := auditStageFor(c)
	st.target, st.before, st.after, st.fields = &target, before, after, true
}

// clearAuditFields drops the staged target and fields, for a change that
// rolled back after the handler staged them.
func clearAuditFields(c fiber.Ctx) {
	if st := stagedAudit(c); st != nil {
		st.target, st.before, st.after, st.fields = nil, nil, nil, false
	}
}

// auditTarget stages the target without fields.
func auditTarget(c fiber.Ctx, target audit.Target) {
	auditStageFor(c).target = &target
}

// auditDetail adds one detail field.
func auditDetail(c fiber.Ctx, key string, value any) {
	st := auditStageFor(c)
	if st.detail == nil {
		st.detail = audit.NewFields()
	}
	st.detail.Set(key, value)
}

// auditAction replaces the route's action with another action of the same
// resource. Any other action is a bug in the handler.
func auditAction(c fiber.Ctx, action audit.Action) {
	route, ok := audit.Lookup(c.Route().Name)
	next, nextOK := audit.Lookup(string(action))
	if !ok || !nextOK || route.TargetType != next.TargetType {
		panic("audit: " + string(action) + " cannot replace the action of route " + c.Route().Name)
	}
	auditStageFor(c).action = action
}

// auditAuth stages the actor, the outcome and a fixed reason code for a
// route that runs without requireAuth.
func auditAuth(c fiber.Ctx, actor audit.Actor, outcome models.AuditOutcome, reason string) {
	st := auditStageFor(c)
	st.actor, st.outcome, st.reason = &actor, outcome, reason
}

// auditSession stages the user of a sign-in or a sign-out. The response
// status decides the outcome. method is local or oidc for a sign-in.
func auditSession(c fiber.Ctx, user *models.User, method string) {
	auditAuth(c, userActor(user, models.AuditAuthSession), "", "")
	auditTarget(c, audit.SessionUserTarget(&user.ID, user.Email))
	if method != "" {
		auditDetail(c, "method", method)
	}
}

// auditSignInFailure stages a failed sign-in. No user is known, so the actor
// is anonymous and its label is the attempted email, if any.
func auditSignInFailure(c fiber.Ctx, email, method, reason string) {
	auditAuth(c, audit.Actor{Type: models.AuditActorAnonymous, Label: email, AuthMethod: models.AuditAuthNone}, models.AuditOutcomeFailure, reason)
	auditTarget(c, audit.SessionUserTarget(nil, email))
	auditDetail(c, "method", method)
}

// userActor is the actor for a user who signed in with a session.
func userActor(user *models.User, method models.AuditAuthMethod) audit.Actor {
	a := audit.Actor{Type: models.AuditActorUser, ID: &user.ID, Label: user.Email, AuthMethod: method}
	if user.AccountType == models.UserAccountTypeService {
		a.Type = models.AuditActorServiceAccount
		if user.FullName != "" {
			a.Label = user.FullName
		}
	}
	return a
}

// requestActor builds the actor from what requireAuth stored. ok is false
// when the request carries no authenticated user.
func requestActor(c fiber.Ctx) (audit.Actor, bool) {
	p := principalFromLocals(c)
	if p.User == nil {
		return audit.Actor{}, false
	}
	switch p.Method {
	case access.AuthSession:
		return userActor(p.User, models.AuditAuthSession), true
	case access.AuthAPIToken:
		a := userActor(p.User, models.AuditAuthAPIToken)
		if token, ok := c.Locals("api_token").(*models.APIToken); ok && token != nil {
			a.CredentialID = strconv.Itoa(token.ID)
		}
		return a, true
	case access.AuthOAuth:
		method := models.AuditAuthOAuthAPI
		if strings.HasPrefix(c.Route().Path, MCPPath) {
			method = models.AuditAuthOAuthMCP
		}
		a := userActor(p.User, method)
		a.CredentialID, a.ClientID = strconv.Itoa(int(p.GrantID)), string(p.ClientID)
		return a, true
	case access.AuthAssistant:
		return userActor(p.User, models.AuditAuthAssistant), true
	case access.AuthSystem, access.AuthReport:
	}
	return audit.Actor{}, false
}

// responseStatus is the status the client receives. Fiber runs the error
// handler after the middleware chain unwinds, so a handler error has not set
// the response status yet.
func responseStatus(c fiber.Ctx, chainErr error) int {
	status := c.Response().StatusCode()
	if chainErr != nil {
		if e, ok := errors.AsType[*fiber.Error](chainErr); ok {
			status = e.Code
		} else if status == fiber.StatusOK {
			status = fiber.StatusInternalServerError
		}
	}
	return status
}

// auditMiddleware records one event for each request to a route whose name
// is a registered action. It records from a deferred function so that a
// panicking handler still records one failure before recoverMiddleware
// answers.
func (s *Server) auditMiddleware(c fiber.Ctx) (err error) {
	defer func() {
		if p := recover(); p != nil {
			s.recordRouteEvent(c, fiber.StatusInternalServerError)
			panic(p)
		}
		s.recordRouteEvent(c, responseStatus(c, err))
	}()
	return c.Next()
}

func (s *Server) recordRouteEvent(c fiber.Ctx, status int) {
	st := stagedAudit(c)
	if st == nil {
		st = &auditStage{}
	}
	if st.skip {
		return
	}
	if ev, ok := s.routeEvent(c, st, status); ok {
		s.audit.Record(c.Context(), ev)
	}
}

// addRouteEvent adds the route's event, built from what the handler staged,
// to a batch that inserts it in the transaction of the change. The handler
// must call Publish after the transaction commits, and then
// markAuditRecorded.
func (s *Server) addRouteEvent(ctx context.Context, c fiber.Ctx, b *audit.Batch, status int) error {
	st := stagedAudit(c)
	if st == nil {
		st = &auditStage{}
	}
	ev, ok := s.routeEvent(c, st, status)
	if !ok {
		return errors.New("audit: route " + c.Route().Name + " has no event for status " + strconv.Itoa(status))
	}
	return b.Add(ctx, ev)
}

// markAuditRecorded tells the middleware that the handler stored the event.
func markAuditRecorded(c fiber.Ctx) {
	auditStageFor(c).skip = true
}

// auditNoChange tells the middleware that the request changed nothing, such
// as removing a member who is not one, so it records no event.
func auditNoChange(c fiber.Ctx) {
	auditStageFor(c).skip = true
}

// routeEvent builds the event of the request's route. ok is false when the
// route records nothing: no registered action, a 4xx status, or no actor.
func (s *Server) routeEvent(c fiber.Ctx, st *auditStage, status int) (ev audit.Event, ok bool) {
	spec, ok := audit.Lookup(c.Route().Name)
	if !ok {
		return audit.Event{}, false
	}

	outcome := st.outcome
	switch {
	case outcome != "":
	case status < fiber.StatusBadRequest:
		outcome = models.AuditOutcomeSuccess
	case status >= fiber.StatusInternalServerError:
		outcome = models.AuditOutcomeFailure
	default:
		return audit.Event{}, false
	}

	var actor audit.Actor
	if st.actor != nil {
		actor = *st.actor
	} else if a, ok := requestActor(c); ok {
		actor = a
	} else {
		return audit.Event{}, false
	}

	action := spec.Action
	if st.action != "" {
		action = st.action
		spec, _ = audit.Lookup(string(action))
	}

	detail := st.detail
	var target audit.Target
	if st.target != nil {
		target = *st.target
	} else {
		target = routeTarget(c, spec.TargetType)
	}
	unstaged := st.target == nil || (spec.Staged && !st.fields)
	if unstaged && outcome == models.AuditOutcomeSuccess {
		metrics.RecordAuditUnstaged()
		if detail == nil {
			detail = audit.NewFields()
		}
		detail.Set("unstaged", true)
	}

	return audit.Event{
		Action:  action,
		Outcome: outcome,
		Reason:  st.reason,
		Actor:   actor,
		Target:  target,
		Request: audit.Request{
			IP:        c.IP(),
			UserAgent: c.Get(fiber.HeaderUserAgent),
			Method:    c.Method(),
			Route:     c.Route().Path,
			Status:    status,
		},
		Before: st.before,
		After:  st.after,
		Detail: detail,
	}, true
}

// routeTarget builds a target from the route parameters when the handler
// staged none: the last parameter is the ID, and teamID and sourceID fill
// their envelope fields.
func routeTarget(c fiber.Ctx, targetType string) audit.Target {
	t := audit.Target{Type: targetType}
	if id, err := strconv.Atoi(c.Params("teamID")); err == nil {
		teamID := models.TeamID(id)
		t.TeamID = &teamID
	}
	if id, err := strconv.Atoi(c.Params("sourceID")); err == nil {
		sourceID := models.SourceID(id)
		t.SourceID = &sourceID
	}
	if names := c.Route().Params; len(names) > 0 {
		last := names[len(names)-1]
		t.ID = c.Params(last)
		if last == "token" {
			t.ID = models.RedactShareToken(t.ID)
		}
	}
	return t
}
