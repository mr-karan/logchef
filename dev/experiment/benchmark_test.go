package experiment

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

const (
	host          = "localhost"
	httpPort      = 8123
	nativePort    = 9000
	database      = "logs"
	table         = "vector_logs"
	recordsToTest = 10000
)

type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type HTTPResponse struct {
	Meta []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"meta"`
	Data       [][]interface{} `json:"data"`
	Rows       int             `json:"rows"`
	Statistics struct {
		Elapsed  float64 `json:"elapsed"`
		RowsRead int     `json:"rows_read"`
	} `json:"statistics"`
}

type BenchmarkStats struct {
	TotalRequests uint64
	TotalBytes    uint64
	TotalLatency  int64
	MaxLatency    int64
	MinLatency    int64
	Errors        uint64
}

func (s *BenchmarkStats) AddLatency(d time.Duration) {
	atomic.AddUint64(&s.TotalRequests, 1)
	atomic.AddInt64(&s.TotalLatency, int64(d))

	// Update max latency
	for {
		current := atomic.LoadInt64(&s.MaxLatency)
		if int64(d) <= current {
			break
		}
		if atomic.CompareAndSwapInt64(&s.MaxLatency, current, int64(d)) {
			break
		}
	}

	// Update min latency
	for {
		current := atomic.LoadInt64(&s.MinLatency)
		if current == 0 || int64(d) < current {
			if atomic.CompareAndSwapInt64(&s.MinLatency, current, int64(d)) {
				break
			}
		} else {
			break
		}
	}
}

func (s *BenchmarkStats) AddBytes(n uint64) {
	atomic.AddUint64(&s.TotalBytes, n)
}

func (s *BenchmarkStats) AddError() {
	atomic.AddUint64(&s.Errors, 1)
}

// setupHTTPClient creates an HTTP client with proper settings
func setupHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
		Timeout: 30 * time.Second,
	}
}

// setupNativeClient creates a ClickHouse native protocol client
func setupNativeClient() (*sql.DB, error) {
	conn := clickhouse.OpenDB(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", host, nativePort)},
		Auth: clickhouse.Auth{
			Database: database,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		Debug: false,
	})

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping: %w", err)
	}

	return conn, nil
}

// benchmarkHTTP tests the HTTP interface with JSONCompact format
func benchmarkHTTP(b *testing.B) {
	stats := &BenchmarkStats{
		MinLatency: math.MaxInt64,
	}
	client := setupHTTPClient()
	baseURL := fmt.Sprintf("http://%s:%d", host, httpPort)

	query := fmt.Sprintf(`
		SELECT *
		FROM %s.%s
		ORDER BY timestamp DESC
		LIMIT %d
	`, database, table, recordsToTest)

	// Prepare URL with parameters
	u, err := url.Parse(baseURL)
	if err != nil {
		b.Fatal(err)
	}

	q := u.Query()
	q.Set("query", query)
	q.Set("database", database)
	q.Set("default_format", "JSONCompact")
	q.Set("wait_end_of_query", "1")
	u.RawQuery = q.Encode()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			start := time.Now()

			req, err := http.NewRequest("GET", u.String(), nil)
			if err != nil {
				stats.AddError()
				b.Fatal(err)
			}
			req.Header.Set("Accept-Encoding", "gzip")

			resp, err := client.Do(req)
			if err != nil {
				stats.AddError()
				b.Fatal(err)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				resp.Body.Close()
				stats.AddError()
				b.Fatal(err)
			}
			resp.Body.Close()

			stats.AddBytes(uint64(len(body)))

			var result HTTPResponse
			if err := json.Unmarshal(body, &result); err != nil {
				stats.AddError()
				b.Fatal(err)
			}

			if result.Rows != recordsToTest || len(result.Data) != recordsToTest {
				stats.AddError()
				b.Fatalf("expected %d records, got %d rows and %d data items",
					recordsToTest, result.Rows, len(result.Data))
			}

			stats.AddLatency(time.Since(start))
		}
	})

	// Report stats
	b.ReportMetric(float64(stats.TotalBytes)/float64(b.N), "bytes/op")
	b.ReportMetric(float64(stats.TotalLatency)/float64(stats.TotalRequests), "avg_latency_ns")
	b.ReportMetric(float64(stats.MaxLatency), "max_latency_ns")
	b.ReportMetric(float64(stats.MinLatency), "min_latency_ns")
	b.ReportMetric(float64(stats.Errors), "errors")
}

// benchmarkNative tests the native protocol with our current approach
func benchmarkNative(b *testing.B) {
	stats := &BenchmarkStats{
		MinLatency: math.MaxInt64,
	}
	db, err := setupNativeClient()
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	baseQuery := fmt.Sprintf(`
		SELECT *
		FROM %s.%s
		ORDER BY timestamp DESC
		LIMIT %d
	`, database, table, recordsToTest)

	// Schema query using DESCRIBE TABLE
	schemaQuery := fmt.Sprintf("DESCRIBE TABLE (\n%s\n)", baseQuery)

	// Data query with JSON conversion
	dataQuery := fmt.Sprintf(`
		WITH query AS (%s)
		SELECT toJSONString(tuple(*)) as raw_json
		FROM query
	`, baseQuery)

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			start := time.Now()

			// First get schema
			schemaRows, err := db.QueryContext(ctx, schemaQuery)
			if err != nil {
				b.Fatal(err)
			}

			var columns []ColumnInfo
			for schemaRows.Next() {
				var col ColumnInfo
				var defaultType, defaultExpr, comment, codec, ttl string
				if err := schemaRows.Scan(&col.Name, &col.Type, &defaultType, &defaultExpr, &comment, &codec, &ttl); err != nil {
					schemaRows.Close()
					b.Fatal(err)
				}
				columns = append(columns, col)
			}
			schemaRows.Close()

			if err = schemaRows.Err(); err != nil {
				b.Fatal(err)
			}

			// Then get data
			dataRows, err := db.QueryContext(ctx, dataQuery)
			if err != nil {
				b.Fatal(err)
			}

			var count int
			for dataRows.Next() {
				var jsonStr string
				if err := dataRows.Scan(&jsonStr); err != nil {
					dataRows.Close()
					b.Fatal(err)
				}

				var rowData map[string]interface{}
				if err := json.Unmarshal([]byte(jsonStr), &rowData); err != nil {
					dataRows.Close()
					b.Fatal(err)
				}
				count++
			}
			dataRows.Close()

			if err = dataRows.Err(); err != nil {
				b.Fatal(err)
			}

			if count != recordsToTest {
				b.Fatalf("expected %d records, got %d", recordsToTest, count)
			}

			stats.AddLatency(time.Since(start))
		}
	})

	// Report stats
	b.ReportMetric(float64(stats.TotalBytes)/float64(b.N), "bytes/op")
	b.ReportMetric(float64(stats.TotalLatency)/float64(stats.TotalRequests), "avg_latency_ns")
	b.ReportMetric(float64(stats.MaxLatency), "max_latency_ns")
	b.ReportMetric(float64(stats.MinLatency), "min_latency_ns")
	b.ReportMetric(float64(stats.Errors), "errors")
}

func BenchmarkClickHouse(b *testing.B) {
	// Record initial number of goroutines
	initialGoroutines := runtime.NumGoroutine()

	b.Run("HTTP_JSONCompact", benchmarkHTTP)
	b.Run("Native_WithDescribe", benchmarkNative)

	// Check for goroutine leaks
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines > initialGoroutines {
		b.Logf("Possible goroutine leak: started with %d, ended with %d",
			initialGoroutines, finalGoroutines)
	}
}
