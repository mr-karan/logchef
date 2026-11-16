package oauth

import (
	"context"
	"errors"
	"time"

	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrRequestNotFound means the authorization request is unknown, expired,
	// or was decided by another user.
	ErrRequestNotFound = errors.New("authorization request not found or expired")
	// ErrRequestDecided means the request already has a decision.
	ErrRequestDecided = errors.New("authorization request already decided")
	// ErrGrantNotFound means the grant is not the user's or is already revoked.
	ErrGrantNotFound = errors.New("connected app not found")
)

// ScopeInfo is one scope with the text the consent screen shows.
type ScopeInfo struct {
	Scope       models.TokenScope `json:"scope"`
	Description string            `json:"description"`
}

// ConsentRequest is what the consent screen shows for a pending request.
type ConsentRequest struct {
	ID            models.OAuthAuthRequestID `json:"id"`
	Client        ClientInfo                `json:"client"`
	Instance      string                    `json:"instance"`
	Resource      string                    `json:"resource"`
	ResourceKind  string                    `json:"resource_kind"`
	Scopes        []ScopeInfo               `json:"scopes"`
	OfflineAccess bool                      `json:"offline_access"`
	RedirectURI   string                    `json:"redirect_uri"`
	ExpiresAt     time.Time                 `json:"expires_at"`
}

// ConsentRequest returns a pending, unexpired request for the consent screen.
func (s *Server) ConsentRequest(ctx context.Context, id models.OAuthAuthRequestID) (*ConsentRequest, error) {
	req, err := s.pendingRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	scopes := make([]ScopeInfo, 0, len(req.Scopes))
	for _, scope := range req.Scopes {
		scopes = append(scopes, ScopeInfo{Scope: scope, Description: scopeDescriptions[scope]})
	}
	return &ConsentRequest{
		ID:            req.ID,
		Client:        s.ClientInfo(req.ClientID),
		Instance:      s.browserURL,
		Resource:      req.Resource,
		ResourceKind:  s.ResourceKind(req.Resource),
		Scopes:        scopes,
		OfflineAccess: req.OfflineAccess,
		RedirectURI:   req.RedirectURI,
		ExpiresAt:     req.ExpiresAt,
	}, nil
}

func (s *Server) pendingRequest(ctx context.Context, id models.OAuthAuthRequestID) (*models.OAuthAuthRequest, error) {
	req, err := s.db.GetOAuthAuthRequest(ctx, id)
	if errors.Is(err, models.ErrNotFound) {
		return nil, ErrRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	if !req.ExpiresAt.After(time.Now()) {
		return nil, ErrRequestNotFound
	}
	if req.DecidedAt != nil {
		return nil, ErrRequestDecided
	}
	return req, nil
}

// Decision is a recorded consent decision.
type Decision struct {
	// RedirectURL is where the browser goes next: the client's redirect URI
	// with a code (approve) or access_denied (deny), with state and iss in
	// both cases.
	RedirectURL string
	// Request is the request as it was decided.
	Request *models.OAuthAuthRequest
	// Grant is the grant an approval created; nil for a denial.
	Grant *models.OAuthGrant
}

// Decide records user's decision on a pending request.
func (s *Server) Decide(ctx context.Context, id models.OAuthAuthRequestID, user *models.User, approve bool) (*Decision, error) {
	req, err := s.pendingRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if !approve {
		err := s.db.DenyOAuthAuthRequest(ctx, id, user.ID, now)
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrRequestDecided
		}
		if err != nil {
			return nil, err
		}
		return &Decision{
			RedirectURL: s.errorRedirectURL(req.RedirectURI, req.State, "access_denied", "The user denied the request"),
			Request:     req,
		}, nil
	}
	grant, err := s.db.ApproveOAuthAuthRequest(ctx, id, user.ID, now)
	if errors.Is(err, models.ErrNotFound) {
		return nil, ErrRequestDecided
	}
	if err != nil {
		return nil, err
	}
	approved, err := s.db.GetOAuthAuthRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	location, err := op.BuildAuthResponseCallbackURL(ctx, &authRequest{req: approved}, s.provider)
	if err != nil {
		return nil, err
	}
	return &Decision{RedirectURL: withIssuer(location, s.issuer), Request: approved, Grant: grant}, nil
}

// ConnectedApp is one active grant, as the Connected-apps screen shows it.
type ConnectedApp struct {
	ID            models.OAuthGrantID `json:"id"`
	Client        ClientInfo          `json:"client"`
	Resource      string              `json:"resource"`
	ResourceKind  string              `json:"resource_kind"`
	Scopes        []models.TokenScope `json:"scopes"`
	OfflineAccess bool                `json:"offline_access"`
	CreatedAt     time.Time           `json:"created_at"`
	LastUsedAt    *time.Time          `json:"last_used_at"`
}

// ConnectedApps lists the user's active grants, newest first.
func (s *Server) ConnectedApps(ctx context.Context, userID models.UserID) ([]ConnectedApp, error) {
	grants, err := s.db.ListGrantsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	apps := make([]ConnectedApp, 0, len(grants))
	for _, g := range grants {
		apps = append(apps, ConnectedApp{
			ID:            g.ID,
			Client:        s.ClientInfo(g.ClientID),
			Resource:      g.Resource,
			ResourceKind:  s.ResourceKind(g.Resource),
			Scopes:        g.Scopes,
			OfflineAccess: g.OfflineAccess,
			CreatedAt:     g.CreatedAt,
			LastUsedAt:    g.LastUsedAt,
		})
	}
	return apps, nil
}

// RevokeConnectedApp revokes one of the user's grants and returns it. Its
// access and refresh tokens stop working on their next use.
func (s *Server) RevokeConnectedApp(ctx context.Context, userID models.UserID, id models.OAuthGrantID) (*models.OAuthGrant, error) {
	grant, err := s.db.RevokeGrant(ctx, id, userID, time.Now().UTC())
	if errors.Is(err, models.ErrNotFound) {
		return nil, ErrGrantNotFound
	}
	return grant, err
}
