package clickhouse

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"backend-v2/pkg/models"
)

// HTTPClient represents a ClickHouse HTTP client
type HTTPClient struct {
	baseURL  string
	client   *http.Client
	settings HTTPSettings
	log      *slog.Logger
}

// JSONCompactResponse represents the ClickHouse JSONCompact response format
type JSONCompactResponse struct {
	Meta []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"meta"`
	Data       [][]interface{} `json:"data"`
	Rows       int             `json:"rows"`
	Statistics struct {
		Elapsed   float64 `json:"elapsed"`
		RowsRead  int     `json:"rows_read"`
		BytesRead int     `json:"bytes_read"`
	} `json:"statistics"`
}

// NewHTTPClient creates a new ClickHouse HTTP client
func NewHTTPClient(addr string, settings HTTPSettings, log *slog.Logger) *HTTPClient {
	transport := &http.Transport{
		MaxIdleConns:       defaultMaxIdleConns,
		MaxConnsPerHost:    defaultMaxConnsPerHost,
		IdleConnTimeout:    defaultIdleConnTimeout,
		DisableCompression: !settings.EnableCompression, // Let client handle compression
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   defaultHTTPTimeout,
	}

	return &HTTPClient{
		baseURL:  fmt.Sprintf("http://%s", addr),
		client:   client,
		settings: settings,
		log:      log,
	}
}

// buildURL constructs the URL with query parameters
func (c *HTTPClient) buildURL(query string, extraParams map[string]string) string {
	params := url.Values{}

	// Add query
	params.Add("query", query)

	// Add settings
	params.Add("max_memory_usage", c.settings.MaxMemoryUsage)
	params.Add("max_execution_time", c.settings.MaxExecutionTime)
	params.Add("wait_end_of_query", c.settings.WaitEndOfQuery)
	params.Add("buffer_size", c.settings.BufferSize)

	// Add compression settings
	if c.settings.EnableCompression {
		params.Add("enable_http_compression", "1")
		params.Add("http_zlib_compression_level", c.settings.ZlibLevel)
	}

	// Add format if not already in query
	if !strings.Contains(strings.ToLower(query), "format") {
		params.Add("default_format", c.settings.Format)
	}

	// Add any extra parameters
	for k, v := range extraParams {
		params.Add(k, v)
	}

	return fmt.Sprintf("%s/?%s", c.baseURL, params.Encode())
}

// Query executes a query and returns the response
func (c *HTTPClient) Query(ctx context.Context, query string, extraParams map[string]string) (*JSONCompactResponse, error) {
	// Use POST for modifying queries (CREATE, INSERT, ALTER, etc)
	method := "GET"
	var body io.Reader
	isModifying := isModifyingQuery(query)
	if isModifying {
		method = "POST"
		body = strings.NewReader(query)
	}

	// Build URL
	var urlStr string
	if isModifying {
		// For POST requests, don't include query in URL
		params := make(url.Values)
		params.Add("max_memory_usage", c.settings.MaxMemoryUsage)
		params.Add("max_execution_time", c.settings.MaxExecutionTime)
		params.Add("wait_end_of_query", c.settings.WaitEndOfQuery)
		params.Add("buffer_size", c.settings.BufferSize)

		if c.settings.EnableCompression {
			params.Add("enable_http_compression", "1")
			params.Add("http_zlib_compression_level", c.settings.ZlibLevel)
		}

		if !strings.Contains(strings.ToLower(query), "format") {
			params.Add("default_format", c.settings.Format)
		}

		for k, v := range extraParams {
			params.Add(k, v)
		}

		urlStr = fmt.Sprintf("%s/?%s", c.baseURL, params.Encode())
	} else {
		urlStr = c.buildURL(query, extraParams)
	}

	c.log.Debug("executing clickhouse query",
		"method", method,
		"url", urlStr,
		"query_length", len(query),
		"compression_enabled", c.settings.EnableCompression,
		"compression_method", c.settings.CompressionMethod,
	)

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add compression headers if enabled
	if c.settings.EnableCompression {
		req.Header.Set("Accept-Encoding", c.settings.CompressionMethod)
	}

	// For POST requests, set content type
	if method == "POST" {
		req.Header.Set("Content-Type", "text/plain")
	}

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		c.log.Error("failed to execute request",
			"url", urlStr,
			"error", err,
		)
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	var reader io.Reader = resp.Body

	// Handle compressed response
	contentEncoding := resp.Header.Get("Content-Encoding")
	if contentEncoding != "" {
		var err error
		reader, err = getDecompressor(contentEncoding, resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to create decompressor: %w", err)
		}
		if closer, ok := reader.(io.Closer); ok {
			defer closer.Close()
		}
	}

	respBody, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check for ClickHouse error in response body
	if bytes.Contains(respBody, []byte("Code: ")) && bytes.Contains(respBody, []byte("DB::Exception")) {
		return nil, fmt.Errorf("clickhouse error: %s", string(respBody))
	}

	// For modifying queries, return empty response if successful
	if isModifying && len(respBody) == 0 {
		return &JSONCompactResponse{}, nil
	}

	// Parse response
	var result JSONCompactResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Check if the response contains an exception
		var errResp struct {
			Exception string `json:"exception"`
		}
		if jsonErr := json.Unmarshal(respBody, &errResp); jsonErr == nil && errResp.Exception != "" {
			return nil, fmt.Errorf("clickhouse error: %s", errResp.Exception)
		}
		return nil, fmt.Errorf("failed to parse response: %w, body: %s", err, string(respBody))
	}

	return &result, nil
}

// isModifyingQuery checks if the query modifies the database
func isModifyingQuery(query string) bool {
	q := strings.TrimSpace(strings.ToUpper(query))
	modifyingPrefixes := []string{
		"CREATE",
		"ALTER",
		"DROP",
		"INSERT",
		"UPDATE",
		"DELETE",
		"TRUNCATE",
		"RENAME",
		"OPTIMIZE",
		"SYSTEM",
	}

	for _, prefix := range modifyingPrefixes {
		if strings.HasPrefix(q, prefix) {
			return true
		}
	}
	return false
}

// getDecompressor returns a reader that decompresses the given encoding
func getDecompressor(encoding string, reader io.Reader) (io.Reader, error) {
	switch encoding {
	case "gzip":
		return gzip.NewReader(reader)
	// Add support for other compression methods as needed:
	// case "br":
	// case "deflate":
	// case "zstd":
	// etc.
	default:
		return nil, fmt.Errorf("unsupported compression encoding: %s", encoding)
	}
}

// CheckHealth performs a health check
func (c *HTTPClient) CheckHealth(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	_, err := c.Query(ctx, healthCheckQuery, nil)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}

// Close closes the HTTP client
func (c *HTTPClient) Close() error {
	c.client.CloseIdleConnections()
	return nil
}

// ConvertToMap converts JSONCompact array response to map format
func ConvertToMap(resp *JSONCompactResponse) []map[string]interface{} {
	result := make([]map[string]interface{}, len(resp.Data))

	// Create column name slice for faster lookups
	columnNames := make([]string, len(resp.Meta))
	for i, col := range resp.Meta {
		columnNames[i] = col.Name
	}

	// Convert each row
	for i, row := range resp.Data {
		rowMap := make(map[string]interface{})
		for j, value := range row {
			if j < len(columnNames) {
				rowMap[columnNames[j]] = value
			}
		}
		result[i] = rowMap
	}

	return result
}

// GetQueryStats returns query statistics
func GetQueryStats(resp *JSONCompactResponse) models.QueryStats {
	return models.QueryStats{
		ExecutionTimeMs: resp.Statistics.Elapsed * 1000, // Convert to milliseconds
		RowsRead:        resp.Statistics.RowsRead,
		BytesRead:       resp.Statistics.BytesRead,
	}
}

// GetColumns returns column information
func GetColumns(resp *JSONCompactResponse) []models.ColumnInfo {
	columns := make([]models.ColumnInfo, len(resp.Meta))
	for i, col := range resp.Meta {
		columns[i] = models.ColumnInfo{
			Name: col.Name,
			Type: col.Type,
		}
	}
	return columns
}
