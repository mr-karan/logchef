package oauth

import (
	"context"
	"net/http"
	"strconv"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/pkg/models"
)

// recordClientRevoke records a client's revocation through /oauth/revoke.
// The actor is the grant's user, authenticated by the token it revoked.
func (s *Server) recordClientRevoke(ctx context.Context, grant *models.OAuthGrant) {
	method := models.AuditAuthOAuthAPI
	if grant.Resource == s.mcpResource {
		method = models.AuditAuthOAuthMCP
	}
	actor := audit.Actor{
		Type:         models.AuditActorUser,
		ID:           &grant.UserID,
		AuthMethod:   method,
		CredentialID: strconv.Itoa(int(grant.ID)),
		ClientID:     string(grant.ClientID),
	}
	if user, err := s.db.GetUser(ctx, grant.UserID); err == nil {
		actor.Label = user.Email
	} else {
		s.log.Warn("audit: grant owner not found, recording the revocation without a label", "grant_id", grant.ID, "error", err)
	}
	req := audit.RequestFrom(ctx)
	req.Status = http.StatusOK
	s.recordRevoke(ctx, grant, actor, req)
}

// recordReplay records the revocation that a replayed code or refresh token
// caused. Logchef revoked the grant, not the caller, so the actor is the
// system and, as for every system event, the IP is empty. grant is nil when
// the call revoked nothing, which records nothing.
func (s *Server) recordReplay(ctx context.Context, grant *models.OAuthGrant, status int) {
	if grant == nil {
		return
	}
	actor := audit.Actor{
		Type:       models.AuditActorSystem,
		Label:      "oauth",
		AuthMethod: models.AuditAuthNone,
		ClientID:   string(grant.ClientID),
	}
	req := audit.RequestFrom(ctx)
	req.IP, req.Status = "", status
	s.recordRevoke(ctx, grant, actor, req)
}

func (s *Server) recordRevoke(ctx context.Context, grant *models.OAuthGrant, actor audit.Actor, req audit.Request) {
	s.audit.Record(ctx, audit.Event{
		Action:  audit.OAuthGrantRevoke,
		Outcome: models.AuditOutcomeSuccess,
		Actor:   actor,
		Target:  audit.OAuthGrantTarget(grant.ID),
		Request: req,
		Before:  audit.OAuthGrantFields(grant, s.ResourceKind(grant.Resource)),
		Detail:  audit.NewFields().Set("cause", grant.RevokeReason),
	})
}
