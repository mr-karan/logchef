package victorialogs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/backends"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	DefaultQueryTimeout = 60
	MaxQueryTimeout     = 300
	DefaultLimit        = 1000
)

var _ backends.BackendClient = (*Client)(nil)

type Client struct {
	httpClient *http.Client
	baseURL    string
	accountID  string
	projectID  string
	logger     *slog.Logger
	mu         sync.Mutex
}

type ClientOptions struct {
	URL       string
	AccountID string
	ProjectID string
	Timeout   time.Duration
}

func NewClient(opts ClientOptions, logger *slog.Logger) (*Client, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("VictoriaLogs URL is required")
	}

	baseURL := strings.TrimSuffix(opts.URL, "/")

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	client := &Client{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		baseURL:   baseURL,
		accountID: opts.AccountID,
		projectID: opts.ProjectID,
		logger:    logger,
	}

	return client, nil
}

func (c *Client) Query(ctx context.Context, query string, timeoutSeconds *int) (*models.QueryResult, error) {
	return c.QueryWithLimit(ctx, query, 0, timeoutSeconds)
}

func (c *Client) QueryWithLimit(ctx context.Context, query string, limit int, timeoutSeconds *int) (*models.QueryResult, error) {
	start := time.Now()

	timeout := DefaultQueryTimeout
	if timeoutSeconds != nil {
		timeout = *timeoutSeconds
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("timeout", fmt.Sprintf("%ds", timeout))

	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	resp, err := c.doRequest(ctx, "POST", "/select/logsql/query", params)
	if err != nil {
		return nil, fmt.Errorf("query request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query failed with status %d: %s", resp.StatusCode, string(body))
	}

	result, err := parseJSONLResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing query response: %w", err)
	}

	stats := parseStatsFromHeaders(resp.Header)
	if stats.ExecutionTimeMs == 0 {
		stats.ExecutionTimeMs = float64(time.Since(start).Milliseconds())
	}
	if stats.RowsRead == 0 {
		stats.RowsRead = len(result.Logs)
	}

	return &models.QueryResult{
		Logs:    result.Logs,
		Columns: result.Columns,
		Stats:   stats,
	}, nil
}

func (c *Client) GetTableInfo(ctx context.Context, database, table string) (*backends.TableInfo, error) {
	params := url.Values{}
	params.Set("query", "*")
	params.Set("start", "1h")

	resp, err := c.doRequest(ctx, "GET", "/select/logsql/field_names", params)
	if err != nil {
		return nil, fmt.Errorf("field names request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("field names request failed with status %d: %s", resp.StatusCode, string(body))
	}

	fieldNames, err := parseFieldNamesResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing field names: %w", err)
	}

	return convertFieldNamesToTableInfo(fieldNames), nil
}

func (c *Client) GetHistogramData(ctx context.Context, tableName, timestampField string, params backends.HistogramParams) (*backends.HistogramResult, error) {
	query := params.Query
	if query == "" {
		query = "*"
	}

	reqParams := url.Values{}
	reqParams.Set("query", query)
	reqParams.Set("step", params.Window)

	if params.GroupBy != "" {
		reqParams.Set("field", params.GroupBy)
		reqParams.Set("fields_limit", "10")
	}

	if params.Timezone != "" && params.Timezone != "UTC" {
		offset := getTimezoneOffset(params.Timezone)
		if offset != "" {
			reqParams.Set("offset", offset)
		}
	}

	if params.TimeoutSeconds != nil && *params.TimeoutSeconds > 0 {
		reqParams.Set("timeout", fmt.Sprintf("%ds", *params.TimeoutSeconds))
	}

	resp, err := c.doRequest(ctx, "GET", "/select/logsql/hits", reqParams)
	if err != nil {
		return nil, fmt.Errorf("hits request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("hits request failed with status %d: %s", resp.StatusCode, string(body))
	}

	hitsResp, err := parseHitsResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing hits response: %w", err)
	}

	return convertHitsToHistogram(hitsResp, params.Window)
}

func (c *Client) GetSurroundingLogs(ctx context.Context, tableName, timestampField string, params backends.LogContextParams, timeoutSeconds *int) (*backends.LogContextResult, error) {
	timeout := DefaultQueryTimeout
	if timeoutSeconds != nil {
		timeout = *timeoutSeconds
	}

	result := &backends.LogContextResult{
		BeforeLogs: make([]map[string]any, 0),
		TargetLogs: make([]map[string]any, 0),
		AfterLogs:  make([]map[string]any, 0),
	}

	targetTimeStr := params.TargetTime.UTC().Format(time.RFC3339Nano)

	beforeOp := "<="
	if params.ExcludeBoundary {
		beforeOp = "<"
	}

	beforeQuery := fmt.Sprintf("_time:%s%s | sort by (_time desc) | limit %d",
		beforeOp, targetTimeStr, params.BeforeLimit)
	if params.BeforeOffset > 0 {
		beforeQuery += fmt.Sprintf(" | offset %d", params.BeforeOffset)
	}

	beforeParams := url.Values{}
	beforeParams.Set("query", beforeQuery)
	beforeParams.Set("timeout", fmt.Sprintf("%ds", timeout))

	beforeResp, err := c.doRequest(ctx, "POST", "/select/logsql/query", beforeParams)
	if err != nil {
		return nil, fmt.Errorf("before logs request failed: %w", err)
	}
	defer beforeResp.Body.Close()

	if beforeResp.StatusCode == http.StatusOK {
		beforeResult, err := parseJSONLResponse(beforeResp.Body)
		if err == nil {
			result.BeforeLogs = reverseSlice(beforeResult.Logs)
		}
	}

	afterQuery := fmt.Sprintf("_time:>%s | sort by (_time asc) | limit %d",
		targetTimeStr, params.AfterLimit)
	if params.AfterOffset > 0 {
		afterQuery += fmt.Sprintf(" | offset %d", params.AfterOffset)
	}

	afterParams := url.Values{}
	afterParams.Set("query", afterQuery)
	afterParams.Set("timeout", fmt.Sprintf("%ds", timeout))

	afterResp, err := c.doRequest(ctx, "POST", "/select/logsql/query", afterParams)
	if err != nil {
		return nil, fmt.Errorf("after logs request failed: %w", err)
	}
	defer afterResp.Body.Close()

	if afterResp.StatusCode == http.StatusOK {
		afterResult, err := parseJSONLResponse(afterResp.Body)
		if err == nil {
			result.AfterLogs = afterResult.Logs
		}
	}

	result.Stats.RowsRead = len(result.BeforeLogs) + len(result.AfterLogs)

	return result, nil
}

func (c *Client) GetFieldDistinctValues(ctx context.Context, database, table string, params backends.FieldValuesParams) (*backends.FieldValuesResult, error) {
	query := "*"
	if params.FilterQuery != "" {
		query = params.FilterQuery
	}

	reqParams := url.Values{}
	reqParams.Set("query", query)
	reqParams.Set("field", params.FieldName)

	if !params.TimeRange.Start.IsZero() {
		reqParams.Set("start", params.TimeRange.Start.UTC().Format(time.RFC3339))
	}
	if !params.TimeRange.End.IsZero() {
		reqParams.Set("end", params.TimeRange.End.UTC().Format(time.RFC3339))
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	reqParams.Set("limit", strconv.Itoa(limit))

	if params.TimeoutSeconds != nil && *params.TimeoutSeconds > 0 {
		reqParams.Set("timeout", fmt.Sprintf("%ds", *params.TimeoutSeconds))
	}

	resp, err := c.doRequest(ctx, "GET", "/select/logsql/field_values", reqParams)
	if err != nil {
		return nil, fmt.Errorf("field values request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("field values request failed with status %d: %s", resp.StatusCode, string(body))
	}

	fieldValues, err := parseFieldValuesResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing field values: %w", err)
	}

	return convertFieldValuesToResult(params.FieldName, fieldValues), nil
}

func (c *Client) GetAllFilterableFieldValues(ctx context.Context, database, table string, params backends.AllFieldValuesParams) (map[string]*backends.FieldValuesResult, error) {
	tableInfo, err := c.GetTableInfo(ctx, database, table)
	if err != nil {
		return nil, fmt.Errorf("getting table info: %w", err)
	}

	results := make(map[string]*backends.FieldValuesResult)

	for _, col := range tableInfo.Columns {
		if ctx.Err() != nil {
			break
		}

		if strings.HasPrefix(col.Name, "_") && col.Name != "_msg" {
			continue
		}

		fieldParams := backends.FieldValuesParams{
			FieldName:      col.Name,
			FieldType:      col.Type,
			TimestampField: params.TimestampField,
			TimeRange:      params.TimeRange,
			Timezone:       params.Timezone,
			Limit:          params.Limit,
			TimeoutSeconds: params.TimeoutSeconds,
			FilterQuery:    params.FilterQuery,
		}

		fieldResult, err := c.GetFieldDistinctValues(ctx, database, table, fieldParams)
		if err != nil {
			c.logger.Debug("skipping field values", "field", col.Name, "error", err)
			continue
		}
		results[col.Name] = fieldResult
	}

	return results, nil
}

func (c *Client) Ping(ctx context.Context, database, table string) error {
	params := url.Values{}
	params.Set("query", "* | limit 1")
	params.Set("start", "1m")

	resp, err := c.doRequest(ctx, "GET", "/select/logsql/query", params)
	if err != nil {
		return fmt.Errorf("ping request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ping failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *Client) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

func (c *Client) Reconnect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.httpClient.CloseIdleConnections()
	return c.Ping(ctx, "", "")
}

func (c *Client) doRequest(ctx context.Context, method, path string, params url.Values) (*http.Response, error) {
	fullURL := c.baseURL + path

	var body io.Reader
	if method == "POST" {
		body = bytes.NewBufferString(params.Encode())
	} else {
		fullURL = fullURL + "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	if c.accountID != "" {
		req.Header.Set("AccountID", c.accountID)
	}
	if c.projectID != "" {
		req.Header.Set("ProjectID", c.projectID)
	}

	c.logger.Debug("executing VictoriaLogs request",
		"method", method,
		"path", path,
		"query", params.Get("query"),
	)

	return c.httpClient.Do(req)
}

func getTimezoneOffset(timezone string) string {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return ""
	}

	_, offset := time.Now().In(loc).Zone()
	hours := offset / 3600
	minutes := (offset % 3600) / 60

	if hours >= 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("-%dh%dm", -hours, -minutes)
}

func reverseSlice(logs []map[string]any) []map[string]any {
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}
	return logs
}
