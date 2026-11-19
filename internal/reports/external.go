package reports

import (
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strings"
)

// MaxAddressBytes bounds an external address: the longest path RFC 5321
// allows. The To header that carries it stays far inside the 998-byte line
// limit.
const MaxAddressBytes = 254

// AllDomains in the allowed domain list lets every domain through.
const AllDomains = "*"

// ExternalPolicy is the two settings that gate addresses outside Logchef:
// reports.external_recipients and reports.allowed_email_domains.
type ExternalPolicy struct {
	Enabled bool
	// Domains are lowercase; AllDomains allows every domain.
	Domains []string
}

// Allows returns "" when address may receive a report, or the reason it is
// skipped. With no domain listed, no address passes.
func (p ExternalPolicy) Allows(address string) string {
	if !p.Enabled {
		return SkipExternalDisabled
	}
	domain := strings.ToLower(address[strings.LastIndexByte(address, '@')+1:])
	if slices.Contains(p.Domains, AllDomains) || slices.Contains(p.Domains, domain) {
		return ""
	}
	return SkipDomainNotAllowed
}

// ParseExternalAddress reads an address as an owner types it: a bare ASCII
// address with no display name, comment or domain literal, so it goes into
// the To header as it is. It returns the address with its domain in lower
// case.
func ParseExternalAddress(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("the address is empty")
	}
	if len(raw) > MaxAddressBytes {
		return "", fmt.Errorf("the address is longer than %d bytes", MaxAddressBytes)
	}
	for i := range len(raw) {
		if raw[i] <= ' ' || raw[i] >= 0x7f {
			return "", fmt.Errorf("%q is not a plain email address: use ASCII without spaces", raw)
		}
	}
	parsed, err := mail.ParseAddress(raw)
	if err != nil || parsed.Name != "" || parsed.Address != raw {
		return "", fmt.Errorf("%q is not a plain email address such as name@example.com", raw)
	}
	at := strings.LastIndexByte(raw, '@')
	domain := strings.ToLower(raw[at+1:])
	if !validDomain(domain) {
		return "", fmt.Errorf("%q does not end in a domain name such as example.com", raw)
	}
	return raw[:at+1] + domain, nil
}

// ParseAllowedDomains reads reports.allowed_email_domains: domains
// separated by commas, or AllDomains. An empty value allows none.
func ParseAllowedDomains(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var out []string
	for entry := range strings.SplitSeq(value, ",") {
		d := strings.ToLower(strings.TrimSpace(entry))
		if d != AllDomains && !validDomain(d) {
			return nil, fmt.Errorf("%q is not a domain name such as example.com, or * for every domain", strings.TrimSpace(entry))
		}
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out, nil
}

// validDomain accepts a lowercase host name of two or more labels.
func validDomain(d string) bool {
	if len(d) > 253 || !strings.Contains(d, ".") {
		return false
	}
	for label := range strings.SplitSeq(d, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := range len(label) {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}
