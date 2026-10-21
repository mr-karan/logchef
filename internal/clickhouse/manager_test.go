package clickhouse

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// testProxy forwards TCP connections to upstream. While silent, it accepts
// connections and never answers, like a hung ClickHouse.
type testProxy struct {
	t        *testing.T
	upstream string
	addr     string
	silent   atomic.Bool

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

func waitForHealth(t *testing.T, m *Manager, id models.SourceID, timeout time.Duration, done func(models.SourceHealth) bool) models.SourceHealth {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		health := m.GetCachedHealth(id)
		if done(health) || time.Now().After(deadline) {
			return health
		}
		time.Sleep(20 * time.Millisecond)
	}
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
	proxy.start()
	if _, err := client.Query(ctx, "SELECT 1"); err != nil {
		t.Fatalf("query after restart: %v", err)
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
	initial := waitForHealth(t, m, id, 5*time.Second, func(h models.SourceHealth) bool { return h.Error != "Initial connection pending" })
	if initial.Status != models.HealthStatusUnhealthy {
		t.Fatalf("silent server reported %s", initial.Status)
	}

	hungCtx, cancelHung := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancelHung)
	go func() { _, _ = client.Query(hungCtx, "SELECT 1") }()
	time.Sleep(100 * time.Millisecond)
	m.StartBackgroundHealthChecks(time.Second)
	// Let the first background check fail against the silent server.
	time.Sleep(1500 * time.Millisecond)
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
	health := waitForHealth(t, m, id, 4*time.Second, func(h models.SourceHealth) bool { return h.Status == models.HealthStatusHealthy })
	if health.Status != models.HealthStatusHealthy {
		t.Fatalf("health loop stalled: %+v", health)
	}
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
	m := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = m.Close() })
	const id models.SourceID = 1
	// The silent server holds the initial health check open past the removal.
	addTestSource(t, m, id, silentServer(t))
	time.Sleep(100 * time.Millisecond)
	if err := m.RemoveSource(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * HealthCheckTimeout)
	if health := m.GetCachedHealth(id); !health.LastChecked.IsZero() {
		t.Fatalf("removed source has health entry: %+v", health)
	}
}
