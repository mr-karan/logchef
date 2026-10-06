package oauth

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/pkg/models"
)

// ErrInvalidAccessToken means the bearer value is not a live Logchef OAuth
// access token for the requested resource. Resource servers answer 401
// invalid_token.
var ErrInvalidAccessToken = errors.New("invalid or expired access token")

// grantTouchInterval limits last_used_at writes to one per grant per minute.
const grantTouchInterval = time.Minute

// AccessToken is an authenticated OAuth access token.
type AccessToken struct {
	Principal access.Principal
	ExpiresAt time.Time
}

// AuthenticateAccessToken checks an opaque access token for resource and
// builds the caller's principal. Every call reads the database, so a revoked
// grant, a deactivated user or a token for the other resource fails on the
// next request. ID tokens, refresh tokens and tokens in the legacy AES-CFB
// format are rejected because they do not decrypt with AES-GCM.
func (s *Server) AuthenticateAccessToken(ctx context.Context, token string, resource Resource) (*AccessToken, error) {
	plain, err := s.crypto.Decrypt(token)
	if err != nil {
		return nil, ErrInvalidAccessToken
	}
	tokenID, subject, ok := strings.Cut(plain, ":")
	if !ok || strings.Contains(subject, ":") {
		return nil, ErrInvalidAccessToken
	}
	scopes, expires, ok := parseAccessTokenID(tokenID)
	now := time.Now().UTC()
	if !ok || !expires.After(now) {
		return nil, ErrInvalidAccessToken
	}
	grant, user, err := s.db.AuthenticateOAuthAccessToken(ctx, s.hash(tokenID), s.resourceURL(resource), now)
	if errors.Is(err, models.ErrNotFound) {
		return nil, ErrInvalidAccessToken
	}
	if err != nil {
		return nil, err
	}
	if subject != strconv.Itoa(int(user.ID)) {
		return nil, ErrInvalidAccessToken
	}
	scopes = slices.DeleteFunc(scopes, func(scope models.TokenScope) bool {
		return !slices.Contains(grant.Scopes, scope)
	})
	if grant.LastUsedAt == nil || now.Sub(*grant.LastUsedAt) >= grantTouchInterval {
		if err := s.db.TouchOAuthGrant(ctx, grant.ID, now); err != nil {
			s.log.Warn("recording OAuth grant use", "error", err, "grant_id", grant.ID)
		}
	}
	return &AccessToken{
		Principal: access.OAuthPrincipal(user, grant.ID, grant.ClientID, scopes),
		ExpiresAt: expires,
	}, nil
}
