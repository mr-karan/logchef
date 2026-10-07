package oauth

// AuthorizationServerMetadata is the RFC 8414 document. Logchef serves its own
// instead of ZITADEL's discovery (adapter A3), so it advertises only what is
// implemented: no OpenID Connect fields, no registration, no JWKS.
type AuthorizationServerMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	RevocationEndpoint                         string   `json:"revocation_endpoint"`
	ResponseTypesSupported                     []string `json:"response_types_supported"`
	ResponseModesSupported                     []string `json:"response_modes_supported"`
	GrantTypesSupported                        []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported     []string `json:"revocation_endpoint_auth_methods_supported"`
	ScopesSupported                            []string `json:"scopes_supported"`
	AuthorizationResponseIssParameterSupported bool     `json:"authorization_response_iss_parameter_supported"`
}

// ProtectedResourceMetadata is the RFC 9728 document for the MCP resource.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// Metadata returns the authorization server metadata.
func (s *Server) Metadata() AuthorizationServerMetadata {
	return AuthorizationServerMetadata{
		Issuer:                                     s.issuer,
		AuthorizationEndpoint:                      s.browserURL + AuthorizePath,
		TokenEndpoint:                              s.issuer + TokenPath,
		RevocationEndpoint:                         s.issuer + RevokePath,
		ResponseTypesSupported:                     []string{"code"},
		ResponseModesSupported:                     []string{"query"},
		GrantTypesSupported:                        []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:              []string{"S256"},
		TokenEndpointAuthMethodsSupported:          []string{"none"},
		RevocationEndpointAuthMethodsSupported:     []string{"none"},
		ScopesSupported:                            scopeStrings(ReadScopes, true),
		AuthorizationResponseIssParameterSupported: true,
	}
}

// MCPResourceMetadata returns the protected resource metadata for /mcp. Its
// authorization server is the exact issuer string. offline_access is not a
// resource scope, so it is not listed.
func (s *Server) MCPResourceMetadata() ProtectedResourceMetadata {
	return ProtectedResourceMetadata{
		Resource:               s.mcpResource,
		AuthorizationServers:   []string{s.issuer},
		ScopesSupported:        scopeStrings(ReadScopes, false),
		BearerMethodsSupported: []string{"header"},
	}
}
