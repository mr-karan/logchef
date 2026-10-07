package oauth

import (
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
	// ClientUnknown describes a grant whose client is no longer configured.
	ClientUnknown ClientKind = "unknown"
)

// ClientInfo is what the consent and Connected-apps screens show about a
// client.
type ClientInfo struct {
	ID   models.OAuthClientID `json:"id"`
	Name string               `json:"name"`
	Kind ClientKind           `json:"kind"`
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

// newClients builds the static clients. consentURL is the browser consent
// page, ending in "?request=".
func newClients(cfg config.OAuthConfig, issuer, consentURL string) map[models.OAuthClientID]*client {
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

// redirectAllowed applies Logchef's redirect policy. Web clients need an
// exact match. Native clients follow RFC 8252 section 7.3, more strictly than
// ZITADEL: scheme http, host literally 127.0.0.1, [::1] or localhost, any
// port, path exactly /callback, and no user info, query or fragment.
// localhost is allowed because Claude Code and Cursor desktop redirect there.
func (c *client) redirectAllowed(raw string) bool {
	if c.info.Kind == ClientWeb {
		return slices.Contains(c.redirectURIs, raw)
	}
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

func (c *client) GetID() string                    { return string(c.info.ID) }
func (c *client) RedirectURIs() []string           { return c.redirectURIs }
func (c *client) PostLogoutRedirectURIs() []string { return nil }
func (c *client) ApplicationType() op.ApplicationType {
	if c.info.Kind == ClientNative {
		return op.ApplicationTypeNative
	}
	return op.ApplicationTypeUserAgent
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
