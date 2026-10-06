// Package oauth is Logchef's OAuth 2.1 authorization server. It wraps the
// ZITADEL op provider with Logchef policy: static public clients, S256-only
// PKCE, one fixed resource per client (RFC 8707), read-only scopes, RFC 9207
// issuer identification, and storage in the metadata store. It also
// authenticates the opaque access tokens it issues for the /api and /mcp
// resource servers.
package oauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// Lifetimes from planner section 3.6.
const (
	authRequestTTL  = 10 * time.Minute
	authCodeTTL     = 2 * time.Minute
	accessTokenTTL  = 10 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
)

// HKDF labels. Changing either one invalidates every stored hash or every
// issued token.
const (
	hashKeyLabel   = "logchef-oauth-v1"
	cryptoKeyLabel = "logchef-oauth-crypto-v1"
)

// Endpoint paths, relative to the issuer.
const (
	AuthorizePath = "/oauth/authorize"
	TokenPath     = "/oauth/token" //nolint:gosec // G101: an endpoint path, not a credential
	RevokePath    = "/oauth/revoke"
	// ConsentPath is the SPA page that shows a pending request.
	ConsentPath = "/oauth/consent"
	// AuthorizationServerMetadataPath is the RFC 8414 metadata document.
	AuthorizationServerMetadataPath = "/.well-known/oauth-authorization-server"
	// ProtectedResourceMetadataPath is the RFC 9728 document for /mcp.
	ProtectedResourceMetadataPath = "/.well-known/oauth-protected-resource/mcp"
)

// Resource is a protected resource that an access token is issued for.
type Resource int

const (
	// ResourceAPI is the REST API under /api, used by the CLI.
	ResourceAPI Resource = iota + 1
	// ResourceMCP is the MCP endpoint, used by MCP hosts.
	ResourceMCP
)

// Server is the authorization server. Build it with New.
type Server struct {
	issuer      string
	origin      string
	apiResource string
	mcpResource string
	db          store.Store
	clients     map[models.OAuthClientID]*client
	hashKey     models.OAuthHashKey
	crypto      op.Crypto
	provider    *op.Provider
	log         *slog.Logger
}

// New builds the authorization server from the validated configuration. The
// caller must only call it when cfg.Auth.OAuth.Enabled is true.
func New(cfg *config.Config, db store.Store, log *slog.Logger) (*Server, error) {
	issuer := cfg.Server.PublicURL
	u, err := url.Parse(issuer)
	if err != nil {
		return nil, fmt.Errorf("parsing server.public_url: %w", err)
	}
	hashKey, err := deriveKey(cfg.Auth.APITokenSecret, hashKeyLabel)
	if err != nil {
		return nil, err
	}
	cryptoKey, err := deriveKey(cfg.Auth.APITokenSecret, cryptoKeyLabel)
	if err != nil {
		return nil, err
	}
	signingKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating ID-token signing key: %w", err)
	}

	s := &Server{
		issuer:      issuer,
		origin:      u.Scheme + "://" + u.Host,
		apiResource: issuer + "/api",
		mcpResource: issuer + "/mcp",
		db:          db,
		clients:     newClients(cfg.Auth.OAuth, issuer),
		hashKey:     models.OAuthHashKey(hashKey),
		// AES-GCM only. ZITADEL's default crypto also decrypts the legacy,
		// unauthenticated AES-CFB format (dependency audit F3).
		crypto: op.NewAES256GCMCrypto(cryptoKey, ""),
		log:    log.With("component", "oauth"),
	}

	opts := []op.Option{
		op.WithCrypto(s.crypto),
		op.WithCustomAuthEndpoint(op.NewEndpoint(AuthorizePath)),
		op.WithCustomTokenEndpoint(op.NewEndpoint(TokenPath)),
		op.WithCustomRevocationEndpoint(op.NewEndpoint(RevokePath)),
	}
	if u.Scheme == "http" {
		// Config validation allows http only for a loopback public_url.
		opts = append(opts, op.WithAllowInsecure())
	}
	provider, err := op.NewProvider(&op.Config{
		CryptoKey:             cryptoKey,
		CodeMethodS256:        true,
		GrantTypeRefreshToken: true,
		SupportedScopes:       scopeStrings(ReadScopes, true),
	}, &storage{server: s, signingKey: signingKey}, op.StaticIssuer(issuer), opts...)
	if err != nil {
		return nil, fmt.Errorf("creating OAuth provider: %w", err)
	}
	s.provider = provider
	return s, nil
}

// Issuer returns the issuer identifier, which is server.public_url.
func (s *Server) Issuer() string { return s.issuer }

// Origin returns the scheme and host of the issuer. Session-only routes that
// change state require the browser Origin header to equal it.
func (s *Server) Origin() string { return s.origin }

func (s *Server) resourceURL(r Resource) string {
	if r == ResourceMCP {
		return s.mcpResource
	}
	return s.apiResource
}

// ResourceKind names a resource identifier for display: "api" or "mcp".
func (s *Server) ResourceKind(resource string) string {
	switch resource {
	case s.apiResource:
		return "api"
	case s.mcpResource:
		return "mcp"
	}
	return "unknown"
}

// ClientInfo describes a client, including one removed from config.
func (s *Server) ClientInfo(id models.OAuthClientID) ClientInfo {
	if c, ok := s.clients[id]; ok {
		return c.info
	}
	return ClientInfo{ID: id, Name: string(id), Kind: ClientUnknown}
}

func (s *Server) hash(secret string) models.OAuthSecretHash {
	return models.HashOAuthSecret(s.hashKey, secret)
}

func deriveKey(secret, label string) ([32]byte, error) {
	key, err := hkdf.Key(sha256.New, []byte(secret), nil, label, 32)
	if err != nil {
		return [32]byte{}, fmt.Errorf("deriving OAuth key: %w", err)
	}
	return [32]byte(key), nil
}

// randomID returns 256 random bits, base64url encoded (43 characters).
func randomID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
