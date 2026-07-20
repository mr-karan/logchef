package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// fakeNetTimeoutError implements net.Error with Timeout() == true, standing
// in for a real *net.OpError without needing an actual socket.
type fakeNetTimeoutError struct{}

func (fakeNetTimeoutError) Error() string   { return "fake: i/o timeout" }
func (fakeNetTimeoutError) Timeout() bool   { return true }
func (fakeNetTimeoutError) Temporary() bool { return true }

var _ net.Error = fakeNetTimeoutError{}

func TestIsTimeoutError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "wrapped context.DeadlineExceeded",
			err:  fmt.Errorf("executing query: %w", context.DeadlineExceeded),
			want: true,
		},
		{
			name: "bare context.DeadlineExceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "context.Canceled is not a timeout",
			err:  context.Canceled,
			want: false,
		},
		{
			name: "clickhouse exception code 159 TIMEOUT_EXCEEDED",
			err: &clickhouse.Exception{
				Code:    159,
				Name:    "TIMEOUT_EXCEEDED",
				Message: "Timeout exceeded: elapsed 60.1 seconds, maximum: 60",
			},
			want: true,
		},
		{
			name: "wrapped clickhouse exception code 159",
			err: fmt.Errorf("executing query: %w", &clickhouse.Exception{
				Code:    159,
				Name:    "TIMEOUT_EXCEEDED",
				Message: "Timeout exceeded",
			}),
			want: true,
		},
		{
			name: "clickhouse exception code 209 SOCKET_TIMEOUT",
			err: &clickhouse.Exception{
				Code:    209,
				Name:    "SOCKET_TIMEOUT",
				Message: "Timeout: connect timed out",
			},
			want: true,
		},
		{
			name: "clickhouse exception for an unrelated error code",
			err: &clickhouse.Exception{
				Code:    60,
				Name:    "UNKNOWN_TABLE",
				Message: "Table default.foo doesn't exist",
			},
			want: false,
		},
		{
			name: "net.Error with Timeout() true",
			err:  fakeNetTimeoutError{},
			want: true,
		},
		{
			name: "generic error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTimeoutError(tc.err); got != tc.want {
				t.Errorf("isTimeoutError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsTimeoutError_LiveDeadlineExceeded proves the classifier fires against
// a real driver round-trip, not just a hand-built error value: it opens a
// client against the local dev ClickHouse and issues a query with an
// impossibly small context timeout, then asserts the resulting error
// classifies as a timeout.
//
// Skips (rather than fails) if no ClickHouse is reachable at 127.0.0.1:9000,
// so it doesn't break CI environments without the dev stack running.
func TestIsTimeoutError_LiveDeadlineExceeded(t *testing.T) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:9000", 500*time.Millisecond)
	if err != nil {
		t.Skipf("no ClickHouse reachable at 127.0.0.1:9000, skipping live test: %v", err)
	}
	conn.Close()

	opts := &clickhouse.Options{
		Addr: []string{"127.0.0.1:9000"},
		Auth: clickhouse.Auth{
			Database: "default",
		},
	}
	chConn, err := clickhouse.Open(opts)
	if err != nil {
		t.Fatalf("clickhouse.Open: %v", err)
	}
	defer chConn.Close()

	// A 1-nanosecond deadline guarantees the context is already expired by
	// the time the driver issues the query.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	queryErr := chConn.Ping(ctx)
	if queryErr == nil {
		t.Fatal("expected an error from Ping with an already-expired context, got nil")
	}
	if !isTimeoutError(queryErr) {
		t.Errorf("isTimeoutError(%v) = false, want true for an expired-context query", queryErr)
	}
}
