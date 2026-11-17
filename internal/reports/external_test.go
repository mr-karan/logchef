package reports

import (
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestParseExternalAddress(t *testing.T) {
	longest := strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 58) + ".io"
	if len(longest) != MaxAddressBytes {
		t.Fatalf("fixture is %d bytes, want %d", len(longest), MaxAddressBytes)
	}
	for _, tc := range []struct{ in, want string }{
		{"oncall@example.com", "oncall@example.com"},
		{"On.Call+x@Example.COM", "On.Call+x@example.com"},
		{longest, longest},
	} {
		got, err := ParseExternalAddress(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseExternalAddress(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{
		"",
		longest + "x",
		"no-at-sign",
		"Name <a@example.com>",
		"<a@example.com>",
		"a@example.com (comment)",
		"a@example.com\r\nBcc: b@example.com",
		"a@example.com,b@example.com",
		" a@example.com",
		"a@localhost",
		"a@[192.0.2.1]",
		"a@-example.com",
		"a@exa_mple.com",
		"ü@example.com",
		"a@bücher.example",
	} {
		if got, err := ParseExternalAddress(in); err == nil {
			t.Errorf("ParseExternalAddress(%q) = %q, want an error", in, got)
		}
	}
}

// TestLongestExternalAddressFitsTheHeader sends to the longest address the
// editor accepts: every header line stays within SMTP's 998 bytes.
func TestLongestExternalAddressFitsTheHeader(t *testing.T) {
	addr, err := ParseExternalAddress(strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 58) + ".io")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := BuildMessage(Message{
		From: mail.Address{Address: "logchef@example.com"}, To: mail.Address{Address: addr},
		Subject: Subject(strings.Repeat("日", 200), time.Unix(0, 0), time.UTC), Date: time.Unix(0, 0), Text: "t", HTML: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(raw), "\r\n\r\n")
	for line := range strings.SplitSeq(head, "\r\n") {
		if len(line) > 998 {
			t.Errorf("a header line is %d bytes, over the SMTP limit of 998", len(line))
		}
	}
	if !strings.Contains(head, "To: <"+addr+">") {
		t.Errorf("the To header does not carry the address as it is:\n%s", head)
	}
}

func TestParseAllowedDomains(t *testing.T) {
	for in, want := range map[string]string{
		"":                               "",
		"  ":                             "",
		"example.com":                    "example.com",
		" Example.com , partner.co.uk ":  "example.com,partner.co.uk",
		"*":                              "*",
		"example.com,*":                  "example.com,*",
		"example.com,EXAMPLE.com":        "example.com",
		"xn--bcher-kva.example,a-b.test": "xn--bcher-kva.example,a-b.test",
	} {
		got, err := ParseAllowedDomains(in)
		if err != nil || strings.Join(got, ",") != want {
			t.Errorf("ParseAllowedDomains(%q) = %v, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"example.com,", ",example.com", "@example.com", "*.example.com", "localhost", "a@example.com", "example .com"} {
		if got, err := ParseAllowedDomains(in); err == nil {
			t.Errorf("ParseAllowedDomains(%q) = %v, want an error", in, got)
		}
	}
}

func TestExternalPolicyAllows(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy ExternalPolicy
		addr   string
		want   string
	}{
		{"off", ExternalPolicy{Domains: []string{AllDomains}}, "a@example.com", SkipExternalDisabled},
		{"on with no domain", ExternalPolicy{Enabled: true}, "a@example.com", SkipDomainNotAllowed},
		{"listed domain", ExternalPolicy{Enabled: true, Domains: []string{"example.com"}}, "a@Example.com", ""},
		{"subdomain of a listed domain", ExternalPolicy{Enabled: true, Domains: []string{"example.com"}}, "a@mail.example.com", SkipDomainNotAllowed},
		{"other domain", ExternalPolicy{Enabled: true, Domains: []string{"example.com"}}, "a@example.org", SkipDomainNotAllowed},
		{"every domain", ExternalPolicy{Enabled: true, Domains: []string{AllDomains}}, "a@example.org", ""},
	} {
		if got := tc.policy.Allows(tc.addr); got != tc.want {
			t.Errorf("%s: Allows(%q) = %q, want %q", tc.name, tc.addr, got, tc.want)
		}
	}
}
