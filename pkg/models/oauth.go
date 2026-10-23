package models

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// OAuthGrantID identifies one completed consent (a "connected app"). A grant
// is also the refresh-token family: revoking it invalidates every token issued
// under it.
type OAuthGrantID int

// OAuthClientID identifies a statically registered OAuth client.
type OAuthClientID string

// OAuthAuthRequestID identifies a pending authorization request. It is a
// random 256-bit value chosen by the authorization server.
type OAuthAuthRequestID string

// OAuthHashKey is the HMAC key for OAuth codes and tokens. It is derived from
// the server secret by the caller; storage never sees the secret.
type OAuthHashKey [32]byte

// OAuthSecretHash is the stored form of an authorization code, access-token
// ID or refresh token. It can only be built by HashOAuthSecret, so a plaintext
// value cannot reach storage by accident.
type OAuthSecretHash struct{ hex string }

// HashOAuthSecret returns the HMAC-SHA256 of secret under key.
func HashOAuthSecret(key OAuthHashKey, secret string) OAuthSecretHash {
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(secret))
	return OAuthSecretHash{hex: hex.EncodeToString(mac.Sum(nil))}
}

// Hex returns the hex digest that storage persists and compares.
func (h OAuthSecretHash) Hex() string { return h.hex }

// OAuthRevokeReason records why a grant was revoked.
type OAuthRevokeReason string

const (
	// OAuthRevokeUser is a revocation from the Connected-apps UI.
	OAuthRevokeUser OAuthRevokeReason = "user_revoked"
	// OAuthRevokeTokenEndpoint is a revocation through /oauth/revoke.
	OAuthRevokeTokenEndpoint OAuthRevokeReason = "token_revoked"
	// OAuthRevokeCodeReplay is set when a consumed authorization code is
	// presented again.
	OAuthRevokeCodeReplay OAuthRevokeReason = "code_replay"
	// OAuthRevokeRefreshReplay is set when a consumed refresh token is
	// presented again.
	OAuthRevokeRefreshReplay OAuthRevokeReason = "refresh_replay"
)

var (
	// ErrOAuthInvalidGrant means a code or refresh token cannot be
	// used: it is unknown, expired, already used, or its grant is revoked.
	// The token endpoint maps it to invalid_grant.
	ErrOAuthInvalidGrant = errors.New("oauth: invalid grant")
	// ErrOAuthCodeReplay is a consumed authorization code presented again.
	// Storage has already revoked and committed the grant. It wraps
	// ErrOAuthInvalidGrant.
	ErrOAuthCodeReplay = fmt.Errorf("%w: authorization code replay", ErrOAuthInvalidGrant)
)

// OAuthGrant is one user's consent for one client and one resource.
type OAuthGrant struct {
	ID            OAuthGrantID
	UserID        UserID
	ClientID      OAuthClientID
	Resource      string
	Scopes        []TokenScope
	OfflineAccess bool
	CreatedAt     time.Time
	LastUsedAt    *time.Time
	RevokedAt     *time.Time
	RevokeReason  OAuthRevokeReason
}

// OAuthAuthRequest is a pending or decided authorization-code request.
// UserID, GrantID and DecidedAt are set once the user decides. GrantID is set
// only on approval.
type OAuthAuthRequest struct {
	ID            OAuthAuthRequestID
	ClientID      OAuthClientID
	RedirectURI   string
	Resource      string
	Scopes        []TokenScope
	OfflineAccess bool
	CodeChallenge string
	State         string
	UserID        *UserID
	GrantID       *OAuthGrantID
	DecidedAt     *time.Time
	Denied        bool
	ExpiresAt     time.Time
	CreatedAt     time.Time
}

// OAuthTokenIssue holds the hashes and expiries for a new access token and,
// when the grant has offline access, a new refresh token.
type OAuthTokenIssue struct {
	AccessIDHash     OAuthSecretHash
	AccessExpiresAt  time.Time
	RefreshHash      *OAuthSecretHash
	RefreshExpiresAt time.Time
}

// RefreshOutcomeKind discriminates RefreshOutcome.
type RefreshOutcomeKind int

const (
	// RefreshOK: the token is valid. After RotateRefreshToken it is also
	// consumed and the new tokens are stored.
	RefreshOK RefreshOutcomeKind = iota + 1
	// RefreshReplay: the old token was already consumed. Storage revoked the
	// grant and committed the revocation. Map to invalid_grant.
	RefreshReplay
	// RefreshInvalid: unknown, expired, or the grant is revoked. Map to
	// invalid_grant.
	RefreshInvalid
)

// RefreshOutcome is the result of a refresh-token check or rotation. Grant is
// set only when Kind is RefreshOK.
type RefreshOutcome struct {
	Kind  RefreshOutcomeKind
	Grant OAuthGrant
}
