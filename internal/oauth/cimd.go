package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/sync/singleflight"

	"github.com/mr-karan/logchef/pkg/models"
)

// Client ID Metadata Documents (draft-ietf-oauth-client-id-metadata-document-00,
// as pinned by the MCP 2026-07-28 client registration rules). A client whose
// client_id is an https URL publishes its metadata at that URL, and Logchef
// fetches it instead of requiring registration. Only the public-client
// method "none" is implemented.
const (
	cimdMaxClientIDLen = 2048
	cimdMaxNameLen     = 200
	cimdMaxCacheTTL    = 7 * 24 * time.Hour
	cimdCacheSize      = 1024
	// Fetch rate limits: per client_id host, and for the whole process.
	cimdHostRate    = 1.0 / 6
	cimdHostBurst   = 10
	cimdGlobalRate  = 1.0
	cimdGlobalBurst = 60
	cimdMaxHosts    = 1024
)

var errCIMDRateLimited = errors.New("too many client metadata fetches; try again later")

// dangerousSchemes reject a whole document when any URI field uses one.
var dangerousSchemes = []string{"javascript:", "data:", "vbscript:", "file:", "blob:", "filesystem:"}

// symmetricAuthMethods need a shared secret, which a published document
// cannot carry.
var symmetricAuthMethods = []string{"client_secret_basic", "client_secret_post", "client_secret_jwt"}

// privateJWKMembers are the JWK members of private or symmetric keys.
var privateJWKMembers = []string{"d", "p", "q", "dp", "dq", "qi", "oth", "k"}

// isCIMDClientID reports whether id is a Client Identifier URL: https, a
// path other than "/", no user info, fragment or dot segments, and written
// in canonical form so that the exact string comparison with the document's
// client_id is meaningful.
func isCIMDClientID(id string) bool {
	if len(id) > cimdMaxClientIDLen || !strings.HasPrefix(id, "https://") {
		return false
	}
	u, err := url.Parse(id)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(id, "#") ||
		u.Path == "" || u.Path == "/" || u.String() != id {
		return false
	}
	return !slices.ContainsFunc(strings.Split(u.Path, "/"), func(seg string) bool { return seg == "." || seg == ".." })
}

// cimdDocument is the validated part of a client metadata document.
type cimdDocument struct {
	name string
	// redirectURIs holds only the compliant redirect URIs. Others in the
	// document (for example cursor://) are accepted but never matched.
	redirectURIs []string
}

// parseCIMDDocument validates a fetched document for clientID.
func parseCIMDDocument(clientID string, body []byte) (*cimdDocument, error) {
	if !utf8.Valid(body) {
		return nil, errors.New("document is not valid UTF-8")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, errors.New("document is not a JSON object")
	}
	for _, secret := range []string{"client_secret", "client_secret_expires_at"} {
		if _, ok := fields[secret]; ok {
			return nil, fmt.Errorf("document must not contain %s", secret)
		}
	}
	var id, name string
	if json.Unmarshal(fields["client_id"], &id) != nil || id != clientID {
		return nil, errors.New("document client_id does not equal its URL")
	}
	if json.Unmarshal(fields["client_name"], &name) != nil {
		return nil, errors.New("client_name must be a string")
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > cimdMaxNameLen ||
		strings.ContainsFunc(name, func(r rune) bool { return unicode.In(r, unicode.Cc, unicode.Cf) }) {
		return nil, fmt.Errorf("client_name must be 1 to %d printable characters", cimdMaxNameLen)
	}
	if err := checkURIFields(fields); err != nil {
		return nil, err
	}
	var redirects []string
	if json.Unmarshal(fields["redirect_uris"], &redirects) != nil || len(redirects) == 0 {
		return nil, errors.New("redirect_uris must be a non-empty array of strings")
	}
	compliant := slices.DeleteFunc(slices.Clone(redirects), func(u string) bool { return !compliantRedirectURI(u) })
	if len(compliant) == 0 {
		return nil, errors.New("redirect_uris has no https or loopback http URI")
	}
	if err := checkAuthMethods(fields); err != nil {
		return nil, err
	}
	if err := checkPublicJWKS(fields["jwks"]); err != nil {
		return nil, err
	}
	return &cimdDocument{name: name, redirectURIs: compliant}, nil
}

// checkURIFields rejects the document when any *_uri or *_uris field is
// malformed or uses a dangerous scheme.
func checkURIFields(fields map[string]json.RawMessage) error {
	for key, raw := range fields {
		if !strings.HasSuffix(key, "_uri") && !strings.HasSuffix(key, "_uris") {
			continue
		}
		uris, err := stringOrStrings(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if slices.ContainsFunc(uris, hasDangerousScheme) {
			return fmt.Errorf("%s uses a forbidden URI scheme", key)
		}
	}
	return nil
}

func stringOrStrings(raw json.RawMessage) ([]string, error) {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, errors.New("must be a string or an array of strings")
	}
	return many, nil
}

// hasDangerousScheme reports whether a browser would treat raw as a URI with
// a script or local-content scheme. Browsers drop tabs and newlines anywhere
// and C0 controls and spaces at either end before they read the scheme.
func hasDangerousScheme(raw string) bool {
	s := strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, raw)
	s = strings.ToLower(strings.TrimFunc(s, func(r rune) bool { return r <= ' ' }))
	return slices.ContainsFunc(dangerousSchemes, func(scheme string) bool { return strings.HasPrefix(s, scheme) })
}

// compliantRedirectURI accepts https, or http on a loopback host, with no
// user info or fragment.
func compliantRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") || u.Hostname() == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		return isLoopbackHostname(u.Hostname())
	}
	return false
}

// isLoopbackHostname accepts localhost, 127.0.0.0/8 and ::1.
func isLoopbackHostname(host string) bool {
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && !addr.Is4In6() && addr.IsLoopback()
}

// checkAuthMethods requires the document to allow "none" and to offer no
// shared-secret method. Both the RFC 7591 field and the OpenID RP Metadata
// Choices list count.
func checkAuthMethods(fields map[string]json.RawMessage) error {
	var methods []string
	if raw, ok := fields["token_endpoint_auth_method"]; ok {
		var m string
		if json.Unmarshal(raw, &m) != nil {
			return errors.New("token_endpoint_auth_method must be a string")
		}
		methods = append(methods, m)
	}
	if raw, ok := fields["token_endpoint_auth_methods_supported"]; ok {
		var ms []string
		if json.Unmarshal(raw, &ms) != nil {
			return errors.New("token_endpoint_auth_methods_supported must be an array of strings")
		}
		methods = append(methods, ms...)
	}
	if slices.ContainsFunc(methods, func(m string) bool { return slices.Contains(symmetricAuthMethods, m) }) {
		return errors.New("a client metadata document cannot use a client secret")
	}
	if !slices.Contains(methods, "none") {
		return errors.New(`token_endpoint_auth_method must allow "none"`)
	}
	return nil
}

// checkPublicJWKS refuses private or symmetric key material.
func checkPublicJWKS(raw json.RawMessage) error {
	if raw == nil {
		return nil
	}
	var set struct {
		Keys []map[string]json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		return errors.New("jwks must be a JSON Web Key Set")
	}
	for _, key := range set.Keys {
		var kty string
		_ = json.Unmarshal(key["kty"], &kty)
		if kty == "oct" || slices.ContainsFunc(privateJWKMembers, func(m string) bool { _, ok := key[m]; return ok }) {
			return errors.New("jwks must not contain private or symmetric keys")
		}
	}
	return nil
}

// cimdResolver fetches, validates and caches client metadata documents.
type cimdResolver struct {
	fetcher    *cimdFetcher
	resource   string
	consentURL string
	log        *slog.Logger
	flights    singleflight.Group

	mu     sync.Mutex
	cache  map[string]cimdCacheEntry
	hosts  map[string]*tokenBucket
	global tokenBucket
}

// cimdCacheEntry keeps the raw document, which is validated again on every
// use.
type cimdCacheEntry struct {
	body    []byte
	expires time.Time
}

func newCIMDResolver(resource, consentURL string, log *slog.Logger) *cimdResolver {
	return &cimdResolver{
		fetcher:    newCIMDFetcher(),
		resource:   resource,
		consentURL: consentURL,
		log:        log,
		cache:      make(map[string]cimdCacheEntry),
		hosts:      make(map[string]*tokenBucket),
	}
}

// resolve returns the client for a Client Identifier URL, from the cache or
// from its origin. Concurrent requests for one URL share a fetch.
func (r *cimdResolver) resolve(ctx context.Context, id string) (*client, error) {
	if c := r.cached(id); c != nil {
		return c, nil
	}
	ch := r.flights.DoChan(id, func() (any, error) {
		return r.fetchClient(context.WithoutCancel(ctx), id)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*client), nil
	}
}

// cached returns a fresh cached client. An expired entry, or one that no
// longer validates, is evicted.
func (r *cimdResolver) cached(id string) *client {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[id]
	if !ok {
		return nil
	}
	if !time.Now().Before(entry.expires) {
		delete(r.cache, id)
		return nil
	}
	c, err := r.newClient(id, entry.body)
	if err != nil {
		delete(r.cache, id)
		r.log.Warn("evicting cached client metadata document", "client_id", id, "error", err)
		return nil
	}
	return c
}

func (r *cimdResolver) fetchClient(ctx context.Context, id string) (*client, error) {
	u, err := url.Parse(id)
	if err != nil {
		return nil, err
	}
	if !r.allowFetch(rateLimitHost(u.Hostname()), time.Now()) {
		return nil, errCIMDRateLimited
	}
	resp, err := r.fetcher.fetch(ctx, id)
	if err != nil {
		return nil, err
	}
	c, err := r.newClient(id, resp.body)
	if err != nil {
		return nil, err
	}
	if resp.ttl > 0 {
		r.store(id, cimdCacheEntry{body: resp.body, expires: time.Now().Add(resp.ttl)})
	}
	return c, nil
}

func (r *cimdResolver) newClient(id string, body []byte) (*client, error) {
	doc, err := parseCIMDDocument(id, body)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(id)
	if err != nil {
		return nil, err
	}
	clientID := models.OAuthClientID(id)
	return &client{
		info:         ClientInfo{ID: clientID, Name: doc.name, Kind: ClientCIMD, Host: u.Host},
		redirectURIs: doc.redirectURIs,
		resource:     r.resource,
		consentURL:   r.consentURL,
	}, nil
}

// cachedInfo describes a CIMD client without fetching. A client that is not
// cached is named by its host.
func (r *cimdResolver) cachedInfo(id string) ClientInfo {
	if c := r.cached(id); c != nil {
		return c.info
	}
	host := id
	if u, err := url.Parse(id); err == nil {
		host = u.Host
	}
	return ClientInfo{ID: models.OAuthClientID(id), Name: host, Kind: ClientCIMD, Host: host}
}

// store adds an entry, evicting expired entries and then the one closest to
// expiry when the cache is full.
func (r *cimdResolver) store(id string, entry cimdCacheEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.cache[id]; !ok && len(r.cache) >= cimdCacheSize {
		now := time.Now()
		var oldest string
		for k, e := range r.cache {
			if !now.Before(e.expires) {
				delete(r.cache, k)
			} else if oldest == "" || e.expires.Before(r.cache[oldest].expires) {
				oldest = k
			}
		}
		if len(r.cache) >= cimdCacheSize {
			delete(r.cache, oldest)
		}
	}
	r.cache[id] = entry
}

// rateLimitHost is the bucket key for a client_id host. Spellings of one
// host share a bucket: DNS names are folded to lowercase ASCII (IDNA), then
// lose one trailing root dot (IDNA maps a Unicode full stop to "."), and IP
// literals take their canonical unmapped form, which is what the dialer
// connects to. The key is used only for rate limiting; the client_id itself
// is never rewritten.
func rateLimitHost(host string) string {
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Unmap().String()
	}
	if ascii, err := idna.Lookup.ToASCII(host); err == nil {
		host = ascii
	} else {
		host = strings.ToLower(host)
	}
	return strings.TrimSuffix(host, ".")
}

// allowFetch takes a token from the host's bucket and the global bucket.
func (r *cimdResolver) allowFetch(host string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.hosts[host]
	if !ok {
		if len(r.hosts) >= cimdMaxHosts {
			for h, hb := range r.hosts {
				if hb.full(now, cimdHostRate, cimdHostBurst) {
					delete(r.hosts, h)
				}
			}
			if len(r.hosts) >= cimdMaxHosts {
				return false
			}
		}
		b = &tokenBucket{}
		r.hosts[host] = b
	}
	return b.take(now, cimdHostRate, cimdHostBurst) && r.global.take(now, cimdGlobalRate, cimdGlobalBurst)
}

// tokenBucket is a rate limiter. The zero value is a full bucket.
type tokenBucket struct {
	tokens  float64
	updated time.Time
}

func (b *tokenBucket) refill(now time.Time, rate, burst float64) {
	if b.updated.IsZero() {
		b.tokens = burst
	} else {
		b.tokens = min(burst, b.tokens+now.Sub(b.updated).Seconds()*rate)
	}
	b.updated = now
}

func (b *tokenBucket) take(now time.Time, rate, burst float64) bool {
	b.refill(now, rate, burst)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (b *tokenBucket) full(now time.Time, rate, burst float64) bool {
	b.refill(now, rate, burst)
	return b.tokens >= burst
}
