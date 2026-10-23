package config

import (
	"fmt"
	"net/netip"
	"strings"
)

// WebhookAllowlist holds canonical host exceptions to the webhook address policy.
type WebhookAllowlist struct {
	hosts map[string]struct{}
}

// ParseWebhookAllowlist parses hostnames and bare IP addresses from configuration.
func ParseWebhookAllowlist(entries []string) (WebhookAllowlist, error) {
	hosts := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		host := strings.TrimSpace(entry)
		if ip, err := netip.ParseAddr(host); err == nil && ip.Zone() == "" {
			hosts[ip.Unmap().String()] = struct{}{}
			continue
		}
		host = strings.ToLower(strings.TrimSuffix(host, "."))
		if !validWebhookHostname(host) {
			return WebhookAllowlist{}, fmt.Errorf("alerts.webhook_allowed_hosts entry %q must be a hostname or bare IP address without a port", entry)
		}
		hosts[host] = struct{}{}
	}
	return WebhookAllowlist{hosts: hosts}, nil
}

// Contains reports whether a destination host has an explicit network exception.
func (a WebhookAllowlist) Contains(host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		host = ip.Unmap().String()
	} else {
		host = strings.ToLower(strings.TrimSuffix(host, "."))
	}
	_, ok := a.hosts[host]
	return ok
}

func validWebhookHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
				return false
			}
		}
	}
	return true
}
