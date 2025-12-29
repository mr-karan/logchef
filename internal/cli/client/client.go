// Package client provides the HTTP client for LogChef API communication.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/cli/config"
)

// Client is the LogChef API client
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New creates a new LogChef API client
func New(cfg *config.Config) (*Client, error) {
	if cfg.Server.URL == "" {
		return nil, fmt.Errorf("server URL is required")
	}

	// Ensure URL doesn't have trailing slash
	baseURL := strings.TrimSuffix(cfg.Server.URL, "/")

	return &Client{
		baseURL: baseURL,
		token:   cfg.Auth.Token,
		httpClient: &http.Client{
			Timeout: cfg.Server.Timeout,
		},
	}, nil
}

// SetToken sets the authentication token
func (c *Client) SetToken(token string) {
	c.token = token
}

// Request options
type RequestOptions struct {
	Method  string
	Path    string
	Query   url.Values
	Body    any
	Timeout time.Duration
}

// APIError represents an error response from the API
type APIError struct {
	Status     string `json:"status"`
	Message    string `json:"message"`
	ErrorType  string `json:"error_type,omitempty"`
	StatusCode int    `json:"-"`
}

func (e *APIError) Error() string {
	if e.ErrorType != "" {
		return fmt.Sprintf("%s: %s", e.ErrorType, e.Message)
	}
	return e.Message
}

// Do performs an HTTP request to the LogChef API
func (c *Client) Do(ctx context.Context, opts RequestOptions) (*http.Response, error) {
	// Build URL
	reqURL, err := url.Parse(c.baseURL + opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if opts.Query != nil {
		reqURL.RawQuery = opts.Query.Encode()
	}

	var body io.Reader
	if opts.Body != nil {
		data, err := json.Marshal(opts.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, opts.Method, reqURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "logchef-cli/1.0")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// DoJSON performs a request and decodes the JSON response
func (c *Client) DoJSON(ctx context.Context, opts RequestOptions, result any) error {
	resp, err := c.Do(ctx, opts)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr APIError
		if err := json.Unmarshal(respBody, &apiErr); err != nil {
			return &APIError{
				Status:     "error",
				Message:    string(respBody),
				StatusCode: resp.StatusCode,
			}
		}
		apiErr.StatusCode = resp.StatusCode
		return &apiErr
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// --- API Methods ---

// CurrentUser returns the current authenticated user
type CurrentUserResponse struct {
	Status string `json:"status"`
	Data   *User  `json:"data"`
}

type User struct {
	ID       int    `json:"id"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
}

func (c *Client) CurrentUser(ctx context.Context) (*User, error) {
	var resp CurrentUserResponse
	err := c.DoJSON(ctx, RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/v1/me",
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// ListTeams returns the user's teams
type TeamsResponse struct {
	Status string `json:"status"`
	Data   []Team `json:"data"`
}

type Team struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Role        string `json:"role,omitempty"`
	MemberCount int    `json:"member_count,omitempty"`
}

func (c *Client) ListTeams(ctx context.Context) ([]Team, error) {
	var resp TeamsResponse
	err := c.DoJSON(ctx, RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/v1/me/teams",
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// ListSources returns sources for a team
type SourcesResponse struct {
	Status string   `json:"status"`
	Data   []Source `json:"data"`
}

type Source struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Database    string `json:"database,omitempty"`
	TableName   string `json:"table_name,omitempty"`
	IsConnected bool   `json:"is_connected"`
}

func (c *Client) ListSources(ctx context.Context, teamID int) ([]Source, error) {
	var resp SourcesResponse
	err := c.DoJSON(ctx, RequestOptions{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/api/v1/teams/%d/sources", teamID),
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetSchema returns the schema for a source
type SchemaResponse struct {
	Status string   `json:"status"`
	Data   []Column `json:"data"`
}

type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func (c *Client) GetSchema(ctx context.Context, teamID, sourceID int) ([]Column, error) {
	var resp SchemaResponse
	err := c.DoJSON(ctx, RequestOptions{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/api/v1/teams/%d/sources/%d/schema", teamID, sourceID),
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// Query executes a LogChefQL query
type QueryRequest struct {
	Query        string `json:"query"`
	StartTime    string `json:"start_time"`
	EndTime      string `json:"end_time"`
	Timezone     string `json:"timezone,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	QueryTimeout int    `json:"query_timeout,omitempty"`
}

type QueryResponse struct {
	Logs         []map[string]any `json:"logs"`
	Columns      []Column         `json:"columns"`
	Stats        QueryStats       `json:"stats"`
	QueryID      string           `json:"query_id,omitempty"`
	GeneratedSQL string           `json:"generated_sql,omitempty"`
}

type QueryStats struct {
	ExecutionTimeMs int64 `json:"execution_time_ms"`
	RowsRead        int64 `json:"rows_read"`
	BytesRead       int64 `json:"bytes_read"`
}

type queryAPIResponse struct {
	Status string        `json:"status"`
	Data   QueryResponse `json:"data"`
}

func (c *Client) Query(ctx context.Context, teamID, sourceID int, req QueryRequest) (*QueryResponse, error) {
	var resp queryAPIResponse
	err := c.DoJSON(ctx, RequestOptions{
		Method: http.MethodPost,
		Path:   fmt.Sprintf("/api/v1/teams/%d/sources/%d/logchefql/query", teamID, sourceID),
		Body:   req,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// QuerySQL executes a raw SQL query
type SQLQueryRequest struct {
	RawSQL       string `json:"raw_sql"`
	Limit        int    `json:"limit,omitempty"`
	Timezone     string `json:"timezone,omitempty"`
	StartTime    string `json:"start_time,omitempty"`
	EndTime      string `json:"end_time,omitempty"`
	QueryTimeout int    `json:"query_timeout,omitempty"`
}

type SQLQueryResponse struct {
	Data    []map[string]any `json:"data"`
	Logs    []map[string]any `json:"logs"`
	Columns []Column         `json:"columns"`
	Stats   QueryStats       `json:"stats"`
	QueryID string           `json:"query_id,omitempty"`
}

type sqlQueryAPIResponse struct {
	Status string           `json:"status"`
	Data   SQLQueryResponse `json:"data"`
}

func (c *Client) QuerySQL(ctx context.Context, teamID, sourceID int, req SQLQueryRequest) (*SQLQueryResponse, error) {
	var resp sqlQueryAPIResponse
	err := c.DoJSON(ctx, RequestOptions{
		Method: http.MethodPost,
		Path:   fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs/query", teamID, sourceID),
		Body:   req,
	}, &resp)
	if err != nil {
		return nil, err
	}
	result := &resp.Data
	if result.Data == nil && result.Logs != nil {
		result.Data = result.Logs
	}
	return result, nil
}

// Translate translates a LogChefQL query to SQL
type TranslateRequest struct {
	Query     string `json:"query"`
	StartTime string `json:"start_time,omitempty"`
	EndTime   string `json:"end_time,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type TranslateResponse struct {
	SQL          string   `json:"sql"`
	FullSQL      string   `json:"full_sql,omitempty"`
	SelectClause string   `json:"select_clause,omitempty"`
	Valid        bool     `json:"valid"`
	Error        *Error   `json:"error,omitempty"`
	Conditions   []any    `json:"conditions"`
	FieldsUsed   []string `json:"fields_used"`
}

type Error struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Position *struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	} `json:"position,omitempty"`
}

type translateAPIResponse struct {
	Status string            `json:"status"`
	Data   TranslateResponse `json:"data"`
}

func (c *Client) Translate(ctx context.Context, teamID, sourceID int, req TranslateRequest) (*TranslateResponse, error) {
	var resp translateAPIResponse
	err := c.DoJSON(ctx, RequestOptions{
		Method: http.MethodPost,
		Path:   fmt.Sprintf("/api/v1/teams/%d/sources/%d/logchefql/translate", teamID, sourceID),
		Body:   req,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// GetFieldValues returns distinct values for a field
type FieldValuesResponse struct {
	Status string   `json:"status"`
	Data   []string `json:"data"`
}

func (c *Client) GetFieldValues(ctx context.Context, teamID, sourceID int, fieldName string) ([]string, error) {
	var resp FieldValuesResponse
	err := c.DoJSON(ctx, RequestOptions{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/api/v1/teams/%d/sources/%d/fields/%s/values", teamID, sourceID, url.PathEscape(fieldName)),
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// CancelQuery cancels a running query
func (c *Client) CancelQuery(ctx context.Context, teamID, sourceID int, queryID string) error {
	return c.DoJSON(ctx, RequestOptions{
		Method: http.MethodPost,
		Path:   fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs/query/%s/cancel", teamID, sourceID, queryID),
		Body:   struct{}{},
	}, nil)
}
