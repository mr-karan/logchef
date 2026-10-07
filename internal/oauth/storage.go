package oauth

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/mr-karan/logchef/pkg/models"
)

// resourceKey carries the resource that the boundary validated for this
// request. ZITADEL does not know about RFC 8707, so storage reads it from the
// context.
type resourceKey struct{}

func withResource(ctx context.Context, resource string) context.Context {
	return context.WithValue(ctx, resourceKey{}, resource)
}

func resourceFrom(ctx context.Context) (string, error) {
	resource, ok := ctx.Value(resourceKey{}).(string)
	if !ok || resource == "" {
		return "", errors.New("oauth: request did not pass the resource boundary")
	}
	return resource, nil
}

// storage implements op.Storage on the metadata store. Codes and tokens are
// stored only as HMACs.
type storage struct {
	server     *Server
	signingKey *ecdsa.PrivateKey
}

var _ op.Storage = (*storage)(nil)

// authRequest adapts a stored request to op.AuthRequest. code is set only
// when the request was loaded by its authorization code.
type authRequest struct {
	req  *models.OAuthAuthRequest
	code string
}

func (a *authRequest) GetID() string         { return string(a.req.ID) }
func (a *authRequest) GetACR() string        { return "" }
func (a *authRequest) GetAMR() []string      { return nil }
func (a *authRequest) GetAudience() []string { return []string{a.req.Resource} }
func (a *authRequest) GetAuthTime() time.Time {
	if a.req.DecidedAt != nil {
		return *a.req.DecidedAt
	}
	return a.req.CreatedAt
}
func (a *authRequest) GetClientID() string { return string(a.req.ClientID) }
func (a *authRequest) GetCodeChallenge() *oidc.CodeChallenge {
	return &oidc.CodeChallenge{Challenge: a.req.CodeChallenge, Method: oidc.CodeChallengeMethodS256}
}
func (a *authRequest) GetNonce() string                   { return "" }
func (a *authRequest) GetRedirectURI() string             { return a.req.RedirectURI }
func (a *authRequest) GetResponseType() oidc.ResponseType { return oidc.ResponseTypeCode }
func (a *authRequest) GetResponseMode() oidc.ResponseMode { return oidc.ResponseModeQuery }
func (a *authRequest) GetScopes() []string                { return scopeStrings(a.req.Scopes, a.req.OfflineAccess) }
func (a *authRequest) GetState() string                   { return a.req.State }
func (a *authRequest) GetSubject() string {
	if a.req.UserID == nil {
		return ""
	}
	return strconv.Itoa(int(*a.req.UserID))
}
func (a *authRequest) Done() bool {
	return a.req.DecidedAt != nil && !a.req.Denied && a.req.GrantID != nil
}

// refreshRequest adapts a grant loaded by refresh token. It deliberately does
// not implement op.AuthRequest: ZITADEL uses the type to tell a refresh from
// a first issuance.
type refreshRequest struct {
	grant  *models.OAuthGrant
	scopes []string
}

func (r *refreshRequest) GetAMR() []string                 { return nil }
func (r *refreshRequest) GetAudience() []string            { return []string{r.grant.Resource} }
func (r *refreshRequest) GetAuthTime() time.Time           { return r.grant.CreatedAt }
func (r *refreshRequest) GetClientID() string              { return string(r.grant.ClientID) }
func (r *refreshRequest) GetScopes() []string              { return r.scopes }
func (r *refreshRequest) GetSubject() string               { return strconv.Itoa(int(r.grant.UserID)) }
func (r *refreshRequest) SetCurrentScopes(scopes []string) { r.scopes = slices.Clone(scopes) }

func (s *storage) CreateAuthRequest(ctx context.Context, req *oidc.AuthRequest, _ string) (op.AuthRequest, error) {
	resource, err := resourceFrom(ctx)
	if err != nil {
		return nil, err
	}
	scopes, offline, err := parseScopes(req.Scopes)
	if err != nil {
		return nil, oidc.ErrInvalidScope().WithParent(err)
	}
	now := time.Now().UTC()
	stored := &models.OAuthAuthRequest{
		ID:            models.OAuthAuthRequestID(randomID()),
		ClientID:      models.OAuthClientID(req.ClientID),
		RedirectURI:   req.RedirectURI,
		Resource:      resource,
		Scopes:        scopes,
		OfflineAccess: offline,
		CodeChallenge: req.CodeChallenge,
		State:         req.State,
		ExpiresAt:     now.Add(authRequestTTL),
		CreatedAt:     now,
	}
	if err := s.server.db.CreateOAuthAuthRequest(ctx, stored); err != nil {
		return nil, err
	}
	return &authRequest{req: stored}, nil
}

func (s *storage) AuthRequestByID(ctx context.Context, id string) (op.AuthRequest, error) {
	req, err := s.server.db.GetOAuthAuthRequest(ctx, models.OAuthAuthRequestID(id))
	if err != nil {
		return nil, err
	}
	if !req.ExpiresAt.After(time.Now()) {
		return nil, oidc.ErrInvalidRequest().WithDescription("authorization request expired")
	}
	return &authRequest{req: req}, nil
}

// AuthRequestByCode loads the request without consuming the code. ZITADEL
// checks the verifier, client and redirect after this call, so consuming here
// would let a wrong verifier burn a valid code. The code is consumed
// atomically when tokens are created.
func (s *storage) AuthRequestByCode(ctx context.Context, code string) (op.AuthRequest, error) {
	resource, err := resourceFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := s.server.crypto.Decrypt(code)
	if err != nil {
		return nil, oidc.ErrInvalidGrant()
	}
	req, err := s.server.db.GetOAuthAuthRequest(ctx, models.OAuthAuthRequestID(id))
	if err != nil {
		return nil, oidc.ErrInvalidGrant()
	}
	if req.GrantID == nil || req.UserID == nil {
		return nil, oidc.ErrInvalidGrant()
	}
	if req.Resource != resource {
		return nil, oidc.ErrInvalidTarget().WithDescription("resource does not match the authorization request")
	}
	return &authRequest{req: req, code: code}, nil
}

func (s *storage) SaveAuthCode(ctx context.Context, id, code string) error {
	expires := time.Now().UTC().Add(authCodeTTL)
	return s.server.db.SaveOAuthAuthCode(ctx, models.OAuthAuthRequestID(id), s.server.hash(code), expires)
}

// DeleteAuthRequest is a no-op. The consumed request row is what detects a
// replayed code; cleanup removes it after it expires.
func (s *storage) DeleteAuthRequest(context.Context, string) error { return nil }

func (s *storage) CreateAccessToken(ctx context.Context, req op.TokenRequest) (string, time.Time, error) {
	id, _, exp, err := s.issueFromCode(ctx, req, false)
	return id, exp, err
}

func (s *storage) CreateAccessAndRefreshTokens(ctx context.Context, req op.TokenRequest, currentRefreshToken string) (accessTokenID, refreshToken string, expiration time.Time, err error) {
	if currentRefreshToken == "" {
		return s.issueFromCode(ctx, req, true)
	}
	now := time.Now().UTC()
	tokenID, issue, refresh := s.newIssue(req.GetScopes(), now, true)
	outcome, err := s.server.db.RotateRefreshToken(ctx, s.server.hash(currentRefreshToken), issue, now)
	if err != nil {
		return "", "", time.Time{}, err
	}
	if outcome.Kind != models.RefreshOK {
		return "", "", time.Time{}, oidc.ErrInvalidGrant()
	}
	return tokenID, refresh, issue.AccessExpiresAt, nil
}

// issueFromCode consumes the authorization code once, then stores the new
// tokens under the request's grant. A replayed code has already revoked the
// grant inside ConsumeAuthCode.
func (s *storage) issueFromCode(ctx context.Context, req op.TokenRequest, withRefresh bool) (accessTokenID, refreshToken string, expiration time.Time, err error) {
	ar, ok := req.(*authRequest)
	if !ok || ar.code == "" {
		return "", "", time.Time{}, fmt.Errorf("oauth: unexpected token request %T", req)
	}
	now := time.Now().UTC()
	consumed, err := s.server.db.ConsumeAuthCode(ctx, s.server.hash(ar.code), now)
	if errors.Is(err, models.ErrOAuthInvalidGrant) {
		return "", "", time.Time{}, oidc.ErrInvalidGrant()
	}
	if err != nil {
		return "", "", time.Time{}, err
	}
	if consumed.GrantID == nil {
		return "", "", time.Time{}, oidc.ErrInvalidGrant()
	}
	tokenID, issue, refresh := s.newIssue(req.GetScopes(), now, withRefresh)
	err = s.server.db.IssueOAuthTokens(ctx, *consumed.GrantID, issue, now)
	if errors.Is(err, models.ErrOAuthInvalidGrant) {
		return "", "", time.Time{}, oidc.ErrInvalidGrant()
	}
	if err != nil {
		return "", "", time.Time{}, err
	}
	return tokenID, refresh, issue.AccessExpiresAt, nil
}

// newIssue creates the access-token ID and, when asked, a refresh token.
func (s *storage) newIssue(scopes []string, now time.Time, withRefresh bool) (accessTokenID string, issue models.OAuthTokenIssue, refreshToken string) {
	expires := now.Add(accessTokenTTL)
	accessTokenID = newAccessTokenID(scopes, expires)
	issue = models.OAuthTokenIssue{
		AccessIDHash:    s.server.hash(accessTokenID),
		AccessExpiresAt: expires,
	}
	if !withRefresh {
		return accessTokenID, issue, ""
	}
	refreshToken = randomID()
	refreshHash := s.server.hash(refreshToken)
	issue.RefreshHash = &refreshHash
	issue.RefreshExpiresAt = now.Add(refreshTokenTTL)
	return accessTokenID, issue, refreshToken
}

// newAccessTokenID returns "<random>.<scope mask>.<expiry unix>". ZITADEL
// encrypts it with AES-GCM as "<id>:<subject>". The stored HMAC covers the
// whole ID, so the scopes and expiry in it cannot be changed. This is how an
// access token carries scopes narrowed at refresh, which the grant row does
// not record.
func newAccessTokenID(scopes []string, expires time.Time) string {
	return randomID() + "." + strconv.FormatUint(uint64(scopeMask(scopes)), 10) + "." + strconv.FormatInt(expires.Unix(), 10)
}

func parseAccessTokenID(id string) ([]models.TokenScope, time.Time, bool) {
	parts := strings.Split(id, ".")
	if len(parts) != 3 || parts[0] == "" {
		return nil, time.Time{}, false
	}
	mask, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return nil, time.Time{}, false
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, time.Time{}, false
	}
	return scopesFromMask(uint(mask)), time.Unix(exp, 0).UTC(), true
}

func (s *storage) TokenRequestByRefreshToken(ctx context.Context, refreshToken string) (op.RefreshTokenRequest, error) {
	resource, err := resourceFrom(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := s.server.db.CheckRefreshToken(ctx, s.server.hash(refreshToken), time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if outcome.Kind != models.RefreshOK || outcome.Grant.Resource != resource {
		return nil, op.ErrInvalidRefreshToken
	}
	grant := outcome.Grant
	return &refreshRequest{grant: &grant, scopes: scopeStrings(grant.Scopes, grant.OfflineAccess)}, nil
}

// TerminateSession serves the end-session endpoint, which Logchef does not
// mount.
func (s *storage) TerminateSession(context.Context, string, string) error {
	return oidc.ErrRequestNotSupported()
}

// RevokeToken revokes the grant behind an access-token ID or a refresh token.
// An unknown token is not an error (RFC 7009 section 2.2).
func (s *storage) RevokeToken(ctx context.Context, tokenOrTokenID, _, clientID string) *oidc.Error {
	err := s.server.db.RevokeGrantByToken(ctx, s.server.hash(tokenOrTokenID), models.OAuthClientID(clientID), time.Now().UTC())
	if err != nil {
		return oidc.ErrServerError().WithParent(err)
	}
	return nil
}

func (s *storage) GetRefreshTokenInfo(ctx context.Context, clientID, token string) (userID, tokenID string, err error) {
	outcome, err := s.server.db.CheckRefreshToken(ctx, s.server.hash(token), time.Now().UTC())
	if err != nil {
		return "", "", err
	}
	if outcome.Kind != models.RefreshOK || string(outcome.Grant.ClientID) != clientID {
		return "", "", op.ErrInvalidRefreshToken
	}
	return strconv.Itoa(int(outcome.Grant.UserID)), token, nil
}

const signingKeyID = "logchef-oauth"

type signingKey struct{ key *ecdsa.PrivateKey }

func (k signingKey) SignatureAlgorithm() jose.SignatureAlgorithm { return jose.ES256 }
func (k signingKey) Key() any                                    { return k.key }
func (k signingKey) ID() string                                  { return signingKeyID }

type publicKey struct{ key *ecdsa.PublicKey }

func (k publicKey) Algorithm() jose.SignatureAlgorithm { return jose.ES256 }
func (k publicKey) Key() any                           { return k.key }
func (k publicKey) ID() string                         { return signingKeyID }
func (k publicKey) Use() string                        { return "sig" }

// SigningKey signs the ID tokens that ZITADEL emits with every token
// response. The key lives only in this process: no resource server accepts
// ID tokens, so nothing needs to verify them across restarts or replicas.
func (s *storage) SigningKey(context.Context) (op.SigningKey, error) {
	return signingKey{s.signingKey}, nil
}
func (s *storage) SignatureAlgorithms(context.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.ES256}, nil
}
func (s *storage) KeySet(context.Context) ([]op.Key, error) {
	return []op.Key{publicKey{&s.signingKey.PublicKey}}, nil
}
func (s *storage) Health(context.Context) error { return nil }

func (s *storage) GetClientByClientID(_ context.Context, id string) (op.Client, error) {
	c, ok := s.server.clients[models.OAuthClientID(id)]
	if !ok {
		return nil, oidc.ErrInvalidClient()
	}
	return c, nil
}

// AuthorizeClientIDSecret always fails: every client is public.
func (s *storage) AuthorizeClientIDSecret(context.Context, string, string) error {
	return oidc.ErrInvalidClient()
}

func (s *storage) SetUserinfoFromScopes(context.Context, *oidc.UserInfo, string, string, []string) error {
	return nil
}
func (s *storage) SetUserinfoFromToken(context.Context, *oidc.UserInfo, string, string, string) error {
	return oidc.ErrRequestNotSupported()
}
func (s *storage) SetIntrospectionFromToken(context.Context, *oidc.IntrospectionResponse, string, string, string) error {
	return oidc.ErrRequestNotSupported()
}
func (s *storage) GetPrivateClaimsFromScopes(context.Context, string, string, []string) (map[string]any, error) {
	return nil, nil
}
func (s *storage) GetKeyByIDAndClientID(context.Context, string, string) (*jose.JSONWebKey, error) {
	return nil, oidc.ErrInvalidClient()
}
func (s *storage) ValidateJWTProfileScopes(context.Context, string, []string) ([]string, error) {
	return nil, oidc.ErrUnsupportedGrantType()
}
