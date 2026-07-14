package victorialogs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// tailBatchSize flushes accumulated rows once this many are buffered.
	tailBatchSize = 100
	// tailFlushInterval flushes whatever has accumulated at least this often,
	// so a trickle of rows is not held back waiting for a full batch.
	tailFlushInterval = 200 * time.Millisecond
)

// ErrTailUpstreamClosed reports that the VictoriaLogs tail stream ended
// because the upstream connection was closed (EOF) without our own context
// being cancelled first. /select/logsql/tail is meant to stream indefinitely,
// so an EOF we didn't ask for means VictoriaLogs restarted, timed out the
// connection, or the network dropped — a connection loss, not a graceful
// completion. Distinguishing the two matters because the caller reports
// ctx-cancelled stops (client disconnect, session TTL) as "completed" but
// must not report an unrequested upstream close the same way.
var ErrTailUpstreamClosed = errors.New("victorialogs tail: upstream closed the connection unexpectedly")

// TailLogs proxies VictoriaLogs' native /select/logsql/tail stream. The upstream
// response body streams NDJSON rows as they are ingested; we decode incrementally
// and batch-emit every tailFlushInterval or tailBatchSize rows, whichever comes
// first. ctx cancellation closes the upstream body (via the deferred Close and
// the request context), which unblocks the decode goroutine.
func (p *Provider) TailLogs(ctx context.Context, source *models.Source, req datasource.TailRequest, emit datasource.TailEmitter) error {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return err
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = "*"
	}

	form := url.Values{}
	form.Set("query", query)
	applyScopeFilters(form, conn)

	resp, err := p.doFormRequest(ctx, conn, "/select/logsql/tail", form) //nolint:bodyclose // resp.Body is closed by the unconditional defer on the next line; bodyclose can't trace Close() ownership across the goroutine below that reads resp.Body (verified false positive, not a leak).
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	rowCh := make(chan map[string]any)
	decodeDone := make(chan error, 1)
	go func() {
		decoder := json.NewDecoder(resp.Body)
		decoder.UseNumber()
		for {
			var row map[string]any
			if err := decoder.Decode(&row); err != nil {
				switch {
				case ctx.Err() != nil:
					// We asked for this: client disconnect, session TTL, or
					// admission eviction cancelled ctx, which is what caused the
					// read to fail (EOF or otherwise). Clean, expected stop.
					decodeDone <- nil
				case errors.Is(err, io.EOF):
					// Upstream closed the stream on its own; ctx is still live, so
					// nobody asked for this. Report it as a connection loss, not a
					// graceful completion.
					decodeDone <- ErrTailUpstreamClosed
				default:
					decodeDone <- err
				}
				return
			}
			select {
			case rowCh <- row:
			case <-ctx.Done():
				decodeDone <- nil
				return
			}
		}
	}()

	ticker := time.NewTicker(tailFlushInterval)
	defer ticker.Stop()

	batch := make([]map[string]any, 0, tailBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		out := batch
		batch = make([]map[string]any, 0, tailBatchSize)
		return emit(out)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-decodeDone:
			if flushErr := flush(); flushErr != nil {
				return flushErr
			}
			return err
		case row := <-rowCh:
			batch = append(batch, row)
			if len(batch) >= tailBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		case <-ticker.C:
			if err := flush(); err != nil {
				return err
			}
		}
	}
}
