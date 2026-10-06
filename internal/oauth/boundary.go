package oauth

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/mr-karan/logchef/pkg/models"
)

// s256Challenge is a base64url SHA-256 digest without padding.
var s256Challenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// codeVerifier is the RFC 7636 section 4.1 verifier syntax.
var codeVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

// refreshTokenFormat matches the refresh tokens this server issues (randomID).
var refreshTokenFormat = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// unsupportedAuthorizeParams are OpenID Connect request parameters Logchef
// does not implement. id_token_hint in particular would make ZITADEL parse an
// attacker-supplied JWT, whose claim decoder can panic (audit F1).
var unsupportedAuthorizeParams = []string{"id_token_hint", "claims", "registration"}

// singleValued are the parameters that must not repeat (RFC 6749 section 3.1):
// every scalar that the boundary checks or that ZITADEL decodes on authorize,
// token and revoke. The boundary validates the first value and ZITADEL's
// decoder uses the last, so a repeated parameter would let the two see
// different requests.
var singleValued = []string{
	"client_id", "redirect_uri", "response_type", "response_mode", "scope", "state", "code_challenge", "code_challenge_method", "resource",
	"grant_type", "code", "code_verifier", "refresh_token", "token", "token_type_hint",
	"client_secret", "client_assertion", "client_assertion_type",
	"requested_token_type", "subject_token", "subject_token_type", "actor_token", "actor_token_type",
}

// Handler serves the OAuth endpoints: authorize, token and revoke. Every
// request passes Logchef's policy before it reaches the ZITADEL provider. No
// other ZITADEL route (discovery, userinfo, introspection, end_session, keys)
// is reachable.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+AuthorizePath, s.authorize)
	mux.HandleFunc("POST "+AuthorizePath, s.authorize)
	mux.HandleFunc("POST "+TokenPath, s.token)
	mux.HandleFunc("POST "+RevokePath, s.revoke)
	return mux
}

// authorize validates the request before ZITADEL stores it (adapters A1 and
// A4, plus S256-only PKCE and the native loopback policy). An unknown client
// or a redirect URI that fails policy gets an error page and never a
// redirect. Every other error redirects to the validated redirect URI with
// state and iss.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.errorPage(w, "The authorization request could not be read.")
		return
	}
	form := r.Form
	if repeated(form, "client_id", "redirect_uri") {
		s.errorPage(w, "The authorization request repeats client_id or redirect_uri.")
		return
	}
	c, err := s.lookupClient(r.Context(), models.OAuthClientID(form.Get("client_id")))
	if err != nil {
		s.clientErrorPage(w, form.Get("client_id"), err)
		return
	}
	redirectURI := form.Get("redirect_uri")
	if !c.redirectAllowed(redirectURI) {
		s.errorPage(w, "The application that sent you here asked to return to an address that is not registered for it.")
		return
	}
	state := form.Get("state")
	fail := func(code, description string) {
		// redirectURI passed the client's registered redirect policy above.
		http.Redirect(w, r, s.errorRedirectURL(redirectURI, state, code, description), http.StatusFound)
	}
	switch {
	case repeated(form, singleValued...):
		fail("invalid_request", "parameters must not be repeated")
	case form.Has("request") || form.Has("request_uri"):
		fail("request_not_supported", "request objects are not supported")
	case hasAny(form, unsupportedAuthorizeParams...):
		fail("invalid_request", "OpenID Connect request parameters are not supported")
	case form.Get("response_type") != "code":
		fail("unsupported_response_type", "response_type must be code")
	case form.Get("response_mode") != "" && form.Get("response_mode") != "query":
		fail("invalid_request", "response_mode must be query")
	case state == "":
		fail("invalid_request", "state is required")
	case form.Get("code_challenge_method") != "S256" || !s256Challenge.MatchString(form.Get("code_challenge")):
		fail("invalid_request", "PKCE with code_challenge_method S256 is required")
	case form.Get("resource") != c.resource:
		fail("invalid_target", "resource must be "+c.resource)
	default:
		if _, _, err := parseScopes(strings.Fields(form.Get("scope"))); err != nil {
			fail("invalid_scope", err.Error())
			return
		}
		iw := &issWriter{ResponseWriter: w, redirectURI: redirectURI, issuer: s.issuer}
		s.provider.ServeHTTP(iw, r.WithContext(withClient(withResource(r.Context(), c.resource), c)))
	}
}

// token validates the token request before ZITADEL handles it (adapter A1).
// Only public clients exist, so any client credential is refused. The
// resource must be present and equal the client's resource.
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		s.writeTokenError(w, http.StatusBadRequest, "invalid_request", "parameters must be sent in the request body")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeTokenError(w, http.StatusBadRequest, "invalid_request", "the request body could not be read")
		return
	}
	form := r.PostForm
	switch {
	case repeated(form, singleValued...):
		s.writeTokenError(w, http.StatusBadRequest, "invalid_request", "parameters must not be repeated")
		return
	case hasClientCredential(r, form):
		s.writeTokenError(w, http.StatusUnauthorized, "invalid_client", "only public clients are supported")
		return
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		if !codeVerifier.MatchString(form.Get("code_verifier")) {
			s.writeTokenError(w, http.StatusBadRequest, "invalid_request", "code_verifier must be 43 to 128 characters from [A-Za-z0-9._~-]")
			return
		}
	case "refresh_token":
	default:
		s.writeTokenError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
		return
	}
	c, err := s.lookupClient(r.Context(), models.OAuthClientID(form.Get("client_id")))
	if err != nil {
		s.logClientError(form.Get("client_id"), err)
		s.writeTokenError(w, http.StatusUnauthorized, "invalid_client", "unknown client")
		return
	}
	if form.Get("resource") != c.resource {
		s.writeTokenError(w, http.StatusBadRequest, "invalid_target", "resource must be "+c.resource)
		return
	}
	s.provider.ServeHTTP(w, r.WithContext(withClient(withResource(r.Context(), c.resource), c)))
}

// revoke passes only this server's own token formats to ZITADEL: an access
// token that decrypts with the server key, or a refresh token. For anything
// else ZITADEL would parse the value as a JWT, and its claim decoder can
// panic on malformed input (dependency audit F1). Such tokens cannot belong
// to this server, so they get the RFC 7009 answer for an unknown token: 200.
func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		s.writeTokenError(w, http.StatusBadRequest, "invalid_request", "parameters must be sent in the request body")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeTokenError(w, http.StatusBadRequest, "invalid_request", "the request body could not be read")
		return
	}
	if repeated(r.PostForm, singleValued...) {
		s.writeTokenError(w, http.StatusBadRequest, "invalid_request", "parameters must not be repeated")
		return
	}
	if hasClientCredential(r, r.PostForm) {
		s.writeTokenError(w, http.StatusUnauthorized, "invalid_client", "only public clients are supported")
		return
	}
	c, err := s.lookupClient(r.Context(), models.OAuthClientID(r.PostForm.Get("client_id")))
	if err != nil {
		s.logClientError(r.PostForm.Get("client_id"), err)
		s.writeTokenError(w, http.StatusUnauthorized, "invalid_client", "unknown client")
		return
	}
	token := r.PostForm.Get("token")
	if !refreshTokenFormat.MatchString(token) {
		if _, err := s.crypto.Decrypt(token); err != nil {
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	s.provider.ServeHTTP(w, r.WithContext(withClient(r.Context(), c)))
}

// hasClientCredential reports any form of client authentication. Every client
// is public, so a credential or assertion, even an empty one, is refused
// instead of being passed to ZITADEL's authentication paths.
func hasClientCredential(r *http.Request, form url.Values) bool {
	_, header := r.Header["Authorization"]
	return header || hasAny(form, "client_secret", "client_assertion", "client_assertion_type")
}

func hasAny(form url.Values, keys ...string) bool {
	return slices.ContainsFunc(keys, form.Has)
}

func repeated(form url.Values, keys ...string) bool {
	for _, key := range keys {
		if len(form[key]) > 1 {
			return true
		}
	}
	return false
}

// errorRedirectURL builds an RFC 6749 error redirect with RFC 9207 iss.
// redirectURI must already have passed the client's redirect policy.
func (s *Server) errorRedirectURL(redirectURI, state, code, description string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI
	}
	q := u.Query()
	q.Set("error", code)
	if description != "" {
		q.Set("error_description", description)
	}
	if state != "" {
		q.Set("state", state)
	}
	q.Set("iss", s.issuer)
	u.RawQuery = q.Encode()
	return u.String()
}

// withIssuer sets iss on an authorization response URL (RFC 9207). It
// replaces any iss already present, for example one inside a registered
// redirect URI, so the value is always this server's issuer.
func withIssuer(location, issuer string) string {
	u, err := url.Parse(location)
	if err != nil {
		return location
	}
	q := u.Query()
	q.Set("iss", issuer)
	u.RawQuery = q.Encode()
	return u.String()
}

// sameEndpoint reports whether two URLs have the same scheme, host (with
// port) and path. ZITADEL rewrites and reorders the query of a redirect, so
// the query cannot identify the callback.
func sameEndpoint(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return false
	}
	return ua.Scheme == ub.Scheme && ua.Host == ub.Host && ua.EscapedPath() == ub.EscapedPath()
}

// issWriter is adapter A2. ZITADEL never adds iss, so the wrapper sets it on
// any redirect that goes back to the validated redirect URI. The redirect to
// the consent page is left alone.
type issWriter struct {
	http.ResponseWriter
	redirectURI string
	issuer      string
}

func (w *issWriter) WriteHeader(code int) {
	if code == http.StatusFound || code == http.StatusSeeOther {
		if location := w.Header().Get("Location"); sameEndpoint(location, w.redirectURI) {
			w.Header().Set("Location", withIssuer(location, w.issuer))
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) writeTokenError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description}); err != nil {
		s.log.Warn("writing OAuth token error", "error", err)
	}
}

var errorPageTemplate = template.Must(template.New("oauth-error").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Authorization error</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;line-height:1.5}</style>
</head><body><h1>Authorization error</h1><p>{{.}}</p><p>Close this page and start the connection again from the application.</p></body></html>`))

// clientErrorPage reports a client that cannot be used, without redirecting.
func (s *Server) clientErrorPage(w http.ResponseWriter, clientID string, err error) {
	s.logClientError(clientID, err)
	switch {
	case errors.Is(err, errUnknownClient):
		s.errorPage(w, "The application that sent you here is not registered with this Logchef instance.")
	case errors.Is(err, errCIMDRateLimited):
		s.errorPage(w, "Too many applications are connecting right now. Wait a minute and try again.")
	default:
		s.errorPage(w, "The application's client metadata document could not be loaded or is not valid.")
	}
}

// logClientError records why a CIMD client was refused. Unknown clients are
// not logged: anyone can send them.
func (s *Server) logClientError(clientID string, err error) {
	if !errors.Is(err, errUnknownClient) {
		s.log.Warn("refusing OAuth client", "client_id", clientID, "error", err)
	}
}

// errorPage reports an error without redirecting, for a request whose client
// or redirect URI cannot be trusted.
func (s *Server) errorPage(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusBadRequest)
	if err := errorPageTemplate.Execute(w, message); err != nil {
		s.log.Warn("writing OAuth error page", "error", err)
	}
}
