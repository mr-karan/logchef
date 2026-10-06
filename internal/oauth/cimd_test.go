package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

// testDocHost is a name in the httptest certificate. Test fetchers resolve
// it to the loopback test server.
const testDocHost = "example.com"

var loopbackV4 = netip.MustParseAddr("127.0.0.1")

// docServer is a real HTTPS server that serves client metadata documents.
type docServer struct {
	srv  *httptest.Server
	port string
	mu   sync.Mutex
	docs map[string]http.HandlerFunc
	hits map[string]*atomic.Int64
}

func newDocServer(t *testing.T) *docServer {
	t.Helper()
	d := &docServer{docs: map[string]http.HandlerFunc{}, hits: map[string]*atomic.Int64{}}
	d.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		h, ok := d.docs[r.URL.Path]
		if ok {
			d.hits[r.URL.Path].Add(1)
		}
		d.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(d.srv.Close)
	_, d.port, _ = net.SplitHostPort(d.srv.Listener.Addr().String())
	return d
}

// url returns the client_id for path on this server.
func (d *docServer) url(path string) string {
	return "https://" + testDocHost + ":" + d.port + path
}

func (d *docServer) handle(path string, h http.HandlerFunc) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.docs[path] = h
	d.hits[path] = &atomic.Int64{}
	return d.url(path)
}

// serveJSON serves body with an optional Cache-Control header.
func (d *docServer) serveJSON(path, cacheControl string, body func(id string) string) string {
	id := d.url(path)
	return d.handle(path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if cacheControl != "" {
			w.Header().Set("Cache-Control", cacheControl)
		}
		_, _ = io.WriteString(w, body(id))
	})
}

func (d *docServer) hitCount(path string) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hits[path].Load()
}

func (d *docServer) rootCAs() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(d.srv.Certificate())
	return pool
}

// testFetcher is the production fetcher with two test-only changes: it
// trusts the test server's certificate, and it resolves every spelling of
// testDocHost and its subdomains, and the literal ::1, to 127.0.0.1 and allows that one loopback address. allowAddr still refuses
// every other non-public address.
func testFetcher(d *docServer) *cimdFetcher {
	f := newCIMDFetcher()
	f.client.Transport.(*http.Transport).TLSClientConfig.RootCAs = d.rootCAs()
	f.lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
		if key := rateLimitHost(host); key == testDocHost || strings.HasSuffix(key, "."+testDocHost) {
			return []netip.Addr{loopbackV4}, nil
		}
		// The test server listens on IPv4 only; its certificate also names ::1.
		if addr, err := netip.ParseAddr(host); err == nil && addr == netip.IPv6Loopback() {
			return []netip.Addr{loopbackV4}, nil
		}
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}
	f.allowAddr = func(a netip.Addr) bool { return a == loopbackV4 || isPublicUnicast(a) }
	return f
}

func testResolver(d *docServer) *cimdResolver {
	r := newCIMDResolver("https://logchef.test/mcp", "https://logchef.test/oauth/consent?request=", slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.fetcher = testFetcher(d)
	return r
}

// claudeDoc is the shape of Claude Code's published document.
func claudeDoc(id string) string {
	return `{"client_id":"` + id + `","client_name":"Claude Code","client_uri":"https://claude.ai",` +
		`"redirect_uris":["http://localhost/callback","http://127.0.0.1/callback"],` +
		`"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`
}

func TestIsCIMDClientID(t *testing.T) {
	t.Parallel()
	for id, want := range map[string]bool{
		"https://claude.ai/oauth/claude-code-client-metadata":            true,
		"https://chatgpt.com/oauth/codex/client.json":                    true,
		"https://example.com:8443/client.json":                           true,
		"https://example.com/client.json?v=1":                            true,
		"https://example.com":                                            false,
		"https://example.com/":                                           false,
		"http://example.com/client.json":                                 false,
		"HTTPS://example.com/client.json":                                false,
		"https://user@example.com/client.json":                           false,
		"https://user:pw@example.com/client.json":                        false,
		"https://example.com/client.json#x":                              false,
		"https://example.com/client.json#":                               false,
		"https://example.com/a/../client.json":                           false,
		"https://example.com/a/./client.json":                            false,
		"https://example.com/a/%2e%2e/client.json":                       false,
		"https://example.com/..":                                         false,
		"https:///client.json":                                           false,
		"https://example.com/a b":                                        false,
		"chatgpt":                                                        false,
		"https://example.com/" + strings.Repeat("a", cimdMaxClientIDLen): false,
	} {
		if got := isCIMDClientID(id); got != want {
			t.Errorf("isCIMDClientID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestParseCIMDDocument(t *testing.T) {
	t.Parallel()
	const id = "https://client.example/meta.json"
	// doc builds a valid document and applies edits: a nil value deletes a key.
	doc := func(edits map[string]any) []byte {
		m := map[string]any{
			"client_id":                  id,
			"client_name":                "Example MCP",
			"redirect_uris":              []string{"https://client.example/cb"},
			"token_endpoint_auth_method": "none",
		}
		for k, v := range edits {
			if v == nil {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	valid := []struct {
		name      string
		body      []byte
		redirects []string
	}{
		{"minimal", doc(nil), []string{"https://client.example/cb"}},
		{"claude code", []byte(claudeDoc(id)), []string{"http://localhost/callback", "http://127.0.0.1/callback"}},
		{"chatgpt choices", doc(map[string]any{"token_endpoint_auth_method": "private_key_jwt", "token_endpoint_auth_methods_supported": []string{"none", "private_key_jwt"}}), []string{"https://client.example/cb"}},
		{"auth method only in choices", doc(map[string]any{"token_endpoint_auth_method": nil, "token_endpoint_auth_methods_supported": []string{"none"}}), []string{"https://client.example/cb"}},
		{"non-compliant extras kept out", doc(map[string]any{"redirect_uris": []string{"cursor://anysphere.cursor-mcp/oauth/callback", "http://remote.example/cb", "https://client.example/cb", "http://[::1]/cb", "http://127.9.9.9:8080/cb", "https://u@client.example/cb", "https://client.example/cb#f"}}), []string{"https://client.example/cb", "http://[::1]/cb", "http://127.9.9.9:8080/cb"}},
		{"public jwks", doc(map[string]any{"jwks": map[string]any{"keys": []map[string]string{{"kty": "EC", "crv": "P-256", "x": "a", "y": "b"}}}}), []string{"https://client.example/cb"}},
		{"name trimmed", doc(map[string]any{"client_name": "  Example  "}), []string{"https://client.example/cb"}},
	}
	for _, tc := range valid {
		got, err := parseCIMDDocument(id, tc.body)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if strings.Join(got.redirectURIs, " ") != strings.Join(tc.redirects, " ") {
			t.Errorf("%s: redirects = %v, want %v", tc.name, got.redirectURIs, tc.redirects)
		}
	}
	if got, _ := parseCIMDDocument(id, doc(map[string]any{"client_name": "  Example  "})); got.name != "Example" {
		t.Errorf("name = %q, want trimmed", got.name)
	}

	invalid := map[string][]byte{
		"invalid utf-8":                  append([]byte(`{"client_name":"`), 0xff, '"', '}'),
		"array":                          []byte(`[]`),
		"null":                           []byte(`null`),
		"string":                         []byte(`"x"`),
		"trailing data":                  append(doc(nil), []byte(`{}`)...),
		"client_id mismatch":             doc(map[string]any{"client_id": id + "x"}),
		"client_id missing":              doc(map[string]any{"client_id": nil}),
		"client_id not string":           doc(map[string]any{"client_id": 1}),
		"name missing":                   doc(map[string]any{"client_name": nil}),
		"name empty":                     doc(map[string]any{"client_name": "   "}),
		"name not string":                doc(map[string]any{"client_name": []string{"x"}}),
		"name control char":              doc(map[string]any{"client_name": "Claude\nCode"}),
		"name bidi override":             doc(map[string]any{"client_name": "Claude \u202eedoC"}),
		"name too long":                  doc(map[string]any{"client_name": strings.Repeat("a", cimdMaxNameLen+1)}),
		"redirects missing":              doc(map[string]any{"redirect_uris": nil}),
		"redirects empty":                doc(map[string]any{"redirect_uris": []string{}}),
		"redirects not array":            doc(map[string]any{"redirect_uris": "https://client.example/cb"}),
		"no compliant redirect":          doc(map[string]any{"redirect_uris": []string{"cursor://x/cb", "http://remote.example/cb"}}),
		"javascript redirect":            doc(map[string]any{"redirect_uris": []string{"https://client.example/cb", "javascript:alert(1)"}}),
		"data redirect":                  doc(map[string]any{"redirect_uris": []string{"https://client.example/cb", "data:text/html,x"}}),
		"obfuscated javascript logo":     doc(map[string]any{"logo_uri": " \tJava\nScript:alert(1)"}),
		"vbscript client_uri":            doc(map[string]any{"client_uri": "VBSCRIPT:x"}),
		"file tos_uri":                   doc(map[string]any{"tos_uri": "file:///etc/passwd"}),
		"uri field not string":           doc(map[string]any{"policy_uri": 5}),
		"client_secret":                  doc(map[string]any{"client_secret": "s"}),
		"client_secret_expires_at":       doc(map[string]any{"client_secret_expires_at": 0}),
		"no auth method":                 doc(map[string]any{"token_endpoint_auth_method": nil}),
		"private_key_jwt only":           doc(map[string]any{"token_endpoint_auth_method": "private_key_jwt"}),
		"client_secret_basic":            doc(map[string]any{"token_endpoint_auth_method": "client_secret_basic"}),
		"client_secret_post in choices":  doc(map[string]any{"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post"}}),
		"client_secret_jwt":              doc(map[string]any{"token_endpoint_auth_methods_supported": []string{"client_secret_jwt", "none"}}),
		"auth method not string":         doc(map[string]any{"token_endpoint_auth_method": []string{"none"}}),
		"private jwk":                    doc(map[string]any{"jwks": map[string]any{"keys": []map[string]string{{"kty": "EC", "d": "secret"}}}}),
		"rsa private member":             doc(map[string]any{"jwks": map[string]any{"keys": []map[string]string{{"kty": "RSA", "n": "a", "e": "AQAB", "p": "x"}}}}),
		"symmetric jwk":                  doc(map[string]any{"jwks": map[string]any{"keys": []map[string]string{{"kty": "oct"}}}}),
		"symmetric jwk without kty":      doc(map[string]any{"jwks": map[string]any{"keys": []map[string]string{{"k": "c2VjcmV0"}}}}),
		"jwks not a set":                 doc(map[string]any{"jwks": "x"}),
		"jwks_uri javascript":            doc(map[string]any{"jwks_uri": "javascript:x"}),
		"post_logout_redirect_uris blob": doc(map[string]any{"post_logout_redirect_uris": []string{"blob:https://x/y"}}),
	}
	for name, body := range invalid {
		if _, err := parseCIMDDocument(id, body); err == nil {
			t.Errorf("%s: document accepted", name)
		}
	}
}

func TestCIMDRedirectMatching(t *testing.T) {
	t.Parallel()
	c := &client{info: ClientInfo{Kind: ClientCIMD}, redirectURIs: []string{
		"http://localhost/callback", "http://127.0.0.1/callback", "http://[::1]/cb", "http://127.0.0.1:9000/fixed", "https://app.example/oauth/cb?x=1",
	}}
	for raw, want := range map[string]bool{
		"http://localhost/callback":                    true,
		"http://localhost:43123/callback":              true,
		"http://127.0.0.1:1/callback":                  true,
		"http://127.0.0.1:65535/callback":              true,
		"http://[::1]:5000/cb":                         true,
		"http://127.0.0.1:9000/fixed":                  true,
		"https://app.example/oauth/cb?x=1":             true,
		"http://127.0.0.1:9001/fixed":                  false,
		"http://127.0.0.1/fixed":                       false,
		"http://127.0.0.1:0/callback":                  false,
		"http://127.0.0.1:65536/callback":              false,
		"http://127.0.0.2:5000/callback":               false,
		"http://localhost:5000/callback/":              false,
		"http://localhost:5000/callback?a=1":           false,
		"http://localhost:5000/callback#f":             false,
		"http://u@localhost:5000/callback":             false,
		"https://localhost:5000/callback":              false,
		"http://localhost:5000/Callback":               false,
		"https://app.example/oauth/cb":                 false,
		"https://app.example:443/oauth/cb?x=1":         false,
		"cursor://anysphere.cursor-mcp/oauth/callback": false,
	} {
		if got := c.redirectAllowed(raw); got != want {
			t.Errorf("redirectAllowed(%q) = %v, want %v", raw, got, want)
		}
	}
}

// refusedV6 covers every IPv6 range outside 2000::/3 that matters for SSRF,
// and every special-purpose block that is excluded inside it.
var refusedV6 = []string{
	"::", "::1",
	"::127.0.0.1", "::10.0.0.1", "::169.254.169.254", // IPv4-compatible, ::/96
	"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254", // IPv4-mapped private
	"::ffff:0:10.0.0.1", "::ffff:0:127.0.0.1", // IPv4-translated, ::ffff:0:0:0/96
	"64:ff9b::a00:1", "64:ff9b:1::1", // NAT64
	"100::1", "100:0:0:1::1", // discard and dummy prefixes
	"1::1", "1fff:ffff::1", "4000::1", "5f00::1", "e000::1", // outside 2000::/3, including SRv6
	"fc00::1", "fd12::1", "fe80::1", "fec0::1", "ff02::1",
	"2001::1", "2001:0:4136:e378::1", // Teredo
	"2001:2::1",                // benchmarking
	"2001:10::1", "2001:20::1", // ORCHID
	"2001:1ff:ffff::1",                // last address of 2001::/23
	"2001:db8::1", "2001:db8:ffff::1", // documentation
	"2002::1", "2002:a00:1::", // 6to4
	"3fff::1", "3fff:fff::1", // documentation, 3fff::/20
}

// allowedV6 are public addresses, including the edges of the exclusions and
// a mapped public IPv4 address (the dialer unmaps it).
var allowedV6 = []string{"::ffff:8.8.8.8", "2606:4700:4700::1111", "2001:4860:4860::8888", "2001:200::1", "2000::1", "2003::1", "2a00:1450:4001::1", "3fff:1000::1", "3ffe::1"}

func TestIsPublicUnicast(t *testing.T) {
	t.Parallel()
	want := map[string]bool{
		"8.8.8.8":         true,
		"1.1.1.1":         true,
		"127.0.0.1":       false,
		"127.1.2.3":       false,
		"10.0.0.1":        false,
		"172.16.5.4":      false,
		"192.168.1.1":     false,
		"169.254.169.254": false,
		"100.64.0.1":      false,
		"100.127.255.254": false,
		"0.0.0.0":         false,
		"0.1.2.3":         false,
		"224.0.0.1":       false,
		"255.255.255.255": false,
		"240.0.0.1":       false,
		"192.0.0.170":     false,
		"198.18.0.1":      false,
	}
	for _, a := range refusedV6 {
		want[a] = false
	}
	for _, a := range allowedV6 {
		want[a] = true
	}
	for addr, w := range want {
		if got := isPublicUnicast(netip.MustParseAddr(addr)); got != w {
			t.Errorf("isPublicUnicast(%s) = %v, want %v", addr, got, w)
		}
	}
}

// The Control hook is the SSRF boundary: it sees the address of the socket
// itself.
func TestCIMDDialControl(t *testing.T) {
	t.Parallel()
	f := newCIMDFetcher()
	cases := map[string]bool{
		"8.8.8.8:443":           false,
		"[2606:4700::1111]:443": false,
		"127.0.0.1:443":         true,
		"10.0.0.1:443":          true,
		"[::1]:443":             true,
		"[::ffff:10.0.0.1]:443": true,
		"169.254.169.254:80":    true,
		"[fe80::1%eth0]:443":    true,
		"not-an-address":        true,
	}
	for _, a := range refusedV6 {
		cases["["+a+"]:443"] = true
	}
	for _, a := range allowedV6 {
		cases["["+a+"]:443"] = false
	}
	for address, wantErr := range cases {
		err := f.control("tcp", address, nil)
		if (err != nil) != wantErr {
			t.Errorf("control(%s) = %v, want error %v", address, err, wantErr)
		}
		if err != nil && !errors.Is(err, errNotPublicAddress) {
			t.Errorf("control(%s) error %v does not wrap errNotPublicAddress", address, err)
		}
	}
}

// The production fetcher refuses loopback and private targets however they
// are reached: an IP literal, a name that resolves there, a resolver that
// answers with a private address (DNS rebinding), or a redirect. The test
// servers record that no request reached them.
func TestCIMDFetchSSRF(t *testing.T) {
	t.Parallel()
	d := newDocServer(t)
	d.serveJSON("/claude.json", "", claudeDoc)

	ctx := context.Background()
	prod := newCIMDFetcher()
	prod.client.Transport.(*http.Transport).TLSClientConfig.RootCAs = d.rootCAs()
	for _, id := range []string{
		"https://127.0.0.1:" + d.port + "/claude.json",
		"https://[::1]:" + d.port + "/claude.json",
		"https://localhost:" + d.port + "/claude.json",
	} {
		if _, err := prod.fetch(ctx, id); !errors.Is(err, errNotPublicAddress) {
			t.Errorf("fetch %s: err = %v, want errNotPublicAddress", id, err)
		}
	}

	for _, private := range append([]string{"127.0.0.1", "10.0.0.1", "192.168.0.10", "169.254.169.254", "100.64.1.1"}, refusedV6...) {
		rebinding := newCIMDFetcher()
		rebinding.client.Transport.(*http.Transport).TLSClientConfig.RootCAs = d.rootCAs()
		rebinding.timeout = 2 * time.Second
		rebinding.lookup = func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr(private)}, nil
		}
		if _, err := rebinding.fetch(ctx, d.url("/claude.json")); !errors.Is(err, errNotPublicAddress) {
			t.Errorf("resolver answering %s: err = %v, want errNotPublicAddress", private, err)
		}
	}
	if n := d.hitCount("/claude.json"); n != 0 {
		t.Fatalf("document server received %d requests from a refused fetch", n)
	}

	// A permitted host that redirects to an internal address.
	internal := newDocServer(t)
	internal.serveJSON("/claude.json", "", claudeDoc)
	d.handle("/redirect.json", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://127.0.0.1:"+internal.port+"/claude.json", http.StatusFound)
	})
	f := testFetcher(d)
	if _, err := f.fetch(ctx, d.url("/redirect.json")); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Errorf("redirect: err = %v, want HTTP 302 refusal", err)
	}
	if d.hitCount("/redirect.json") != 1 || internal.hitCount("/claude.json") != 0 {
		t.Errorf("redirect hits: origin %d, internal %d; want 1, 0", d.hitCount("/redirect.json"), internal.hitCount("/claude.json"))
	}
}

func TestCIMDFetchLimits(t *testing.T) {
	t.Parallel()
	d := newDocServer(t)
	ctx := context.Background()
	pad := func(n int) string { return strings.Repeat(" ", n) }
	d.handle("/exact.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, pad(cimdMaxBytes))
	})
	d.handle("/over.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, pad(cimdMaxBytes+1))
	})
	d.handle("/chunked.json", func(w http.ResponseWriter, _ *http.Request) {
		for range 10 {
			_, _ = io.WriteString(w, pad(1024))
			w.(http.Flusher).Flush()
		}
	})
	d.handle("/content-length.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(1<<20))
		_, _ = io.WriteString(w, pad(10))
	})
	d.handle("/slow-headers.json", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	})
	d.handle("/slow-body.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{")
		w.(http.Flusher).Flush()
		for range 50 {
			select {
			case <-time.After(100 * time.Millisecond):
				_, _ = io.WriteString(w, " ")
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	d.handle("/missing.json", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })

	f := testFetcher(d)
	if resp, err := f.fetch(ctx, d.url("/exact.json")); err != nil || len(resp.body) != cimdMaxBytes {
		t.Errorf("exact size: %v", err)
	}
	for _, path := range []string{"/over.json", "/chunked.json", "/content-length.json"} {
		if _, err := f.fetch(ctx, d.url(path)); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Errorf("%s: err = %v, want size refusal", path, err)
		}
	}
	if _, err := f.fetch(ctx, d.url("/missing.json")); err == nil {
		t.Error("404 accepted")
	}

	f.timeout = 300 * time.Millisecond
	for _, path := range []string{"/slow-headers.json", "/slow-body.json"} {
		start := time.Now()
		_, err := f.fetch(ctx, d.url(path))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: err = %v, want deadline exceeded", path, err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("%s: took %v, timeout not enforced", path, elapsed)
		}
	}
	if newCIMDFetcher().timeout != 10*time.Second {
		t.Error("production fetch timeout is not 10 s")
	}
}

func TestCacheTTL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		headers []string // alternating name, value
		want    time.Duration
	}{
		{nil, 0},
		{[]string{"Cache-Control", "public, max-age=300"}, 300 * time.Second},
		{[]string{"Cache-Control", "max-age=300, s-maxage=60"}, 60 * time.Second},
		{[]string{"Cache-Control", "MAX-AGE=\"120\""}, 120 * time.Second},
		{[]string{"Cache-Control", "max-age=300", "Age", "100"}, 200 * time.Second},
		{[]string{"Cache-Control", "max-age=300", "Age", "400"}, 0},
		{[]string{"Cache-Control", "max-age=300, no-store"}, 0},
		{[]string{"Cache-Control", "no-cache, max-age=300"}, 0},
		{[]string{"Cache-Control", "max-age=31536000"}, cimdMaxCacheTTL},
		{[]string{"Cache-Control", "max-age=99999999999999"}, cimdMaxCacheTTL},
		{[]string{"Cache-Control", "max-age=-5"}, 0},
		{[]string{"Cache-Control", "max-age=abc"}, 0},
		{[]string{"Cache-Control", "public", "Cache-Control", "max-age=30"}, 30 * time.Second},
	} {
		h := http.Header{}
		for i := 0; i < len(tc.headers); i += 2 {
			h.Add(tc.headers[i], tc.headers[i+1])
		}
		if got := cacheTTL(h); got != tc.want {
			t.Errorf("cacheTTL(%v) = %v, want %v", h, got, tc.want)
		}
	}
}

func TestCIMDResolverCache(t *testing.T) {
	t.Parallel()
	d := newDocServer(t)
	ctx := context.Background()
	r := testResolver(d)

	cached := d.serveJSON("/cached.json", "public, max-age=300", claudeDoc)
	for range 3 {
		c, err := r.resolve(ctx, cached)
		if err != nil {
			t.Fatal(err)
		}
		if c.info.Name != "Claude Code" || c.info.Kind != ClientCIMD || c.info.Host != testDocHost+":"+d.port || c.resource != "https://logchef.test/mcp" {
			t.Fatalf("client = %+v", c)
		}
	}
	if n := d.hitCount("/cached.json"); n != 1 {
		t.Errorf("max-age document fetched %d times, want 1", n)
	}
	if exp := r.cache[cached].expires; time.Until(exp) > 300*time.Second || time.Until(exp) < 290*time.Second {
		t.Errorf("cache expiry %v not from max-age", exp)
	}

	uncached := d.serveJSON("/no-store.json", "no-store", claudeDoc)
	for range 3 {
		if _, err := r.resolve(ctx, uncached); err != nil {
			t.Fatal(err)
		}
	}
	if n := d.hitCount("/no-store.json"); n != 3 {
		t.Errorf("no-store document fetched %d times, want 3", n)
	}

	// An expired entry is fetched again.
	r.mu.Lock()
	r.cache[cached] = cimdCacheEntry{body: r.cache[cached].body, expires: time.Now().Add(-time.Second)}
	r.mu.Unlock()
	if _, err := r.resolve(ctx, cached); err != nil || d.hitCount("/cached.json") != 2 {
		t.Errorf("expired entry: err %v, fetches %d, want 2", err, d.hitCount("/cached.json"))
	}

	// A cached document that no longer validates is evicted and re-resolved
	// from origin in the same call.
	r.mu.Lock()
	r.cache[cached] = cimdCacheEntry{body: []byte(`{"client_id":"https://other.example/x"}`), expires: time.Now().Add(time.Hour)}
	r.mu.Unlock()
	if c, err := r.resolve(ctx, cached); err != nil || c.info.Name != "Claude Code" || d.hitCount("/cached.json") != 3 {
		t.Errorf("invalid cached entry: client %+v, err %v, fetches %d, want 3", c, err, d.hitCount("/cached.json"))
	}

	// Errors and invalid documents are never cached.
	var fail atomic.Bool
	fail.Store(true)
	flaky := d.handle("/flaky.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=300")
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, claudeDoc(d.url(r.URL.Path)))
	})
	if _, err := r.resolve(ctx, flaky); err == nil {
		t.Fatal("500 response accepted")
	}
	fail.Store(false)
	if _, err := r.resolve(ctx, flaky); err != nil || d.hitCount("/flaky.json") != 2 {
		t.Errorf("after an error: err %v, fetches %d, want 2", err, d.hitCount("/flaky.json"))
	}
	// A cached document that stops validating is evicted even when the
	// origin cannot replace it.
	r.mu.Lock()
	r.cache[flaky] = cimdCacheEntry{body: []byte(`{}`), expires: time.Now().Add(time.Hour)}
	r.mu.Unlock()
	fail.Store(true)
	if _, err := r.resolve(ctx, flaky); err == nil {
		t.Fatal("invalid cached entry with a failing origin resolved")
	}
	if _, ok := r.cache[flaky]; ok {
		t.Error("invalid cached entry not evicted")
	}
	r = testResolver(d) // a fresh host rate-limit bucket
	invalid := d.serveJSON("/invalid.json", "max-age=300", func(string) string {
		return `{"client_id":"https://wrong.example/x","client_name":"x","redirect_uris":["https://a.example/cb"],"token_endpoint_auth_method":"none"}`
	})
	for range 2 {
		if _, err := r.resolve(ctx, invalid); err == nil {
			t.Fatal("invalid document accepted")
		}
	}
	if n := d.hitCount("/invalid.json"); n != 2 {
		t.Errorf("invalid document fetched %d times, want 2 (never cached)", n)
	}
	if _, ok := r.cache[invalid]; ok {
		t.Error("invalid document cached")
	}
}

func TestCIMDCacheBounded(t *testing.T) {
	t.Parallel()
	r := testResolver(newDocServer(t))
	now := time.Now()
	r.cache["expired"] = cimdCacheEntry{expires: now.Add(-time.Second)}
	for i := range cimdCacheSize - 1 {
		r.store(fmt.Sprintf("k%d", i), cimdCacheEntry{expires: now.Add(time.Hour + time.Duration(i)*time.Second)})
	}
	r.store("new1", cimdCacheEntry{expires: now.Add(2 * time.Hour)})
	if _, ok := r.cache["expired"]; ok || len(r.cache) != cimdCacheSize {
		t.Fatalf("expired entry not evicted first: len %d", len(r.cache))
	}
	r.store("new2", cimdCacheEntry{expires: now.Add(2 * time.Hour)})
	if _, ok := r.cache["k0"]; ok || len(r.cache) != cimdCacheSize {
		t.Fatalf("entry closest to expiry not evicted: len %d", len(r.cache))
	}
	if _, ok := r.cache["new2"]; !ok {
		t.Fatal("new entry not stored")
	}
}

func TestCIMDRateLimit(t *testing.T) {
	t.Parallel()
	d := newDocServer(t)
	ctx := context.Background()
	r := testResolver(d)
	for i := range cimdHostBurst {
		id := d.serveJSON(fmt.Sprintf("/c%d.json", i), "", claudeDoc)
		if _, err := r.resolve(ctx, id); err != nil {
			t.Fatalf("fetch %d: %v", i, err)
		}
	}
	over := d.serveJSON("/over.json", "", claudeDoc)
	if _, err := r.resolve(ctx, over); !errors.Is(err, errCIMDRateLimited) {
		t.Fatalf("fetch beyond the host burst: err = %v, want errCIMDRateLimited", err)
	}
	if n := d.hitCount("/over.json"); n != 0 {
		t.Errorf("rate-limited fetch reached the host %d times", n)
	}

	// The global bucket limits fetches across hosts.
	g := testResolver(d)
	g.global = tokenBucket{tokens: 0, updated: time.Now()}
	if _, err := g.resolve(ctx, d.url("/c0.json")); !errors.Is(err, errCIMDRateLimited) {
		t.Errorf("empty global bucket: err = %v, want errCIMDRateLimited", err)
	}

	var b tokenBucket
	now := time.Now()
	for range cimdHostBurst {
		if !b.take(now, cimdHostRate, cimdHostBurst) {
			t.Fatal("bucket refused within its burst")
		}
	}
	if b.take(now, cimdHostRate, cimdHostBurst) {
		t.Fatal("bucket allowed beyond its burst")
	}
	if !b.take(now.Add(6*time.Second), cimdHostRate, cimdHostBurst) {
		t.Fatal("bucket did not refill")
	}
}

func TestRateLimitHost(t *testing.T) {
	t.Parallel()
	for host, want := range map[string]string{
		"example.com":                 "example.com",
		"EXAMPLE.COM":                 "example.com",
		"Example.Com.":                "example.com",
		"bücher.example.com":          "xn--bcher-kva.example.com",
		"BÜCHER.Example.COM.":         "xn--bcher-kva.example.com",
		"XN--BCHER-KVA.example.com":   "xn--bcher-kva.example.com",
		"8.8.8.8":                     "8.8.8.8",
		"2001:4860:4860:0:0:0:0:8888": "2001:4860:4860::8888",
		"2001:4860:4860::8888":        "2001:4860:4860::8888",
		"2001:4860:4860:0::8888":      "2001:4860:4860::8888",
		"2001:4860:4860::8888%eth0":   "2001:4860:4860::8888%eth0",
		"Odd_Name.Example.com":        "odd_name.example.com",
		"Odd_Name.Example.com.":       "odd_name.example.com",
		"example.com\u3002":           "example.com",
		"Example.COM\uff0e":           "example.com",
		"bücher.example.com\u3002":    "xn--bcher-kva.example.com",
		"::ffff:8.8.8.8":              "8.8.8.8",
		"::ffff:127.0.0.1":            "127.0.0.1",
	} {
		if got := rateLimitHost(host); got != want {
			t.Errorf("rateLimitHost(%q) = %q, want %q", host, got, want)
		}
	}
}

// Spellings of one host share its fetch budget, through real fetches to a
// local listener. Each client_id stays exact.
func TestCIMDRateLimitHostAliases(t *testing.T) {
	t.Parallel()
	d := newDocServer(t)
	ctx := context.Background()
	serve := func(rawHost, path string) string {
		id := "https://" + rawHost + ":" + d.port + path
		d.handle(path, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, claudeDoc(id)) })
		return id
	}
	check := func(r *cimdResolver, id string) {
		t.Helper()
		if !isCIMDClientID(id) {
			t.Fatalf("%s is not a CIMD client ID", id)
		}
		c, err := r.resolve(ctx, id)
		if err != nil {
			t.Fatalf("resolve %s: %v", id, err)
		}
		u, _ := url.Parse(id)
		if string(c.info.ID) != id || c.info.Host != u.Host {
			t.Fatalf("client %+v does not keep the exact client_id %s", c.info, id)
		}
	}

	r := testResolver(d)
	spellings := []string{"EXAMPLE.com", "Example.Com", "example.COM.", "example.com.", "eXaMpLe.CoM", "EXAMPLE.COM.", "exAmple.com", "examPle.com.", "Example.com", "EXample.com"}
	for i, h := range spellings[:cimdHostBurst] {
		check(r, serve(h, fmt.Sprintf("/case%d.json", i)))
	}
	over := serve(testDocHost, "/case-over.json")
	if _, err := r.resolve(ctx, over); !errors.Is(err, errCIMDRateLimited) {
		t.Fatalf("eleventh spelling: err = %v, want errCIMDRateLimited", err)
	}
	if n := d.hitCount("/case-over.json"); n != 0 {
		t.Fatalf("rate-limited spelling reached the host %d times", n)
	}

	// Unicode (percent-encoded in the URL), punycode and case variants of one
	// IDN share a bucket. Leave exactly as many tokens as variants.
	u := testResolver(d)
	idn := []string{"b%C3%BCcher.example.com", "xn--bcher-kva.example.com", "XN--BCHER-KVA.EXAMPLE.COM.", "B%C3%9CCHER.example.com"}
	u.hosts["xn--bcher-kva.example.com"] = &tokenBucket{tokens: float64(len(idn)), updated: time.Now()}
	for i, h := range idn {
		check(u, serve(h, fmt.Sprintf("/idn%d.json", i)))
	}
	if _, err := u.resolve(ctx, serve("Xn--Bcher-Kva.example.com", "/idn-over.json")); !errors.Is(err, errCIMDRateLimited) {
		t.Fatalf("IDN variant after the budget: err = %v, want errCIMDRateLimited", err)
	}
	if len(u.hosts) != 1 {
		t.Fatalf("IDN variants used %d buckets, want 1", len(u.hosts))
	}

	// Spellings of one IPv6 literal share a bucket.
	ip := testResolver(d)
	literals := []string{"[::1]", "[0::1]", "[0:0:0:0:0:0:0:1]"}
	ip.hosts["::1"] = &tokenBucket{tokens: float64(len(literals)), updated: time.Now()}
	for i, h := range literals {
		check(ip, serve(h, fmt.Sprintf("/ip%d.json", i)))
	}
	if _, err := ip.resolve(ctx, serve("[0000::0001]", "/ip-over.json")); !errors.Is(err, errCIMDRateLimited) {
		t.Fatalf("IPv6 literal spelling after the budget: err = %v, want errCIMDRateLimited", err)
	}
	if len(ip.hosts) != 1 {
		t.Fatalf("IPv6 literal spellings used %d buckets, want 1", len(ip.hosts))
	}
}

// After one spelling exhausts the host budget, an equivalent spelling is
// refused too: a Unicode root dot (percent-encoded in the URL), and an
// IPv4-mapped literal of the same IPv4 address.
func TestCIMDRateLimitExhaustedAliases(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, base, alias string }{
		{"ideographic root dot", testDocHost, testDocHost + "%E3%80%82"},
		{"fullwidth root dot", testDocHost, testDocHost + "%EF%BC%8E"},
		{"ASCII root dot", testDocHost, testDocHost + "."},
		{"mapped IPv4", "127.0.0.1", "[::ffff:127.0.0.1]"},
		{"native after mapped", "[::ffff:127.0.0.1]", "127.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := newDocServer(t)
			r := testResolver(d)
			r.fetcher.lookup = func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{loopbackV4}, nil }
			ctx := context.Background()
			baseID := "https://" + tc.base + ":" + d.port + "/base.json"
			aliasID := "https://" + tc.alias + ":" + d.port + "/alias.json"
			if !isCIMDClientID(baseID) || !isCIMDClientID(aliasID) {
				t.Fatalf("%s or %s is not a CIMD client ID", baseID, aliasID)
			}
			d.handle("/base.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, claudeDoc(baseID)) })
			d.handle("/alias.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, claudeDoc(aliasID)) })
			for i := range cimdHostBurst {
				if _, err := r.resolve(ctx, baseID); err != nil {
					t.Fatalf("base fetch %d: %v", i, err)
				}
			}
			if _, err := r.resolve(ctx, aliasID); !errors.Is(err, errCIMDRateLimited) {
				t.Fatalf("alias after the budget: err = %v, want errCIMDRateLimited", err)
			}
			if d.hitCount("/base.json") != cimdHostBurst || d.hitCount("/alias.json") != 0 || len(r.hosts) != 1 {
				t.Fatalf("base hits %d, alias hits %d, buckets %d; want %d, 0, 1", d.hitCount("/base.json"), d.hitCount("/alias.json"), len(r.hosts), cimdHostBurst)
			}
		})
	}
	if rateLimitHost("::ffff:8.8.8.8") != rateLimitHost("8.8.8.8") {
		t.Fatal("mapped and native public IPv4 have different keys")
	}
}

func cimdServer(t *testing.T, d *docServer, mutate func(*config.Config)) *Server {
	t.Helper()
	cfg := testConfig()
	cfg.Auth.OAuth.CIMDEnabled = true
	if mutate != nil {
		mutate(cfg)
	}
	s, err := New(cfg, openDB(t, filepath.Join(t.TempDir(), "o.db")), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if s.cimd != nil {
		s.cimd.fetcher = testFetcher(d)
	}
	return s
}

func TestCIMDMetadataAndConfig(t *testing.T) {
	t.Parallel()
	d := newDocServer(t)
	id := d.serveJSON("/claude.json", "", claudeDoc)

	on := cimdServer(t, d, nil)
	md := on.Metadata()
	if !md.ClientIDMetadataDocumentSupported || len(md.TokenEndpointAuthMethodsSupported) != 1 || md.TokenEndpointAuthMethodsSupported[0] != "none" {
		t.Fatalf("enabled metadata = %+v", md)
	}

	off := cimdServer(t, d, func(c *config.Config) { c.Auth.OAuth.CIMDEnabled = false })
	raw, err := json.Marshal(off.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "client_id_metadata_document_supported") {
		t.Fatalf("disabled metadata advertises CIMD: %s", raw)
	}
	if _, err := off.lookupClient(context.Background(), models.OAuthClientID(id)); !errors.Is(err, errUnknownClient) {
		t.Fatalf("disabled lookup: err = %v", err)
	}
	if n := d.hitCount("/claude.json"); n != 0 {
		t.Fatalf("disabled server fetched %d times", n)
	}

	// A pre-registered client with a URL ID wins over its document.
	pre := cimdServer(t, d, func(c *config.Config) {
		c.Auth.OAuth.Clients = append(c.Auth.OAuth.Clients, config.OAuthClientConfig{ID: id, Name: "Configured", RedirectURIs: []string{"https://configured.example/cb"}})
	})
	c, err := pre.lookupClient(context.Background(), models.OAuthClientID(id))
	if err != nil || c.info.Name != "Configured" || c.info.Kind != ClientWeb || d.hitCount("/claude.json") != 0 {
		t.Fatalf("pre-registered lookup: %+v %v, fetches %d", c, err, d.hitCount("/claude.json"))
	}
}

// TestCIMDFlow runs authorize, consent, token and MCP token authentication
// for a CIMD client whose document is served by a local HTTPS server.
func TestCIMDFlow(t *testing.T) {
	t.Parallel()
	d := newDocServer(t)
	id := d.serveJSON("/claude.json", "public, max-age=300", claudeDoc)
	d.serveJSON("/cursor.json", "", func(id string) string {
		return `{"client_id":"` + id + `","client_name":"Cursor","redirect_uris":["cursor://anysphere.cursor-mcp/oauth/callback","https://www.cursor.example/cb"],"token_endpoint_auth_method":"none"}`
	})
	s := cimdServer(t, d, nil)
	h := s.Handler()
	ctx := context.Background()

	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXkdBjftJeZ4CVP"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	redirect := "http://127.0.0.1:43123/callback"
	authorize := func(clientID, redirectURI, resource string) *httptest.ResponseRecorder {
		q := url.Values{
			"client_id": {clientID}, "redirect_uri": {redirectURI}, "response_type": {"code"}, "state": {"st"},
			"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "resource": {resource},
			"scope": {"logs:read sources:read offline_access"},
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://logchef.test"+AuthorizePath+"?"+q.Encode(), http.NoBody))
		return rec
	}

	// Refusals before any request is stored.
	if rec := authorize(id, "http://127.0.0.1:43123/other", s.mcpResource); rec.Code != http.StatusBadRequest {
		t.Errorf("unregistered path: status %d", rec.Code)
	}
	if rec := authorize(d.url("/cursor.json"), "cursor://anysphere.cursor-mcp/oauth/callback", s.mcpResource); rec.Code != http.StatusBadRequest {
		t.Errorf("non-compliant registered redirect: status %d", rec.Code)
	}
	if rec := authorize(d.url("/cursor.json"), "https://www.cursor.example/cb", s.mcpResource); rec.Code != http.StatusFound {
		t.Errorf("compliant redirect next to a non-compliant one: status %d", rec.Code)
	}
	if rec := authorize(d.url("/missing.json"), redirect, s.mcpResource); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "metadata document") {
		t.Errorf("missing document: status %d body %q", rec.Code, rec.Body.String())
	}
	if rec := authorize(id, redirect, s.apiResource); rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "error=invalid_target") {
		t.Errorf("API resource: status %d location %q", rec.Code, rec.Header().Get("Location"))
	}

	rec := authorize(id, redirect, s.mcpResource)
	loc, err := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || err != nil || loc.Path != ConsentPath {
		t.Fatalf("authorize: status %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	reqID := models.OAuthAuthRequestID(loc.Query().Get("request"))

	// The consent screen shows the document's name and the client_id host,
	// even after the document has left the cache.
	s.cimd.mu.Lock()
	clear(s.cimd.cache)
	s.cimd.mu.Unlock()
	consent, err := s.ConsentRequest(ctx, reqID)
	if err != nil {
		t.Fatal(err)
	}
	if consent.Client.Name != "Claude Code" || consent.Client.Host != testDocHost+":"+d.port || consent.Client.Kind != ClientCIMD ||
		consent.RedirectURI != redirect || consent.ResourceKind != "mcp" {
		t.Fatalf("consent = %+v", consent)
	}
	body, err := json.Marshal(consent)
	if err != nil || !strings.Contains(string(body), `"host":"`+testDocHost+":"+d.port+`"`) {
		t.Fatalf("consent JSON lacks host: %s", body)
	}

	user := &models.User{Email: "cimd@example.com", Status: models.UserStatusActive, AccountType: models.UserAccountTypeHuman, Role: models.UserRoleMember}
	if err := s.db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	next, err := s.Decide(ctx, reqID, user, true)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := url.Parse(next)
	if err != nil || !strings.HasPrefix(next, redirect+"?") || cb.Query().Get("state") != "st" || cb.Query().Get("iss") != s.issuer || cb.Query().Get("code") == "" {
		t.Fatalf("callback = %q", next)
	}

	token := func(form url.Values) (int, map[string]any) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "https://logchef.test"+TokenPath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	status, out := token(url.Values{
		"grant_type": {"authorization_code"}, "code": {cb.Query().Get("code")}, "code_verifier": {verifier},
		"client_id": {id}, "redirect_uri": {redirect}, "resource": {s.mcpResource},
	})
	access, _ := out["access_token"].(string)
	refresh, _ := out["refresh_token"].(string)
	if status != http.StatusOK || access == "" || refresh == "" {
		t.Fatalf("token: %d %v", status, out)
	}
	at, err := s.AuthenticateAccessToken(ctx, access, ResourceMCP)
	if err != nil || at.Principal.User.ID != user.ID {
		t.Fatalf("MCP token: %+v %v", at, err)
	}
	if _, err := s.AuthenticateAccessToken(ctx, access, ResourceAPI); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("CIMD token accepted by /api: %v", err)
	}

	status, out = token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {id}, "resource": {s.mcpResource}})
	if status != http.StatusOK || out["access_token"] == nil {
		t.Fatalf("refresh: %d %v", status, out)
	}
	status, out = token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {d.url("/missing.json")}, "resource": {s.mcpResource}})
	if status != http.StatusUnauthorized || out["error"] != "invalid_client" {
		t.Fatalf("unresolvable CIMD client at token: %d %v", status, out)
	}

	apps, err := s.ConnectedApps(ctx, user.ID)
	if err != nil || len(apps) != 1 || apps[0].Client.Name != "Claude Code" || apps[0].Client.Kind != ClientCIMD {
		t.Fatalf("connected apps = %+v %v", apps, err)
	}
}
