package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
)

func (f *serverFact[T]) cached() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.known
}

// TestThrowModesFollowReconnect moves a client's connections between servers
// of different versions, both ways. A dial reaches the new server, and the
// next lookup returns the modes that server knows: a mode the older server
// lacks fails every query there, and one the newer server has goes
// unenforced if left out.
func TestThrowModesFollowReconnect(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		wantLeaf bool
	}{
		{from: "24.1.2.5", to: "23.7.4.5", wantLeaf: false},
		{from: "23.7.4.5", to: "24.1.2.5", wantLeaf: true},
	} {
		t.Run(tc.from+" to "+tc.to, func(t *testing.T) {
			var dials atomic.Uint64
			var mu sync.Mutex
			server := tc.from
			fact := serverFact[clickhouse.Settings]{dials: &dials, read: func(context.Context) (clickhouse.Settings, error) {
				mu.Lock()
				defer mu.Unlock()
				return throwModesFor(server), nil
			}}
			before, err := fact.get(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, leaf := before["timeout_overflow_mode_leaf"]; leaf == tc.wantLeaf {
				t.Fatalf("modes before the move %v", before)
			}
			mu.Lock()
			server = tc.to
			mu.Unlock()
			dials.Add(1)
			after, err := fact.get(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, leaf := after["timeout_overflow_mode_leaf"]; leaf != tc.wantLeaf {
				t.Fatalf("modes after a dial to %s: %v", tc.to, after)
			}
		})
	}
}

// TestServerFactSharesOneRead looks up a stale value from many goroutines at
// once: they share one read, and a waiter that gives up neither fails the
// read nor stops the others getting its value.
func TestServerFactSharesOneRead(t *testing.T) {
	var dials atomic.Uint64
	var reads atomic.Int64
	release := make(chan struct{})
	fact := serverFact[int]{dials: &dials, read: func(ctx context.Context) (int, error) {
		reads.Add(1)
		select {
		case <-release:
			return 7, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}}
	quitter, quit := context.WithCancel(t.Context())
	quitErr := make(chan error, 1)
	go func() {
		_, err := fact.get(quitter)
		quitErr <- err
	}()
	const callers = 20
	values := make(chan int, callers)
	var group sync.WaitGroup
	for range callers {
		group.Go(func() {
			value, err := fact.get(t.Context())
			if err != nil {
				t.Error(err)
			}
			values <- value
		})
	}
	waitForCondition(t, 5*time.Second, func() bool { return reads.Load() > 0 })
	quit()
	if err := <-quitErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("the waiter that gave up got %v", err)
	}
	close(release)
	group.Wait()
	close(values)
	for value := range values {
		if value != 7 {
			t.Fatalf("a waiter got %d, want 7", value)
		}
	}
	if n := reads.Load(); n != 1 {
		t.Fatalf("%d reads for %d concurrent lookups, want 1", n, callers+1)
	}
	if _, err := fact.get(t.Context()); err != nil || reads.Load() != 1 {
		t.Fatalf("a fresh lookup read again (%v)", err)
	}
}

func TestServerFactDoesNotKeepAFailedRead(t *testing.T) {
	var dials atomic.Uint64
	fail := true
	fact := serverFact[int]{dials: &dials, read: func(context.Context) (int, error) {
		if fail {
			return 0, errors.New("server unavailable")
		}
		return 7, nil
	}}
	if _, err := fact.get(t.Context()); err == nil {
		t.Fatal("a failed read returned no error")
	}
	fail = false
	if value, err := fact.get(t.Context()); err != nil || value != 7 {
		t.Fatalf("lookup after a failed read: %d, %v", value, err)
	}
}

// TestClientCountsDials checks the client hears each connection its driver
// dials, here to a listener that accepts and never speaks ClickHouse.
func TestClientCountsDials(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	client, err := NewClient(ClientOptions{Host: ln.Addr().String()}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(t.Context(), "", ""); err == nil {
		t.Fatal("ping succeeded against a server that closes every connection")
	}
	if client.dials.Load() == 0 {
		t.Fatal("the client did not count the driver's dial")
	}
}

// TestServerVersionChangeClickHouse moves a client's address from a server
// that knows timeout_overflow_mode_leaf to one older than 23.11 that does
// not, and back. The user's settings profile on the newer server sets the
// mode to break, so the value a query reads shows whether Logchef sent throw.
// The first query after the move to the older server succeeds, and a later
// query on the newer server sends the mode again.
func TestServerVersionChangeClickHouse(t *testing.T) {
	newer := testClickHouseAddr(t)
	older := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_OLD_ADDR")
	if older == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_OLD_ADDR to a ClickHouse server older than 23.11 to run against both")
	}
	user := fmt.Sprintf("logchef_test_%d", time.Now().UnixNano())
	for addr, profile := range map[string]string{newer: " SETTINGS timeout_overflow_mode_leaf = 'break'", older: ""} {
		admin, err := NewClient(ClientOptions{Host: addr}, discardLogger())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = admin.Close() })
		if err := admin.conn.Exec(t.Context(), "CREATE USER "+user+" IDENTIFIED WITH no_password"+profile); err != nil {
			t.Skipf("%s does not allow creating users: %v", addr, err)
		}
		t.Cleanup(func() { _ = admin.conn.Exec(context.Background(), "DROP USER IF EXISTS "+user) })
	}
	proxy := startProxy(t, newer)
	client, err := NewClient(ClientOptions{Host: proxy.addr, Username: user}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	leafMode := func(t *testing.T) *models.QueryResult {
		t.Helper()
		result, err := client.QueryWithOptions(ctx, "SELECT value FROM system.settings WHERE name = 'timeout_overflow_mode_leaf'", QueryOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	// The first query's read dials the client's first connection, which makes
	// the modes it read stale; the second reads them again and keeps them.
	for range 2 {
		if result := leafMode(t); len(result.Logs) != 1 || result.Logs[0]["value"] != "throw" {
			t.Fatalf("the newer server read %v, want throw", result.Logs)
		}
	}
	move := func(upstream string) {
		proxy.stop()
		proxy.mu.Lock()
		proxy.upstream = upstream
		proxy.mu.Unlock()
		proxy.start()
	}
	move(older)
	for range 2 {
		if result := leafMode(t); len(result.Logs) != 0 || result.Stats.Truncation != models.TruncationNone {
			t.Fatalf("the older server read %v with truncation %q, want no rows and none", result.Logs, result.Stats.Truncation)
		}
	}
	move(newer)
	if result := leafMode(t); result.Stats.Truncation != models.TruncationUnknown {
		t.Fatalf("the first query on the newer server read %v with truncation %q, want unknown: it may not have sent the mode", result.Logs, result.Stats.Truncation)
	}
	if result := leafMode(t); len(result.Logs) != 1 || result.Logs[0]["value"] != "throw" || result.Stats.Truncation != models.TruncationNone {
		t.Fatalf("the newer server read %v with truncation %q after the move back, want throw and none", result.Logs, result.Stats.Truncation)
	}
}
