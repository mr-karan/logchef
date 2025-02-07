# ClickHouse HTTP Protocol Migration Plan

## Current State

Currently, LogChef uses the ClickHouse native protocol through the official Go driver. Our benchmarks show:

```
Native Protocol:
- ~60 MB/op memory usage
- ~658K allocations/op
- Best latency: 91ms
- Poor scaling beyond 4 CPUs

HTTP Protocol:
- ~31.5 MB/op memory usage (47% less)
- ~356K allocations/op (46% fewer)
- Best latency: 48ms
- Excellent scaling up to 8 CPUs
```

## Migration Goals

1. **Performance Improvements**:

   - Reduce memory usage by ~47%
   - Reduce allocations by ~46%
   - Improve latency by ~47%
   - Better CPU scaling

2. **Simplification**:

   - Remove complex SQL wrapping for schema detection
   - Cleaner error handling
   - Better type information
   - Built-in compression

3. **Maintainability**:
   - Simpler codebase
   - Better error messages
   - More standard HTTP tooling support

## Implementation Plan

### Phase 1: HTTP Client Implementation

1. Create configuration types in `pkg/clickhouse/types.go`:

```go
type HTTPOptions struct {
    // Connection settings
    Host            string
    Port            int
    Database        string
    Username        string
    Password        string

    // HTTP specific settings
    MaxIdleConns        int           `yaml:"max_idle_conns" default:"100"`
    MaxIdleConnsPerHost int           `yaml:"max_idle_conns_per_host" default:"100"`
    IdleConnTimeout     time.Duration `yaml:"idle_conn_timeout" default:"90s"`
    RequestTimeout      time.Duration `yaml:"request_timeout" default:"30s"`

    // ClickHouse settings
    BufferSize              int    `yaml:"buffer_size" default:"3000000"`
    WaitEndOfQuery         bool   `yaml:"wait_end_of_query" default:"true"`
    SendProgressInHeaders  bool   `yaml:"send_progress_in_headers" default:"true"`

    // Compression settings
    EnableCompression bool   `yaml:"enable_compression" default:"true"`
    CompressionLevel  int    `yaml:"compression_level" default:"1"`
}

// HTTPClient represents a connection to a ClickHouse server via HTTP
type HTTPClient struct {
    opts     HTTPOptions
    baseURL  string
    client   *http.Client
    settings map[string]string
    log      *zap.Logger
}

// HTTPResponse represents the ClickHouse JSON response format
type HTTPResponse struct {
    Meta []struct {
        Name string `json:"name"`
        Type string `json:"type"`
    } `json:"meta"`
    Data       [][]interface{} `json:"data"`
    Rows       int            `json:"rows"`
    Statistics struct {
        Elapsed  float64 `json:"elapsed"`
        RowsRead int     `json:"rows_read"`
    } `json:"statistics"`
}
```

2. Implement client creation in `pkg/clickhouse/http.go`:

```go
func NewHTTPClient(opts HTTPOptions, log *zap.Logger) (*HTTPClient, error) {
    // Validate options
    if opts.Host == "" {
        return nil, fmt.Errorf("host is required")
    }
    if opts.Port == 0 {
        opts.Port = 8123 // Default ClickHouse HTTP port
    }

    // Setup transport with connection pooling
    transport := &http.Transport{
        MaxIdleConns:        opts.MaxIdleConns,
        MaxIdleConnsPerHost: opts.MaxIdleConnsPerHost,
        IdleConnTimeout:     opts.IdleConnTimeout,
    }

    // Create client with timeout
    client := &http.Client{
        Transport: transport,
        Timeout:   opts.RequestTimeout,
    }

    // Build settings map
    settings := map[string]string{
        "buffer_size":                 strconv.Itoa(opts.BufferSize),
        "wait_end_of_query":          strconv.FormatBool(opts.WaitEndOfQuery),
        "send_progress_in_http_headers": strconv.FormatBool(opts.SendProgressInHeaders),
    }

    // Build base URL with credentials if provided
    baseURL := url.URL{
        Scheme: "http",
        Host:   fmt.Sprintf("%s:%d", opts.Host, opts.Port),
    }
    if opts.Database != "" {
        settings["database"] = opts.Database
    }

    return &HTTPClient{
        opts:     opts,
        baseURL:  baseURL.String(),
        client:   client,
        settings: settings,
        log:      log,
    }, nil
}
```

3. Implement query execution with auth and compression:

```go
func (c *HTTPClient) Query(ctx context.Context, query string) (*QueryResult, error) {
    // Build URL with settings
    u, _ := url.Parse(c.baseURL)
    q := u.Query()
    for k, v := range c.settings {
        q.Set(k, v)
    }
    q.Set("query", query)
    q.Set("default_format", "JSONCompact")
    u.RawQuery = q.Encode()

    // Create request
    req, err := http.NewRequestWithContext(ctx, "POST", u.String(), nil)
    if err != nil {
        return nil, fmt.Errorf("create request: %w", err)
    }

    // Add authentication if configured
    if c.opts.Username != "" {
        req.SetBasicAuth(c.opts.Username, c.opts.Password)
    }

    // Add compression headers
    if c.opts.EnableCompression {
        req.Header.Set("Accept-Encoding", "gzip")
        if c.opts.CompressionLevel > 0 {
            req.Header.Set("Content-Encoding", "gzip")
        }
    }

    // Execute request with logging
    c.log.Debug("executing clickhouse query",
        zap.String("host", c.opts.Host),
        zap.String("database", c.opts.Database),
        zap.String("query", query),
    )

    resp, err := c.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("execute request: %w", err)
    }
    defer resp.Body.Close()

    // Handle errors
    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("clickhouse error: %s", body)
    }

    // Setup response reader with compression
    var reader io.Reader = resp.Body
    if resp.Header.Get("Content-Encoding") == "gzip" {
        reader, err = gzip.NewReader(resp.Body)
        if err != nil {
            return nil, fmt.Errorf("create gzip reader: %w", err)
        }
    }

    // Parse response
    var result HTTPResponse
    if err := json.NewDecoder(reader).Decode(&result); err != nil {
        return nil, fmt.Errorf("decode response: %w", err)
    }

    return &QueryResult{
        Columns: result.Meta,
        Data:    result.Data,
        Stats:   result.Statistics,
    }, nil
}
```

4. Update connection pool in `pkg/clickhouse/pool.go`:

```go
type Pool struct {
    mu       sync.RWMutex
    clients  map[string]*HTTPClient
    log      *zap.Logger
}

func NewPool(log *zap.Logger) *Pool {
    return &Pool{
        clients: make(map[string]*HTTPClient),
        log:     log,
    }
}

func (p *Pool) AddClient(id string, opts HTTPOptions) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    client, err := NewHTTPClient(opts, p.log)
    if err != nil {
        return err
    }
    p.clients[id] = client
    return nil
}

func (p *Pool) GetClient(id string) (*HTTPClient, error) {
    p.mu.RLock()
    defer p.mu.RUnlock()

    client, ok := p.clients[id]
    if !ok {
        return nil, fmt.Errorf("client %s not found", id)
    }
    return client, nil
}
```

### Phase 2: Migration Strategy

1. Direct replacement:

   - Remove all native protocol code
   - Replace with HTTP implementation
   - Update all connection creation points

2. Update configuration:

```yaml
clickhouse:
  sources:
    logs:
      host: clickhouse-logs
      port: 8123
      database: logs
      username: default
      password: secret
      max_idle_conns: 100
      buffer_size: 3000000
      enable_compression: true
    metrics:
      host: clickhouse-metrics
      port: 8123
      database: metrics
```

### Phase 3: Cleanup

1. Remove native protocol code:

   - Delete native client implementation
   - Remove tuple conversion code
   - Clean up unused types

2. Update error handling:
   - Add proper HTTP error mapping
   - Improve error messages
   - Add retry logic for transient failures

## Testing Plan

1. **Unit Tests**:

   - HTTP client configuration
   - Authentication handling
   - Compression settings
   - Connection pooling
   - Multiple client management

2. **Integration Tests**:

   - Multiple concurrent connections
   - Authentication scenarios
   - Large result sets
   - Error conditions
   - Connection failures

3. **Performance Tests**:
   - Memory usage per client
   - CPU utilization
   - Network bandwidth
   - Response times
   - Connection pool behavior

## Success Metrics

1. **Performance**:

   - 47% reduction in memory usage
   - 46% reduction in allocations
   - Sub-50ms latency for typical queries
   - Linear scaling to 8 CPUs

2. **Reliability**:

   - Zero increase in error rate
   - Improved error messages
   - Better failure handling

3. **Maintainability**:
   - 30% reduction in code complexity
   - Fewer dependencies
   - Better debugging capabilities

## Timeline

1. HTTP Client Implementation: 2 days
2. Migration: 2 days
3. Cleanup: 1 day
4. Testing and Monitoring: 2 days

Total: 1 week for full migration

## Next Steps

1. [ ] Create HTTP client implementation
2. [ ] Update configuration structure
3. [ ] Implement connection pooling
4. [ ] Begin migration
5. [ ] Monitor and tune performance
6. [ ] Document new interface and best practices
