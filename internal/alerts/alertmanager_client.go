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
		baseURL = baseURL + "/api/v2/alerts"
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

// HealthCheck verifies connectivity to the Alertmanager instance.
// It makes a GET request to the /api/v2/status endpoint to check if Alertmanager is reachable.
func (c *Client) HealthCheck(ctx context.Context) error {
	// Use the status endpoint for health check
	statusURL := strings.Replace(c.baseURL, "/api/v2/alerts", "/api/v2/status", 1)
	statusURL = strings.Replace(statusURL, "/api/v1/alerts", "/api/v1/status", 1)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
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
