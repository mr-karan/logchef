package clickhouse

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	vmmetrics "github.com/VictoriaMetrics/metrics"

	"github.com/mr-karan/logchef/pkg/models"
)

// testProxy forwards TCP connections to upstream. While silent, it accepts
// connections and never answers, like a hung ClickHouse.
type testProxy struct {
	t        *testing.T
	upstream string
	addr     string
	silent   atomic.Bool
	accepted atomic.Int64

	mu    sync.Mutex
	ln    net.Listener
	conns []net.Conn
}

func startProxy(t *testing.T, upstream string) *testProxy {
	t.Helper()
	p := &testProxy{t: t, upstream: upstream, addr: "127.0.0.1:0"}
	p.start()
	t.Cleanup(p.stop)
	return p
}

func (p *testProxy) start() {
	p.t.Helper()
	ln, err := net.Listen("tcp", p.addr)
	if err != nil {
		p.t.Fatal(err)
	}
	p.mu.Lock()
	p.ln, p.addr = ln, ln.Addr().String()
	p.mu.Unlock()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			p.track(conn)
			p.accepted.Add(1)
			if p.silent.Load() {
				continue
			}
			go p.forward(conn)
		}
	}()
}

func (p *testProxy) forward(client net.Conn) {
	server, err := net.Dial("tcp", p.upstream)
	if err != nil {
		_ = client.Close()
		return
	}
	p.track(server)
	go func() { _, _ = io.Copy(server, client) }()
	_, _ = io.Copy(client, server)
}

func (p *testProxy) track(conn net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conns = append(p.conns, conn)
}

// stop closes the listener and every connection, like a server going down.
func (p *testProxy) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.ln.Close()
	for _, conn := range p.conns {
		_ = conn.Close()
	}
	p.conns = nil
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func testClickHouseAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	return addr
}

// refusedAddr returns a local address with no listener.
func refusedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func addTestSource(t *testing.T, m *Manager, id models.SourceID, host string) *Client {
	t.Helper()
	source := &models.Source{ID: id, Name: t.Name(), Connection: models.ConnectionInfo{Host: host, Database: "default", TableName: "logs"}}
	if err := m.AddSource(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	client, err := m.GetConnection(id)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func waitForHealth(t *testing.T, m *Manager, id models.SourceID, timeout time.Duration, want healthState) {
	t.Helper()
	waitForCondition(t, timeout, func() bool {
		m.healthMux.RLock()
		defer m.healthMux.RUnlock()
		state, exists := m.health[id]
		return exists && state == want
	})
}

// The driver pool redials after the server comes back; the client needs no
// reconnect of its own.
func TestClientRecoversAfterServerRestartClickHouse(t *testing.T) {
	proxy := startProxy(t, testClickHouseAddr(t))
	client, err := NewClient(ClientOptions{Host: proxy.addr}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := client.Query(ctx, "SELECT 1"); err != nil {
		t.Fatalf("query before restart: %v", err)
	}
	proxy.stop()
	if err := client.Ping(ctx, "", ""); err == nil {
		t.Fatal("ping succeeded while the server was down")
	}
	if client.flattenedJSON != nil {
		t.Fatal("capability cache survived a failed ping")
	}
	proxy.start()
	if _, err := client.Query(ctx, "SELECT 1"); err != nil {
		t.Fatalf("query after restart: %v", err)
	}
	if client.flattenedJSON == nil {
		t.Fatal("query after restart did not detect capabilities")
	}
	if err := client.Ping(ctx, "", ""); err != nil {
		t.Fatalf("ping after restart: %v", err)
	}
}

// A query stuck on a hung server must not block the health loop or later
// queries once the server answers again.
func TestHungQueryDoesNotStallHealthOrQueriesClickHouse(t *testing.T) {
	proxy := startProxy(t, testClickHouseAddr(t))
	proxy.silent.Store(true)
	m := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = m.Close() })
	const id models.SourceID = 1
	client := addTestSource(t, m, id, proxy.addr)
	waitForHealth(t, m, id, 5*time.Second, healthUnhealthy)

	hungCtx, cancelHung := context.WithTimeout(context.Background(), 30*time.Second)
	hungDone := make(chan struct{})
	t.Cleanup(func() {
		cancelHung()
		select {
		case <-hungDone:
		case <-time.After(5 * time.Second):
			t.Error("hung query did not stop after cancellation")
		}
	})
	accepted := proxy.accepted.Load()
	go func() {
		_, _ = client.Query(hungCtx, "SELECT 1")
		close(hungDone)
	}()
	waitForCondition(t, time.Second, func() bool { return proxy.accepted.Load() > accepted })
	m.checkAllSourcesHealth()
	select {
	case <-hungDone:
		t.Fatal("query ended before the server recovered")
	default:
	}
	m.StartBackgroundHealthChecks(time.Second)
	proxy.silent.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if _, err := client.Query(ctx, "SELECT 1"); err != nil {
		t.Fatalf("query after recovery: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("query after recovery took %s", elapsed)
	}
	waitForHealth(t, m, id, 4*time.Second, healthHealthy)
}

func TestFailedQueryLogsOnce(t *testing.T) {
	var logs syncBuffer
	m := NewManager(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { _ = m.Close() })
	client := addTestSource(t, m, 1, refusedAddr(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Query(ctx, "SELECT 1"); err == nil {
		t.Fatal("query against a refused address succeeded")
	}
	if got := strings.Count(logs.String(), `"msg":"query failed"`); got != 1 {
		t.Fatalf("failed query logged %d times:\n%s", got, logs.String())
	}
}

func TestRemovedSourceLeavesNoHealth(t *testing.T) {
	proxy := startProxy(t, "")
	proxy.silent.Store(true)
	m := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = m.Close() })
	const id models.SourceID = 1
	addTestSource(t, m, id, proxy.addr)
	waitForCondition(t, time.Second, func() bool { return proxy.accepted.Load() > 0 })
	done := make(chan struct{})
	go func() {
		m.checkSource(context.Background(), id)
		close(done)
	}()
	waitForCondition(t, time.Second, func() bool { return proxy.accepted.Load() > 1 })
	if err := m.RemoveSource(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * HealthCheckTimeout):
		t.Fatal("removed source health check did not finish")
	}
	m.healthMux.RLock()
	_, exists := m.health[id]
	m.healthMux.RUnlock()
	if exists {
		t.Fatal("removed source has a health entry")
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition timed out")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestOldHealthCannotOverwriteReplacementClickHouse(t *testing.T) {
	proxy := startProxy(t, testClickHouseAddr(t))
	proxy.silent.Store(true)
	m := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = m.Close() })
	const id models.SourceID = 1
	addTestSource(t, m, id, proxy.addr)
	waitForHealth(t, m, id, 2*time.Second, healthUnhealthy)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accepted := proxy.accepted.Load()
	done := make(chan struct{})
	go func() {
		m.checkSource(ctx, id)
		close(done)
	}()
	waitForCondition(t, time.Second, func() bool { return proxy.accepted.Load() > accepted })
	if err := m.RemoveSource(id); err != nil {
		t.Fatal(err)
	}
	proxy.silent.Store(false)
	addTestSource(t, m, id, proxy.addr)
	waitForHealth(t, m, id, 500*time.Millisecond, healthHealthy)
	metric := fmt.Sprintf(`logchef_clickhouse_connection_status{source_id="%d",source_name=%q,database="default",table="logs",host=%q}`, id, t.Name(), proxy.addr)
	assertHealthyMetric := func() {
		t.Helper()
		var exposition bytes.Buffer
		vmmetrics.WritePrometheus(&exposition, false)
		if !strings.Contains(exposition.String(), metric+" 1\n") {
			t.Fatalf("replacement connection metric is not healthy:\n%s", exposition.String())
		}
	}
	assertHealthyMetric()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * HealthCheckTimeout):
		t.Fatal("old health check did not finish")
	}
	m.healthMux.RLock()
	state := m.health[id]
	m.healthMux.RUnlock()
	if state != healthHealthy {
		t.Fatalf("old probe overwrote replacement health: got %v, want healthy", state)
	}
	assertHealthyMetric()
}

func TestStartupUnreachableLogsOnce(t *testing.T) {
	var logs syncBuffer
	m := NewManager(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { _ = m.Close() })
	const id models.SourceID = 1
	addTestSource(t, m, id, refusedAddr(t))
	waitForCondition(t, 2*time.Second, func() bool {
		return strings.Contains(logs.String(), `"level":"WARN","msg":"source unhealthy"`)
	})
	m.checkSource(context.Background(), id)
	if got := strings.Count(logs.String(), `"msg":"source unhealthy"`); got != 1 {
		t.Fatalf("startup failure logged %d times:\n%s", got, logs.String())
	}
}

func TestHealthRecoveryLogsAtInfoClickHouse(t *testing.T) {
	proxy := startProxy(t, testClickHouseAddr(t))
	proxy.silent.Store(true)
	var logs syncBuffer
	m := NewManager(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { _ = m.Close() })
	const id models.SourceID = 2
	addTestSource(t, m, id, proxy.addr)
	waitForHealth(t, m, id, 2*time.Second, healthUnhealthy)
	proxy.silent.Store(false)
	m.checkSource(context.Background(), id)
	waitForHealth(t, m, id, 2*time.Second, healthHealthy)
	m.checkSource(context.Background(), id)
	if got := strings.Count(logs.String(), `"level":"INFO","msg":"source healthy"`); got != 1 {
		t.Fatalf("recovery logged at Info %d times:\n%s", got, logs.String())
	}
}

func TestStartupHealthyLogsAtDebugClickHouse(t *testing.T) {
	var logs syncBuffer
	m := NewManager(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { _ = m.Close() })
	const id models.SourceID = 3
	addTestSource(t, m, id, testClickHouseAddr(t))
	waitForHealth(t, m, id, 2*time.Second, healthHealthy)
	m.checkSource(context.Background(), id)
	if got := strings.Count(logs.String(), `"level":"DEBUG","msg":"source healthy"`); got != 1 {
		t.Fatalf("startup success logged at Debug %d times:\n%s", got, logs.String())
	}
	if strings.Contains(logs.String(), `"level":"INFO","msg":"source healthy"`) {
		t.Fatalf("startup success logged as recovery:\n%s", logs.String())
	}
}

func TestManagerCloseTwice(t *testing.T) {
	m := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.StartBackgroundHealthChecks(time.Hour)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}
