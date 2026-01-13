package alerts

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// AlertPayload represents the data sent to Alertmanager's /api/v2/alerts endpoint.
type AlertPayload struct {
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       *time.Time        `json:"endsAt,omitempty"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
}

// ClientOptions configures the Alertmanager client.
type ClientOptions struct {
	BaseURL           string
	Timeout           time.Duration
	SkipTLSVerify     bool
	Logger            *slog.Logger
	AdditionalHeaders http.Header
	MaxRetries        int           // Maximum number of retry attempts (default: 2)
	RetryDelay        time.Duration // Initial retry delay (default: 500ms)
}

// Client sends alerts to an Alertmanager instance.
type Client struct {
	baseURL    string
	client     *http.Client
	log        *slog.Logger
	headers    http.Header
	maxRetries int
	retryDelay time.Duration
}

// NewAlertmanagerClient constructs a new Alertmanager client with sane defaults.
func NewAlertmanagerClient(opts ClientOptions) (*Client, error) {
	baseURL := strings.TrimSpace(opts.BaseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("alertmanager base URL is required")
	}

	// Ensure the URL ends with the correct API endpoint
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/api/v2/alerts") && !strings.HasSuffix(baseURL, "/api/v1/alerts") {
		baseURL += "/api/v2/alerts"
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if opts.SkipTLSVerify {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 - intentionally configurable
		} else {
			transport.TLSClientConfig.InsecureSkipVerify = true // #nosec G402
		}
	}

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	headers := make(http.Header)
	for k, values := range opts.AdditionalHeaders {
		for _, v := range values {
			headers.Add(k, v)
		}
	}

	maxRetries := opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 2 // Default to 2 retries
	}

	retryDelay := opts.RetryDelay
	if retryDelay <= 0 {
		retryDelay = 500 * time.Millisecond // Default initial delay
	}

	return &Client{
		baseURL:    baseURL,
		client:     httpClient,
		log:        logger.With("component", "alertmanager_client"),
		headers:    headers,
		maxRetries: maxRetries,
		retryDelay: retryDelay,
	}, nil
}

// Send publishes the provided alerts to Alertmanager with retry logic.
func (c *Client) Send(ctx context.Context, alerts []AlertPayload) error {
	if len(alerts) == 0 {
		return nil
	}

	body, err := json.Marshal(alerts)
	if err != nil {
		return fmt.Errorf("failed to marshal alert payload: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: delay * 2^(attempt-1)
			// #nosec G115 -- attempt is always > 0 here, so attempt-1 >= 0
			delay := c.retryDelay * time.Duration(1<<uint(attempt-1))
			c.log.Warn("retrying alertmanager request", "attempt", attempt, "delay", delay, "error", lastErr)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to create alertmanager request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		for k, values := range c.headers {
			for _, v := range values {
				req.Header.Add(k, v)
			}
		}

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to send alerts to Alertmanager: %w", err)
			continue // Retry on network errors
		}

		// Read and close body
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil // Success
		}

		// Check if error is retryable (5xx errors)
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("alertmanager returned server error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			continue // Retry on 5xx errors
		}

		// 4xx errors are not retryable
		if readErr != nil {
			return fmt.Errorf("alertmanager returned status %d (body read error: %w)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("alertmanager returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return fmt.Errorf("alertmanager request failed after %d retries: %w", c.maxRetries, lastErr)
}

// AlertmanagerStatus represents the response from /api/v2/status endpoint.
type AlertmanagerStatus struct {
	Config struct {
		Original string `json:"original"`
	} `json:"config"`
}

// AlertmanagerConfig represents the parsed Alertmanager configuration.
type AlertmanagerConfig struct {
	Route     *RouteConfig     `yaml:"route" json:"route"`
	Receivers []ReceiverConfig `yaml:"receivers" json:"receivers"`
}

// RouteConfig represents a routing rule in Alertmanager.
type RouteConfig struct {
	Receiver       string            `yaml:"receiver" json:"receiver"`
	GroupBy        []string          `yaml:"group_by,omitempty" json:"group_by,omitempty"`
	GroupWait      string            `yaml:"group_wait,omitempty" json:"group_wait,omitempty"`
	GroupInterval  string            `yaml:"group_interval,omitempty" json:"group_interval,omitempty"`
	RepeatInterval string            `yaml:"repeat_interval,omitempty" json:"repeat_interval,omitempty"`
	Matchers       []string          `yaml:"matchers,omitempty" json:"matchers,omitempty"`
	Match          map[string]string `yaml:"match,omitempty" json:"match,omitempty"`
	MatchRE        map[string]string `yaml:"match_re,omitempty" json:"match_re,omitempty"`
	Continue       bool              `yaml:"continue,omitempty" json:"continue,omitempty"`
	Routes         []RouteConfig     `yaml:"routes,omitempty" json:"routes,omitempty"`
}

// ReceiverConfig represents a receiver definition in Alertmanager.
type ReceiverConfig struct {
	Name string `yaml:"name" json:"name"`
}

// RoutingInfo provides a simplified view of routing rules for the UI.
type RoutingInfo struct {
	Receivers     []string       `json:"receivers"`
	DefaultRoute  string         `json:"default_route"`
	RoutingRules  []RoutingRule  `json:"routing_rules"`
	CommonLabels  []string       `json:"common_labels"`
	LabelExamples []LabelExample `json:"label_examples"`
}

// RoutingRule represents a simplified routing rule for the UI.
type RoutingRule struct {
	Receiver string            `json:"receiver"`
	Matchers map[string]string `json:"matchers"`
	Priority int               `json:"priority"`
}

// LabelExample provides example label combinations for routing to a specific receiver.
type LabelExample struct {
	Receiver    string            `json:"receiver"`
	Labels      map[string]string `json:"labels"`
	Description string            `json:"description"`
}

// GetRoutingConfig fetches and parses the Alertmanager routing configuration.
func (c *Client) GetRoutingConfig(ctx context.Context) (*RoutingInfo, error) {
	statusURL := strings.Replace(c.baseURL, "/api/v2/alerts", "/api/v2/status", 1)
	statusURL = strings.Replace(statusURL, "/api/v1/alerts", "/api/v1/status", 1)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create status request: %w", err)
	}

	for k, values := range c.headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch alertmanager status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, fmt.Errorf("alertmanager returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var status AlertmanagerStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode alertmanager status: %w", err)
	}

	// Parse the YAML configuration
	var config AlertmanagerConfig
	if err := yaml.Unmarshal([]byte(status.Config.Original), &config); err != nil {
		return nil, fmt.Errorf("failed to parse alertmanager config: %w", err)
	}

	return c.buildRoutingInfo(&config), nil
}

// buildRoutingInfo converts the AlertmanagerConfig to a simplified RoutingInfo for the UI.
func (c *Client) buildRoutingInfo(config *AlertmanagerConfig) *RoutingInfo {
	info := &RoutingInfo{
		Receivers:     make([]string, 0, len(config.Receivers)),
		RoutingRules:  make([]RoutingRule, 0),
		LabelExamples: make([]LabelExample, 0),
	}

	// Extract receiver names (exclude "blackhole" type receivers)
	for _, r := range config.Receivers {
		if r.Name != "" && r.Name != "blackhole" {
			info.Receivers = append(info.Receivers, r.Name)
		}
	}

	// Set default route
	if config.Route != nil {
		info.DefaultRoute = config.Route.Receiver
	}

	// Track common labels used in matchers
	labelUsage := make(map[string]int)

	// Extract routing rules recursively
	if config.Route != nil {
		c.extractRoutingRules(config.Route.Routes, info, labelUsage, 0)
	}

	// Determine common labels (sorted by usage)
	for label, count := range labelUsage {
		if count > 0 {
			info.CommonLabels = append(info.CommonLabels, label)
		}
	}

	// Generate label examples for each receiver
	info.LabelExamples = c.generateLabelExamples(info.RoutingRules)

	return info
}

// extractRoutingRules recursively extracts routing rules from the route tree.
func (c *Client) extractRoutingRules(routes []RouteConfig, info *RoutingInfo, labelUsage map[string]int, depth int) {
	for i := range routes {
		route := &routes[i]
		matchers := make(map[string]string)

		// Parse matchers in the new format (e.g., "room=~\"kite.*\"")
		for _, m := range route.Matchers {
			key, value := parseMatcherString(m)
			if key != "" {
				matchers[key] = value
				labelUsage[key]++
			}
		}

		// Also handle old-style match/match_re
		for k, v := range route.Match {
			matchers[k] = v
			labelUsage[k]++
		}
		for k, v := range route.MatchRE {
			matchers[k] = "~" + v // Indicate regex with ~ prefix
			labelUsage[k]++
		}

		if len(matchers) > 0 && route.Receiver != "" && route.Receiver != "blackhole" {
			info.RoutingRules = append(info.RoutingRules, RoutingRule{
				Receiver: route.Receiver,
				Matchers: matchers,
				Priority: depth,
			})
		}

		// Recurse into child routes
		if len(route.Routes) > 0 {
			c.extractRoutingRules(route.Routes, info, labelUsage, depth+1)
		}
	}
}

// parseMatcherString parses a matcher string like 'room=~"kite.*"' into key and value.
func parseMatcherString(matcher string) (key, value string) {
	// Handle regex matchers: key=~"value" or key=~value
	if idx := strings.Index(matcher, "=~"); idx > 0 {
		key = strings.TrimSpace(matcher[:idx])
		value = strings.Trim(strings.TrimSpace(matcher[idx+2:]), "\"")
		return key, "~" + value // Prefix with ~ to indicate regex
	}

	// Handle negated regex matchers: key!~"value"
	if idx := strings.Index(matcher, "!~"); idx > 0 {
		key = strings.TrimSpace(matcher[:idx])
		value = strings.Trim(strings.TrimSpace(matcher[idx+2:]), "\"")
		return key, "!~" + value
	}

	// Handle exact matchers: key="value" or key=value
	if idx := strings.Index(matcher, "="); idx > 0 {
		key = strings.TrimSpace(matcher[:idx])
		value = strings.Trim(strings.TrimSpace(matcher[idx+1:]), "\"")
		return key, value
	}

	return "", ""
}

// generateLabelExamples creates simple label examples for each receiver.
func (c *Client) generateLabelExamples(rules []RoutingRule) []LabelExample {
	examples := make([]LabelExample, 0)
	receiverExamples := make(map[string]bool)

	// First pass: find the simplest route to each receiver (fewest matchers)
	for _, rule := range rules {
		if receiverExamples[rule.Receiver] {
			continue // Already have an example for this receiver
		}

		// Convert matchers to clean labels (remove regex prefix for display)
		labels := make(map[string]string)
		for k, v := range rule.Matchers {
			// Clean up regex prefix for display
			cleanValue := strings.TrimPrefix(strings.TrimPrefix(v, "~"), "!~")
			labels[k] = cleanValue
		}

		if len(labels) > 0 {
			examples = append(examples, LabelExample{
				Receiver:    rule.Receiver,
				Labels:      labels,
				Description: fmt.Sprintf("Route to %s", rule.Receiver),
			})
			receiverExamples[rule.Receiver] = true
		}
	}

	return examples
}

// HealthCheck verifies connectivity to the Alertmanager instance.
// It makes a GET request to the /api/v2/status endpoint to check if Alertmanager is reachable.
func (c *Client) HealthCheck(ctx context.Context) error {
	// Use the status endpoint for health check
	statusURL := strings.Replace(c.baseURL, "/api/v2/alerts", "/api/v2/status", 1)
	statusURL = strings.Replace(statusURL, "/api/v1/alerts", "/api/v1/status", 1)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	for k, values := range c.headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Alertmanager: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error details
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<10))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.log.Info("alertmanager health check successful", "status_code", resp.StatusCode)
		return nil
	}

	if readErr != nil {
		return fmt.Errorf("alertmanager health check failed with status %d (body read error: %w)", resp.StatusCode, readErr)
	}

	return fmt.Errorf("alertmanager health check failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}
