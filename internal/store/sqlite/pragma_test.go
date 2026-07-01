package sqlite

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestConnectionPragmasOnEveryConnection guards the DSN-based PRAGMA setup: the
// per-connection pragmas (busy_timeout, foreign_keys) must be present on EVERY
// pooled read connection, not just the one that happened to run a PRAGMA
// statement. It forces several connections open simultaneously and checks each.
func TestConnectionPragmasOnEveryConnection(t *testing.T) {
	db := newTxTestDB(t)
	ctx := context.Background()

	const n = 5
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, n)

	for range n {
		wg.Go(func() {
			// Grab a dedicated connection and hold it until all n are open, so
			// the pool is forced to create n distinct connections.
			conn, err := db.readDB.Conn(ctx)
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close()
			<-start

			var fk, busy int
			if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
				errs <- err
				return
			}
			if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
				errs <- err
				return
			}
			if fk != 1 {
				errs <- fmt.Errorf("foreign_keys = %d, want 1", fk)
			}
			if busy != 5000 {
				errs <- fmt.Errorf("busy_timeout = %d, want 5000", busy)
			}
		})
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
