# ClickHouse HTTP Interface Migration

## Context & Current State

Currently, LogChef uses the ClickHouse native protocol through the official Go driver. While this provides good performance, we're facing several challenges:

1. **Column Type Detection**: We currently need complex SQL wrapping to detect column types:

   ```sql
   WITH query AS (...)
   SELECT
       arrayMap(x -> (...), JSONExtractKeysAndValuesRaw(toJSONString(*))) as _column_info,
       toJSONString(tuple(*)) as raw_json
   FROM query
   ```

2. **Error Handling**: Due to the streaming nature of the native protocol, errors can occur after headers are sent, making it difficult to provide clean error responses to the client.

3. **Query Progress**: While the native protocol supports progress tracking, it requires custom handling and doesn't integrate well with standard monitoring tools.

## Why Migrate to HTTP Interface

The ClickHouse HTTP interface offers several advantages that directly address our current challenges:

1. **Rich Metadata Out of the Box**:

   ```json
   {
       "meta": [
           {
               "name": "service_name",
               "type": "String"
           }
       ],
       "data": [...],
       "statistics": {
           "elapsed": 0.001137687,
           "rows_read": 3,
           "bytes_read": 24
       }
   }
   ```

   - No need for complex SQL wrapping
   - Accurate type information
   - Built-in query statistics

2. **Better Error Handling**:

   - Response buffering support (`wait_end_of_query=1`)
   - Errors returned in the same format as successful responses
   - Clean error handling before sending headers

3. **Standard HTTP Features**:
   - Compression (gzip, deflate, br)
   - Connection pooling
   - Standard monitoring and debugging tools
   - Better integration with proxies and load balancers

## Implementation Plan

### 1. HTTP Client Implementation

```go
type HTTPClient struct {
    baseURL    string
    client     *http.Client
    settings   map[string]string
}

func NewHTTPClient(addr string) *HTTPClient {
    return &HTTPClient{
        baseURL: fmt.Sprintf("http://%s:8123", addr),
        client: &http.Client{
            Transport: &http.Transport{
                MaxIdleConns: 100,
                MaxIdleConnsPerHost: 100,
                IdleConnTimeout: 90 * time.Second,
            },
        },
        settings: map[string]string{
            "buffer_size": "3000000",
            "wait_end_of_query": "1",
            "send_progress_in_http_headers": "1",
        },
    }
}

func (c *HTTPClient) Query(ctx context.Context, query string) (*QueryResult, error) {
    // Build URL with settings
    u, _ := url.Parse(c.baseURL)
    q := u.Query()
    for k, v := range c.settings {
        q.Set(k, v)
    }
    q.Set("query", query)
    u.RawQuery = q.Encode()

    // Create request
    req, err := http.NewRequestWithContext(ctx, "POST", u.String(), nil)
    if err != nil {
        return nil, err
    }

    // Add headers
    req.Header.Set("Accept-Encoding", "gzip")
    req.Header.Set("Format", "JSONCompact")

    // Execute and parse response
    resp, err := c.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result struct {
        Meta []struct {
            Name string `json:"name"`
            Type string `json:"type"`
        } `json:"meta"`
        Data []map[string]interface{} `json:"data"`
        Statistics struct {
            Elapsed  float64 `json:"elapsed"`
            RowsRead int     `json:"rows_read"`
        } `json:"statistics"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    return &QueryResult{
        Columns: result.Meta,
        Data:    result.Data,
        Stats:   result.Statistics,
    }, nil
}
```

### 2. Performance Optimizations

1. **Response Buffering**:

   ```go
   // Control buffer size based on query type
   settings := map[string]string{
       "buffer_size": "3000000",
       "wait_end_of_query": "1",
   }
   ```

2. **Compression**:

   ```go
   // Enable compression for large responses
   req.Header.Set("Accept-Encoding", "gzip")
   ```

3. **Connection Pooling**:
   ```go
   transport := &http.Transport{
       MaxIdleConns: 100,
       MaxIdleConnsPerHost: 100,
       IdleConnTimeout: 90 * time.Second,
   }
   ```

### 3. Migration Strategy

1. Create new HTTP client implementation
2. Add feature flag for HTTP vs Native protocol
3. Gradually migrate queries to HTTP interface
4. Monitor performance and error rates
5. Full migration once stability is confirmed

## Potential Challenges & Mitigations

1. **Performance Impact**:

   - Use compression for large datasets
   - Tune buffer sizes based on query patterns
   - Monitor and adjust connection pool settings

2. **Memory Usage**:

   - Control with `max_memory_usage` setting
   - Use streaming for large result sets
   - Implement proper cleanup in defer blocks

3. **Error Handling Edge Cases**:
   - Implement retries for network errors
   - Add timeout handling
   - Proper context cancellation

## Success Metrics

1. **Reduced Complexity**:

   - Elimination of complex SQL wrapping
   - Simpler error handling code
   - More maintainable codebase

2. **Improved Reliability**:

   - Fewer error handling edge cases
   - Better error messages
   - More consistent behavior

3. **Performance**:
   - Comparable query execution times
   - Acceptable memory usage
   - No degradation in large result sets

## Next Steps

1. [ ] Create HTTP client implementation
2. [ ] Add feature flag configuration
3. [ ] Implement monitoring for both protocols
4. [ ] Begin gradual migration
5. [ ] Monitor and tune performance
6. [ ] Document new interface and best practices

Key advantages of HTTP:
Direct JSON: No intermediate string/byte conversions 2. Rich Metadata: Column types come directly in response
Built-in Compression: Native gzip support
Better Error Handling: Errors come as JSON responses
Simpler Code: No need for complex JSON wrapping
For our log analytics use case where we're:
Dealing with large result sets
Need column metadata
Want precise numeric handling
Care about memory efficiency
The HTTP interface would be more performant and simpler to work with. The only downside might be slightly higher latency for very small queries, but for our typical use case (1000s of logs), HTTP would likely be faster overall due to better data handling and compression.
Would you like me to:
Create a prototype using the HTTP interface?
Run some benchmarks comparing both approaches?
Document a migration plan?

```
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
)

// ClickHouseResponse represents the structure of the JSONCompact response
type ClickHouseResponse struct {
	Meta []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"meta"`
	Data       [][]interface{} `json:"data"` // Changed to array of arrays for JSONCompact
	Rows       int             `json:"rows"`
	Statistics struct {
		Elapsed   float64 `json:"elapsed"`
		RowsRead  int     `json:"rows_read"`
		BytesRead int     `json:"bytes_read"`
	} `json:"statistics"`
}

// ClickHouseClient handles interactions with ClickHouse
type ClickHouseClient struct {
	baseURL string
	client  *http.Client
}

// NewClickHouseClient creates a new ClickHouse client
func NewClickHouseClient(host string, port int) *ClickHouseClient {
	return &ClickHouseClient{
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		client:  &http.Client{},
	}
}

// Query executes a query and returns the response
func (c *ClickHouseClient) Query(query string) (*ClickHouseResponse, error) {
	params := url.Values{}
	params.Add("query", query+" FORMAT JSONCompact")

	url := fmt.Sprintf("%s/?%s", c.baseURL, params.Encode())

	resp, err := c.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("clickhouse error: status=%d, body=%s", resp.StatusCode, string(body))
	}

	return parseResponse(resp.Body)
}

// parseResponse parses the HTTP response body into a ClickHouseResponse
func parseResponse(body io.Reader) (*ClickHouseResponse, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result ClickHouseResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &result, nil
}

// GetRows returns the data rows with column names
func (r *ClickHouseResponse) GetRows() []map[string]interface{} {
	result := make([]map[string]interface{}, len(r.Data))

	for i, row := range r.Data {
		rowMap := make(map[string]interface{})
		for j, value := range row {
			if j < len(r.Meta) {
				rowMap[r.Meta[j].Name] = value
			}
		}
		result[i] = rowMap
	}

	return result
}

func main() {
	client := NewClickHouseClient("127.0.0.1", 8123)

	query := "SELECT * FROM logs.vector_logs LIMIT 1"
	result, err := client.Query(query)
	if err != nil {
		log.Fatalf("Error executing query: %v", err)
	}

	// Print the metadata
	log.Printf("Columns:")
	for _, col := range result.Meta {
		log.Printf("  %s (%s)", col.Name, col.Type)
	}

	// Print the rows in a more readable format
	log.Printf("\nRows:")
	for _, row := range result.GetRows() {
		log.Printf("  %+v", row)
	}

	// Print statistics
	log.Printf("\nStatistics: elapsed=%.6fs, rows=%d, bytes=%d",
		result.Statistics.Elapsed,
		result.Statistics.RowsRead,
		result.Statistics.BytesRead)
}
```
