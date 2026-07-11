package auth

import "testing"

func TestMatchAllowedDomain(t *testing.T) {
	tests := []struct {
		name           string
		email          string
		allowedDomains []string
		wantDomain     string
		wantOK         bool
	}{
		{
			name:           "exact match",
			email:          "alice@example.com",
			allowedDomains: []string{"example.com"},
			wantDomain:     "example.com",
			wantOK:         true,
		},
		{
			name:           "case-insensitive email domain",
			email:          "alice@EXAMPLE.com",
			allowedDomains: []string{"example.com"},
			wantDomain:     "example.com",
			wantOK:         true,
		},
		{
			name:           "case-insensitive allowed_domains entry",
			email:          "alice@example.com",
			allowedDomains: []string{"EXAMPLE.COM"},
			wantDomain:     "example.com",
			wantOK:         true,
		},
		{
			name:           "multi-domain list matches second entry",
			email:          "bob@other.example.org",
			allowedDomains: []string{"example.com", "other.example.org"},
			wantDomain:     "other.example.org",
			wantOK:         true,
		},
		{
			name:           "multi-domain list, no match",
			email:          "bob@notallowed.com",
			allowedDomains: []string{"example.com", "other.example.org"},
			wantOK:         false,
		},
		{
			name:           "no subdomain wildcard matching",
			email:          "alice@sub.example.com",
			allowedDomains: []string{"example.com"},
			wantOK:         false,
		},
		{
			name:           "empty local part still matches on domain",
			email:          "@example.com",
			allowedDomains: []string{"example.com"},
			wantDomain:     "example.com",
			wantOK:         true,
		},
		{
			name:           "empty allowed_domains never matches",
			email:          "alice@example.com",
			allowedDomains: nil,
			wantOK:         false,
		},
		{
			name:           "no @ in email",
			email:          "not-an-email",
			allowedDomains: []string{"example.com"},
			wantOK:         false,
		},
		{
			name:           "@ is the last character",
			email:          "alice@",
			allowedDomains: []string{"example.com"},
			wantOK:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domain, ok := matchAllowedDomain(tt.email, tt.allowedDomains)
			if ok != tt.wantOK {
				t.Errorf("matchAllowedDomain(%q, %v) ok = %v, want %v", tt.email, tt.allowedDomains, ok, tt.wantOK)
			}
			if ok && domain != tt.wantDomain {
				t.Errorf("matchAllowedDomain(%q, %v) domain = %q, want %q", tt.email, tt.allowedDomains, domain, tt.wantDomain)
			}
		})
	}
}
