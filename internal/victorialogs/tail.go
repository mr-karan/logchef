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
				// EOF or a read error caused by ctx cancellation are clean stops.
				if errors.Is(err, io.EOF) || ctx.Err() != nil {
					decodeDone <- nil
				} else {
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
