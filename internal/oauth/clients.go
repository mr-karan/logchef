package oauth

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

// ClientKind is how a client runs. It decides the redirect policy and the
// resource the client may request.
type ClientKind string

const (
	// ClientNative is a built-in client that redirects to a loopback
	// listener: the CLI (API resource) or a local MCP host (MCP resource).
	ClientNative ClientKind = "native"
	// ClientWeb is an operator-configured hosted client such as ChatGPT. It
	// redirects to exact registered URIs and may only obtain tokens for the
	// MCP resource.
	ClientWeb ClientKind = "web"
	// ClientCIMD is an MCP host identified by a Client ID Metadata Document
	// URL. It redirects to the compliant URIs in its document and may only
	// obtain tokens for the MCP resource.
	ClientCIMD ClientKind = "cimd"
	// ClientUnknown describes a grant whose client is no longer configured.
	ClientUnknown ClientKind = "unknown"
)

// ClientInfo is what the consent and Connected-apps screens show about a
// client.
type ClientInfo struct {
	ID   models.OAuthClientID `json:"id"`
	Name string               `json:"name"`
	Kind ClientKind           `json:"kind"`
	// Host is the host of a CIMD client's metadata URL. The name is
	// self-declared; the host is what proves who published it.
	Host string `json:"host,omitempty"`
}

// client is a statically registered public client. It implements op.Client.
type client struct {
	info         ClientInfo
	redirectURIs []string
	resource     string
	consentURL   string
}

// nativeRedirectURIs are the registered loopback forms for native clients.
// ZITADEL matches a loopback redirect on path and query only, so any port
// passes its check; redirectAllowed applies the stricter policy first.
var nativeRedirectURIs = []string{"http://127.0.0.1/callback", "http://[::1]/callback", "http://localhost/callback"}

func newClients(cfg config.OAuthConfig, issuer string) map[models.OAuthClientID]*client {
	consentURL := issuer + "/oauth/consent?request="
	clients := map[models.OAuthClientID]*client{
		config.OAuthCLIClientID: {
			info:         ClientInfo{ID: config.OAuthCLIClientID, Name: "Logchef CLI", Kind: ClientNative},
			redirectURIs: nativeRedirectURIs,
			resource:     issuer + "/api",
			consentURL:   consentURL,
		},
		config.OAuthMCPClientID: {
			info:         ClientInfo{ID: config.OAuthMCPClientID, Name: "Local MCP client", Kind: ClientNative},
			redirectURIs: nativeRedirectURIs,
			resource:     issuer + "/mcp",
			consentURL:   consentURL,
		},
	}
	for _, c := range cfg.Clients {
		id := models.OAuthClientID(c.ID)
		clients[id] = &client{
			info:         ClientInfo{ID: id, Name: c.Name, Kind: ClientWeb},
			redirectURIs: slices.Clone(c.RedirectURIs),
			resource:     issuer + "/mcp",
			consentURL:   consentURL,
		}
	}
	return clients
}

var errUnknownClient = errors.New("oauth: unknown client")

// clientKey carries the client that the boundary resolved for this request,
// so ZITADEL's lookups see the same client without another fetch.
type clientKey struct{}

func withClient(ctx context.Context, c *client) context.Context {
	return context.WithValue(ctx, clientKey{}, c)
}

// lookupClient finds a client: built-in and configured clients first, then a
// Client ID Metadata Document when CIMD is enabled.
func (s *Server) lookupClient(ctx context.Context, id models.OAuthClientID) (*client, error) {
	if c, ok := s.clients[id]; ok {
		return c, nil
	}
	if c, ok := ctx.Value(clientKey{}).(*client); ok && c.info.ID == id {
		return c, nil
	}
	if s.cimd == nil || !isCIMDClientID(string(id)) {
		return nil, errUnknownClient
	}
	return s.cimd.resolve(ctx, string(id))
}

// redirectAllowed applies Logchef's redirect policy. Web clients need an
// exact match. CIMD clients need an exact match, except that a loopback URI
// registered without a port matches any port (RFC 8252 section 7.3).
func (c *client) redirectAllowed(raw string) bool {
	switch c.info.Kind {
	case ClientWeb:
		return slices.Contains(c.redirectURIs, raw)
	case ClientCIMD:
		return slices.ContainsFunc(c.redirectURIs, func(registered string) bool {
			return registered == raw || loopbackAnyPortMatch(registered, raw)
		})
	case ClientNative:
		return nativeRedirectAllowed(raw)
	case ClientUnknown:
	}
	return false
}

// nativeRedirectAllowed follows RFC 8252 section 7.3, more strictly than
// ZITADEL: scheme http, host literally 127.0.0.1, [::1] or localhost, any
// port, path exactly /callback, and no user info, query or fragment.
// localhost is allowed because Claude Code and Cursor desktop redirect there.
func nativeRedirectAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !strings.HasPrefix(raw, "http://") || strings.Contains(raw, "#") || u.User != nil ||
		u.Path != "/callback" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery {
		return false
	}
	host := u.Host
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
		host = strings.TrimSuffix(host, ":"+port)
	}
	if host != "127.0.0.1" && host != "[::1]" && host != "localhost" {
		return false
	}
	return true
}

// loopbackAnyPortMatch reports whether raw is registered with a port added:
// registered is a loopback http URI without a port, and raw equals it once
// raw's port is removed.
func loopbackAnyPortMatch(registered, raw string) bool {
	reg, err := url.Parse(registered)
	if err != nil || reg.Scheme != "http" || reg.Port() != "" || !isLoopbackHostname(reg.Hostname()) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil {
		return false
	}
	port := u.Port()
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return false
	}
	prefix := "http://" + u.Host
	if !strings.HasPrefix(raw, prefix) {
		return false
	}
	return "http://"+strings.TrimSuffix(u.Host, ":"+port)+raw[len(prefix):] == registered
}

func (c *client) GetID() string                    { return string(c.info.ID) }
func (c *client) RedirectURIs() []string           { return c.redirectURIs }
func (c *client) PostLogoutRedirectURIs() []string { return nil }

// ApplicationType is native for CIMD clients so that ZITADEL accepts their
// loopback redirects on any port. redirectAllowed has already applied the
// stricter policy.
func (c *client) ApplicationType() op.ApplicationType {
	if c.info.Kind == ClientWeb {
		return op.ApplicationTypeUserAgent
	}
	return op.ApplicationTypeNative
}
func (c *client) AuthMethod() oidc.AuthMethod { return oidc.AuthMethodNone }
func (c *client) ResponseTypes() []oidc.ResponseType {
	return []oidc.ResponseType{oidc.ResponseTypeCode}
}
func (c *client) GrantTypes() []oidc.GrantType {
	return []oidc.GrantType{oidc.GrantTypeCode, oidc.GrantTypeRefreshToken}
}
func (c *client) LoginURL(id string) string           { return c.consentURL + url.QueryEscape(id) }
func (c *client) AccessTokenType() op.AccessTokenType { return op.AccessTokenTypeBearer }
func (c *client) IDTokenLifetime() time.Duration      { return accessTokenTTL }
func (c *client) DevMode() bool                       { return false }

// RestrictAdditionalIdTokenScopes keeps user claims out of the ID token that
// ZITADEL always emits. Logchef does not offer OpenID Connect.
func (c *client) RestrictAdditionalIdTokenScopes() func([]string) []string { //nolint:revive // name fixed by op.Client
	return func([]string) []string { return nil }
}
func (c *client) RestrictAdditionalAccessTokenScopes() func([]string) []string {
	return slices.Clone[[]string]
}
func (c *client) IsScopeAllowed(scope string) bool {
	return slices.Contains(ReadScopes, models.TokenScope(scope))
}
func (c *client) IDTokenUserinfoClaimsAssertion() bool { return false }
func (c *client) ClockSkew() time.Duration             { return 0 }
