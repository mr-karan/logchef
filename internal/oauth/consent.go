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
		Client:        s.consentClientInfo(ctx, req.ClientID),
		Instance:      s.issuer,
		Resource:      req.Resource,
		ResourceKind:  s.ResourceKind(req.Resource),
		Scopes:        scopes,
		OfflineAccess: req.OfflineAccess,
		RedirectURI:   req.RedirectURI,
		ExpiresAt:     req.ExpiresAt,
	}, nil
}

// consentClientInfo describes the client for the consent screen. A CIMD
// client's document is fetched again if it has left the cache, so the screen
// shows its current name.
func (s *Server) consentClientInfo(ctx context.Context, id models.OAuthClientID) ClientInfo {
	if c, err := s.lookupClient(ctx, id); err == nil {
		return c.info
	}
	return s.ClientInfo(id)
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

// Decide records user's decision and returns where the browser goes next:
// the client's redirect URI with a code (approve) or access_denied (deny),
// with state and iss in both cases.
func (s *Server) Decide(ctx context.Context, id models.OAuthAuthRequestID, user *models.User, approve bool) (string, error) {
	req, err := s.pendingRequest(ctx, id)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	if !approve {
		err := s.db.DenyOAuthAuthRequest(ctx, id, user.ID, now)
		if errors.Is(err, models.ErrNotFound) {
			return "", ErrRequestDecided
		}
		if err != nil {
			return "", err
		}
		return s.errorRedirectURL(req.RedirectURI, req.State, "access_denied", "The user denied the request"), nil
	}
	_, err = s.db.ApproveOAuthAuthRequest(ctx, id, user.ID, now)
	if errors.Is(err, models.ErrNotFound) {
		return "", ErrRequestDecided
	}
	if err != nil {
		return "", err
	}
	approved, err := s.db.GetOAuthAuthRequest(ctx, id)
	if err != nil {
		return "", err
	}
	location, err := op.BuildAuthResponseCallbackURL(ctx, &authRequest{req: approved}, s.provider)
	if err != nil {
		return "", err
	}
	return withIssuer(location, s.issuer), nil
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

// RevokeConnectedApp revokes one of the user's grants. Its access and refresh
// tokens stop working on their next use.
func (s *Server) RevokeConnectedApp(ctx context.Context, userID models.UserID, id models.OAuthGrantID) error {
	err := s.db.RevokeGrant(ctx, id, userID, time.Now().UTC())
	if errors.Is(err, models.ErrNotFound) {
		return ErrGrantNotFound
	}
	return err
}
