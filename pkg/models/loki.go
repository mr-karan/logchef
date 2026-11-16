package models

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type LokiAuth struct {
	Mode     string `json:"mode,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
}

type LokiLimits struct {
	MaxEntriesPerQuery     int `json:"max_entries_per_query,omitempty"`
	SidebarLookbackSeconds int `json:"sidebar_lookback_seconds,omitempty"`
	SchemaLookbackSeconds  int `json:"schema_lookback_seconds,omitempty"`
}

// LokiConnectionInfo is the connection_config of a Loki source (RFC 0011).
type LokiConnectionInfo struct {
	BaseURL   string            `json:"base_url"`
	Auth      LokiAuth          `json:"auth,omitempty"`
	TenantID  string            `json:"tenant_id,omitempty"`
	Selector  string            `json:"selector"`
	Principal string            `json:"principal,omitempty"`
	Parser    string            `json:"parser,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Limits    *LokiLimits       `json:"limits,omitempty"`
}

// LokiConnectionError names the connection field that failed validation,
// relative to the connection object, so a provider can report it under its own
// field prefix.
type LokiConnectionError struct {
	Field   string
	Message string
}

func (e *LokiConnectionError) Error() string {
	return e.Field + ": " + e.Message
}

const maxLokiTenantIDBytes = 150

var (
	lokiTenantIDPart = regexp.MustCompile(`^[A-Za-z0-9!\-_.*'()]+$`)
	lokiPrincipal    = regexp.MustCompile(`^[A-Za-z0-9\-_.]{1,64}$`)
	lokiLabelName    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Logchef sets these from the auth and tenant fields, so a custom header must
// not override them.
var lokiReservedHeaders = map[string]struct{}{
	"authorization": {},
	"x-scope-orgid": {},
}

// WithDefaults fills every unset limit with its default.
func (l LokiLimits) WithDefaults() LokiLimits {
	if l.MaxEntriesPerQuery == 0 {
		l.MaxEntriesPerQuery = 5000
	}
	if l.SidebarLookbackSeconds == 0 {
		l.SidebarLookbackSeconds = 900
	}
	if l.SchemaLookbackSeconds == 0 {
		l.SchemaLookbackSeconds = 3600
	}
	return l
}

// EffectiveLimits returns the configured limits with defaults applied.
func (c LokiConnectionInfo) EffectiveLimits() LokiLimits {
	if c.Limits == nil {
		return LokiLimits{}.WithDefaults()
	}
	return c.Limits.WithDefaults()
}

// Validate checks the connection fields without contacting Loki.
func (c LokiConnectionInfo) Validate() error {
	if err := validateLokiBaseURL(c.BaseURL); err != nil {
		return err
	}
	if err := c.Auth.validate(); err != nil {
		return err
	}
	if err := validateLokiTenantID(c.TenantID); err != nil {
		return err
	}
	if strings.TrimSpace(c.Selector) == "" {
		return &LokiConnectionError{Field: "selector", Message: "selector is required, such as {service_name=~\".+\"}"}
	}
	if err := validateLokiSelector(c.Selector); err != nil {
		return &LokiConnectionError{Field: "selector", Message: err.Error()}
	}
	if c.Principal != "" && !lokiPrincipal.MatchString(c.Principal) {
		return &LokiConnectionError{Field: "principal", Message: "principal must be at most 64 letters, digits, '-', '_' or '.'"}
	}
	switch c.Parser {
	case "", "json", "logfmt":
	default:
		return &LokiConnectionError{Field: "parser", Message: `parser must be empty, "json" or "logfmt"`}
	}
	for key := range c.Headers {
		if _, reserved := lokiReservedHeaders[strings.ToLower(strings.TrimSpace(key))]; reserved {
			return &LokiConnectionError{
				Field:   "headers",
				Message: fmt.Sprintf("custom header %q is reserved and set from the auth and tenant fields; remove it", strings.TrimSpace(key)),
			}
		}
	}
	return c.EffectiveLimits().validate()
}

func validateLokiBaseURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return &LokiConnectionError{Field: "base_url", Message: "base_url is required"}
	}
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil {
		return &LokiConnectionError{Field: "base_url", Message: "base_url must be a valid URL"}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return &LokiConnectionError{Field: "base_url", Message: "base_url must use http or https"}
	}
	if parsed.Host == "" {
		return &LokiConnectionError{Field: "base_url", Message: "base_url must include a host"}
	}
	// The base URL is part of the identity key and of every source response,
	// so it must not carry a credential.
	if parsed.User != nil {
		return &LokiConnectionError{Field: "base_url", Message: "base_url must not hold credentials; set them in auth"}
	}
	return nil
}

// Unlike VictoriaLogs, an unset mode means "none": there is no auto-detect.
func (a LokiAuth) validate() error {
	switch strings.ToLower(strings.TrimSpace(a.Mode)) {
	case "", "none":
	case "basic":
		if strings.TrimSpace(a.Username) == "" {
			return &LokiConnectionError{Field: "auth.username", Message: "username is required for basic auth"}
		}
		if strings.TrimSpace(a.Password) == "" {
			return &LokiConnectionError{Field: "auth.password", Message: "password is required for basic auth"}
		}
	case "bearer":
		if strings.TrimSpace(a.Token) == "" {
			return &LokiConnectionError{Field: "auth.token", Message: "token is required for bearer auth"}
		}
	default:
		return &LokiConnectionError{Field: "auth.mode", Message: "auth.mode must be one of none, basic or bearer"}
	}
	return nil
}

// validateLokiTenantID applies Loki's tenant ID rules to each tenant of a
// pipe-separated multi-tenant value.
func validateLokiTenantID(tenantID string) error {
	if tenantID == "" {
		return nil
	}
	if len(tenantID) > maxLokiTenantIDBytes {
		return &LokiConnectionError{Field: "tenant_id", Message: fmt.Sprintf("tenant_id must be at most %d bytes", maxLokiTenantIDBytes)}
	}
	for part := range strings.SplitSeq(tenantID, "|") {
		if !lokiTenantIDPart.MatchString(part) || part == "." || part == ".." {
			return &LokiConnectionError{
				Field:   "tenant_id",
				Message: "tenant_id must be letters, digits and !-_.*'(), with a pipe between tenant IDs",
			}
		}
	}
	return nil
}

func (l LokiLimits) validate() error {
	if l.MaxEntriesPerQuery < 100 || l.MaxEntriesPerQuery > 50000 {
		return &LokiConnectionError{Field: "limits.max_entries_per_query", Message: "max_entries_per_query must be between 100 and 50000"}
	}
	if l.SidebarLookbackSeconds < 60 || l.SidebarLookbackSeconds > 86400 {
		return &LokiConnectionError{Field: "limits.sidebar_lookback_seconds", Message: "sidebar_lookback_seconds must be between 60 and 86400"}
	}
	if l.SchemaLookbackSeconds < 60 || l.SchemaLookbackSeconds > 86400 {
		return &LokiConnectionError{Field: "limits.schema_lookback_seconds", Message: "schema_lookback_seconds must be between 60 and 86400"}
	}
	return nil
}

type lokiMatcher struct {
	name  string
	op    string
	value string
}

// matchesEmpty reports whether the matcher selects a stream that lacks the
// label. Loki rejects a selector whose every matcher does.
func (m lokiMatcher) matchesEmpty() bool {
	switch m.op {
	case "=":
		return m.value == ""
	case "!=":
		return m.value != ""
	case "=~":
		return regexp.MustCompile("^(?:" + m.value + ")$").MatchString("")
	default:
		return !regexp.MustCompile("^(?:" + m.value + ")$").MatchString("")
	}
}

// validateLokiSelector accepts exactly one LogQL stream selector, such as
// {namespace="payments", app=~"api|web"}, with a matcher that does not match
// the empty string.
func validateLokiSelector(selector string) error {
	matchers, err := parseLokiSelector(selector)
	if err != nil {
		return err
	}
	for _, m := range matchers {
		if !m.matchesEmpty() {
			return nil
		}
	}
	return fmt.Errorf("selector needs a matcher that does not match the empty string, such as {service_name=~\".+\"}")
}

func parseLokiSelector(selector string) ([]lokiMatcher, error) {
	s := strings.TrimSpace(selector)
	if !strings.HasPrefix(s, "{") {
		return nil, fmt.Errorf("selector must start with '{'")
	}
	s = strings.TrimLeft(s[1:], " \t\r\n")
	var matchers []lokiMatcher
	for {
		m, rest, err := parseLokiMatcher(s)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, m)
		rest = strings.TrimLeft(rest, " \t\r\n")
		switch {
		case strings.HasPrefix(rest, ","):
			s = strings.TrimLeft(rest[1:], " \t\r\n")
		case rest == "}":
			return matchers, nil
		case strings.HasPrefix(rest, "}"):
			return nil, fmt.Errorf("selector must be one stream selector with nothing after '}'")
		default:
			return nil, fmt.Errorf("expected ',' or '}' after matcher %s", m.name)
		}
	}
}

func parseLokiMatcher(s string) (lokiMatcher, string, error) {
	end := strings.IndexAny(s, "=!")
	if end <= 0 {
		return lokiMatcher{}, "", fmt.Errorf("expected a matcher such as app=\"api\"")
	}
	name := strings.TrimRight(s[:end], " \t\r\n")
	if !lokiLabelName.MatchString(name) {
		return lokiMatcher{}, "", fmt.Errorf("invalid label name %q", name)
	}
	s = s[end:]
	var op string
	for _, candidate := range []string{"=~", "!~", "!=", "="} {
		if strings.HasPrefix(s, candidate) {
			op = candidate
			break
		}
	}
	if op == "" {
		return lokiMatcher{}, "", fmt.Errorf("label %s needs one of =, !=, =~, !~", name)
	}
	s = strings.TrimLeft(s[len(op):], " \t\r\n")
	value, rest, err := parseLokiString(s)
	if err != nil {
		return lokiMatcher{}, "", fmt.Errorf("label %s: %w", name, err)
	}
	if op == "=~" || op == "!~" {
		if _, err := regexp.Compile("^(?:" + value + ")$"); err != nil {
			return lokiMatcher{}, "", fmt.Errorf("label %s: invalid regex: %w", name, err)
		}
	}
	return lokiMatcher{name: name, op: op, value: value}, rest, nil
}

// parseLokiString reads a double-quoted string with Go escapes or a backtick
// raw string, the two string forms of LogQL.
func parseLokiString(s string) (value, rest string, err error) {
	if s == "" {
		return "", "", fmt.Errorf("expected a quoted value")
	}
	switch s[0] {
	case '`':
		end := strings.IndexByte(s[1:], '`')
		if end < 0 {
			return "", "", fmt.Errorf("unterminated raw string")
		}
		return s[1 : end+1], s[end+2:], nil
	case '"':
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				i++
			case '"':
				value, err := strconv.Unquote(s[:i+1])
				if err != nil {
					return "", "", fmt.Errorf("invalid string %s", s[:i+1])
				}
				return value, s[i+1:], nil
			}
		}
		return "", "", fmt.Errorf("unterminated string")
	default:
		return "", "", fmt.Errorf("expected a quoted value")
	}
}

func BuildLokiIdentityKey(conn LokiConnectionInfo) string {
	return fmt.Sprintf(
		"%s:%s|tenant=%s|selector=%s|principal=%s",
		SourceTypeLoki,
		normalizeHTTPBaseURL(conn.BaseURL),
		strings.TrimSpace(conn.TenantID),
		strings.TrimSpace(conn.Selector),
		strings.TrimSpace(conn.Principal),
	)
}
