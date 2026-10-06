package oauth

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Fetch limits for a client metadata document.
const (
	cimdMaxBytes     = 5 * 1024
	cimdFetchTimeout = 10 * time.Second
)

var errNotPublicAddress = errors.New("address is not a public unicast address")

// nonPublicV4Prefixes are the special-purpose IPv4 ranges (RFC 6890 and the
// IANA registry) that netip's predicates do not cover.
var nonPublicV4Prefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

// globalUnicastV6 is the only IPv6 space a CIMD fetch may reach. Everything
// outside it is refused, including IPv4-compatible (::/96), IPv4-translated
// (::ffff:0:0:0/96), site-local (fec0::/10), NAT64, discard and SRv6 space.
var globalUnicastV6 = netip.MustParsePrefix("2000::/3")

// nonPublicV6Prefixes are the special-purpose blocks inside globalUnicastV6.
var nonPublicV6Prefixes = []netip.Prefix{
	netip.MustParsePrefix("2001::/23"), // IETF protocol assignments: Teredo, benchmarking, ORCHID
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), // 6to4
	netip.MustParsePrefix("3fff::/20"),
}

// isPublicUnicast reports whether a CIMD fetch may connect to addr. IPv4 uses
// a deny-list of special ranges; IPv6 uses an allow-list. An IPv4-mapped
// address is judged as the IPv4 address it is, because the dialer connects
// to it over IPv4.
func isPublicUnicast(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	addr = addr.Unmap()
	if addr.Is6() {
		return globalUnicastV6.Contains(addr) &&
			!slices.ContainsFunc(nonPublicV6Prefixes, func(p netip.Prefix) bool { return p.Contains(addr) })
	}
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() ||
		addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	return !slices.ContainsFunc(nonPublicV4Prefixes, func(p netip.Prefix) bool { return p.Contains(addr) })
}

// cimdFetcher downloads client metadata documents without reaching internal
// addresses. The address check runs in the dialer's Control hook on the
// address of every socket it opens, so a DNS answer that changes between
// lookup and dial cannot route a fetch to a private address.
type cimdFetcher struct {
	client    *http.Client
	lookup    func(ctx context.Context, host string) ([]netip.Addr, error)
	allowAddr func(netip.Addr) bool
	timeout   time.Duration
}

// newCIMDFetcher builds the production fetcher: system DNS, system roots,
// public unicast addresses only. Tests in this package replace lookup,
// allowAddr or the TLS roots on the value it returns; nothing outside the
// package can.
func newCIMDFetcher() *cimdFetcher {
	f := &cimdFetcher{
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		allowAddr: isPublicUnicast,
		timeout:   cimdFetchTimeout,
	}
	f.client = &http.Client{
		Transport: &http.Transport{
			// An environment proxy would make the proxy, not this dialer,
			// choose the destination address.
			Proxy:                  nil,
			DialContext:            f.dial,
			TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout:    cimdFetchTimeout,
			ResponseHeaderTimeout:  cimdFetchTimeout,
			MaxResponseHeaderBytes: 16 << 10,
			DisableCompression:     true,
			MaxIdleConns:           16,
			IdleConnTimeout:        time.Minute,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return f
}

// control refuses a socket whose remote address is not allowed.
func (f *cimdFetcher) control(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil || !f.allowAddr(ap.Addr()) {
		return fmt.Errorf("client metadata host %s: %w", address, errNotPublicAddress)
	}
	return nil
}

// dial resolves the host and dials each address as a literal, so the
// connection goes to an address the Control hook has checked.
func (f *cimdFetcher) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	addrs, err := f.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("client metadata host %s has no addresses", host)
	}
	dialer := &net.Dialer{Control: f.control}
	var errs []error
	for _, a := range addrs {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(a.String(), port))
		if err == nil {
			return conn, nil
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}

// cimdResponse is a fetched document and how long it may be cached.
type cimdResponse struct {
	body []byte
	ttl  time.Duration
}

// fetch downloads clientID. One deadline covers the connection, the headers
// and the body. Redirects are not followed.
func (f *cimdFetcher) fetch(ctx context.Context, clientID string) (*cimdResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	// The URL is attacker-chosen by design; the dialer's Control hook keeps
	// the connection off internal addresses.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, http.NoBody) //nolint:gosec // G704: SSRF-guarded client
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := f.client.Do(req) //nolint:gosec // G704: SSRF-guarded client
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("client metadata document returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > cimdMaxBytes {
		return nil, fmt.Errorf("client metadata document exceeds %d bytes", cimdMaxBytes)
	}
	var body bytes.Buffer
	if _, err := io.Copy(&body, io.LimitReader(resp.Body, cimdMaxBytes+1)); err != nil {
		return nil, fmt.Errorf("reading client metadata document: %w", err)
	}
	if body.Len() > cimdMaxBytes {
		return nil, fmt.Errorf("client metadata document exceeds %d bytes", cimdMaxBytes)
	}
	return &cimdResponse{body: body.Bytes(), ttl: cacheTTL(resp.Header)}, nil
}

// cacheTTL is the freshness lifetime that Cache-Control grants a shared
// cache, less the response's Age, capped at cimdMaxCacheTTL. A response
// without max-age or s-maxage is not cached.
func cacheTTL(h http.Header) time.Duration {
	var maxAge, sMaxAge = -1, -1
	for directive := range strings.SplitSeq(strings.Join(h.Values("Cache-Control"), ","), ",") {
		name, value, _ := strings.Cut(strings.TrimSpace(directive), "=")
		switch strings.ToLower(name) {
		case "no-store", "no-cache":
			return 0
		case "max-age":
			maxAge = parseSeconds(value)
		case "s-maxage":
			sMaxAge = parseSeconds(value)
		}
	}
	seconds := maxAge
	if sMaxAge >= 0 {
		seconds = sMaxAge
	}
	if age := parseSeconds(h.Get("Age")); age > 0 {
		seconds -= age
	}
	if seconds <= 0 {
		return 0
	}
	if seconds >= int(cimdMaxCacheTTL/time.Second) {
		return cimdMaxCacheTTL
	}
	return time.Duration(seconds) * time.Second
}

// parseSeconds parses a delta-seconds value, or returns -1.
func parseSeconds(v string) int {
	n, err := strconv.Atoi(strings.Trim(v, `"`))
	if err != nil || n < 0 {
		return -1
	}
	return n
}
