package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
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

// rewriteModeReads passes each read of client's overflow modes, numbered from
// 1, to rewrite, as if new connections had reached servers of other versions.
func rewriteModeReads(client *Client, rewrite func(read int64, modes clickhouse.Settings)) {
	read := client.throwModes.read
	var reads atomic.Int64
	client.throwModes.read = func(ctx context.Context) (clickhouse.Settings, error) {
		modes, err := read(ctx)
		if err == nil {
			rewrite(reads.Add(1), modes)
		}
		return modes, err
	}
}

// queryCounter counts the statements a client sends with Query.
type queryCounter struct {
	driver.Conn
	queries atomic.Int64
}

func (c *queryCounter) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	c.queries.Add(1)
	return c.Conn.Query(ctx, query, args...)
}

// countedClient returns a new client whose Query calls counter counts. Its
// first lookup of the server's modes dials its first connection, so a
// statement it sends first runs after a dial.
func countedClient(t *testing.T) (*Client, *queryCounter) {
	t.Helper()
	client, err := NewClient(ClientOptions{Host: testClickHouseAddr(t)}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	counter := &queryCounter{Conn: client.conn}
	client.conn = counter
	return client, counter
}

// TestSendDoesNotRepeatAnInsertClickHouse runs an INSERT that writes rows and
// then fails with code 115, UNKNOWN_SETTING, raised by throwIf. The read of
// the server's modes after the failure leaves out one the INSERT sent, as if
// its new connection had reached an older server. The INSERT may have run,
// so Logchef must not send it again, and its error must say so.
func TestSendDoesNotRepeatAnInsertClickHouse(t *testing.T) {
	client, counter := countedClient(t)
	rewriteModeReads(client, func(read int64, modes clickhouse.Settings) {
		if read > 1 {
			delete(modes, "read_overflow_mode_leaf")
		}
	})
	file := fmt.Sprintf("logchef_test_%d.tsv", time.Now().UnixNano())
	_, err := client.Query(t.Context(), fmt.Sprintf("INSERT INTO FUNCTION file('%s', 'TSV', 'n UInt64') SELECT number FROM numbers(1000000) WHERE throwIf(number = 500000, 'Unknown setting', toInt32(115)) = 0 SETTINGS max_block_size = 1000, max_threads = 1, allow_custom_error_code_in_throwif = 1", file))
	if !isUnknownSetting(err) || !strings.Contains(err.Error(), "did not send the statement again") {
		t.Fatalf("the INSERT returned %v, want UNKNOWN_SETTING and that Logchef did not send it again", err)
	}
	if n := counter.queries.Load(); n != 1 {
		t.Fatalf("the INSERT was sent %d times, want 1", n)
	}
	// One run writes at most the 500000 rows before the row that throws.
	var written uint64
	if err := client.conn.QueryRow(t.Context(), fmt.Sprintf("SELECT count() FROM file('%s', 'TSV', 'n UInt64')", file)).Scan(&written); err != nil {
		t.Fatal(err)
	}
	t.Logf("the file holds %d rows", written)
	if written == 0 {
		t.Fatal("the INSERT wrote no rows before it failed, so a second run would not show")
	}
	if written > 500000 {
		t.Fatalf("the file holds %d rows: the INSERT ran twice", written)
	}
}

// TestSendRepeatsARejectedSelectClickHouse has the first read of the
// server's modes return a setting the server does not know, as a read from a
// newer server before a new connection reached this one. The server rejects
// the SELECT before it runs it, the read again leaves the setting out, and
// Logchef sends the SELECT once more.
func TestSendRepeatsARejectedSelectClickHouse(t *testing.T) {
	client, counter := countedClient(t)
	rewriteModeReads(client, func(read int64, modes clickhouse.Settings) {
		if read == 1 {
			modes["logchef_test_unknown_overflow_mode"] = "throw"
		}
	})
	result, err := client.QueryWithOptions(t.Context(), "SELECT number FROM numbers(3)", QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Logs) != 3 || result.Stats.Truncation != models.TruncationNone {
		t.Fatalf("the SELECT read %v with truncation %q, want 3 rows and none", result.Logs, result.Stats.Truncation)
	}
	if n := counter.queries.Load(); n != 2 {
		t.Fatalf("the SELECT was sent %d times, want 2", n)
	}
}

// TestSendKeepsTheQueryOwnUnknownSettingClickHouse sends a SELECT whose own
// SETTINGS name a setting the server does not know, right after a dial. The
// server's modes read again are the same, so sending it again cannot help:
// it fails once, with ClickHouse's error.
func TestSendKeepsTheQueryOwnUnknownSettingClickHouse(t *testing.T) {
	client, counter := countedClient(t)
	_, err := client.QueryWithOptions(t.Context(), "SELECT 1 SETTINGS logchef_test_no_such_setting = 1", QueryOptions{})
	if !isUnknownSetting(err) || !strings.Contains(err.Error(), "logchef_test_no_such_setting") {
		t.Fatalf("the SELECT returned %v, want ClickHouse's unknown setting error", err)
	}
	if n := counter.queries.Load(); n != 1 {
		t.Fatalf("the SELECT was sent %d times, want 1", n)
	}
}
