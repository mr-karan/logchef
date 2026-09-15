package victorialogs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestHistogramEscapedLabelsBudget(t *testing.T) {
	for _, tc := range []struct {
		name        string
		character   string
		repetitions int
	}{
		{name: "control", character: "\x00", repetitions: 2 * 1024 * 1024},
		{name: "HTML", character: "<>&", repetitions: 700_000},
		{name: "short escape", character: "\n", repetitions: 5 * 1024 * 1024},
		{name: "line separator", character: "\u2028", repetitions: 2 * 1024 * 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			label, err := json.Marshal(strings.Repeat(tc.character, tc.repetitions))
			if err != nil {
				t.Fatal(err)
			}
			body := []byte(`{"hits":[{"fields":{"service":` + string(label) + `},"timestamps":["2026-09-11T00:00:00Z","2026-09-11T00:01:00Z"],"values":[3,5]}]}`)
			if len(body) >= models.MaxHistogramResponseBytes {
				t.Fatal("fixture exceeds the wire budget")
			}
			decoded, err := readHistogramResponse(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			result, err := buildHistogramResult(decoded, "service", "1m")
			if !errors.Is(err, models.ErrHistogramBudgetExceeded) || result != nil {
				t.Fatalf("repeated escaped label bypassed flattened response budget: %v", err)
			}
			decoded.Hits[0].Timestamps = decoded.Hits[0].Timestamps[:1]
			decoded.Hits[0].Values = decoded.Hits[0].Values[:1]
			result, err = buildHistogramResult(decoded, "service", "1m")
			if err != nil {
				t.Fatalf("single escaped label fits the budget: %v", err)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded) > models.MaxHistogramResponseBytes || result.Data[0].LogCount != 3 {
				t.Fatalf("accepted histogram bytes=%d count=%d", len(encoded), result.Data[0].LogCount)
			}
		})
	}
}

func TestHistogramResponseReadBudget(t *testing.T) {
	const prefix = `{"hits":[]}`
	for _, size := range []int{models.MaxHistogramResponseBytes - 1, models.MaxHistogramResponseBytes, models.MaxHistogramResponseBytes + 4096} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			body := strings.NewReader(prefix + strings.Repeat(" ", size-len(prefix)))
			result, err := readHistogramResponse(body)
			if size > models.MaxHistogramResponseBytes {
				if !errors.Is(err, models.ErrHistogramBudgetExceeded) || result != nil {
					t.Fatalf("expected budget rejection, err=%v", err)
				}
				if read := size - body.Len(); read != models.MaxHistogramResponseBytes+1 {
					t.Fatalf("read %d bytes past the bounded reader", read)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHistogramBodyCancellation(t *testing.T) {
	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"hits":[`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(requestCanceled)
	}))
	defer server.Close()
	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, err := provider.Histogram(ctx, source, datasource.HistogramRequest{Query: "*", Window: "1s"})
	if !errors.Is(err, context.DeadlineExceeded) || result != nil {
		t.Fatalf("expected read cancellation without partial counts, err=%v", err)
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("upstream request was not canceled")
	}
}

func TestHistogramMismatchedSeries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"hits":[{"timestamps":["2026-09-11T00:00:00Z"],"values":[]}]}`))
	}))
	defer server.Close()
	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})
	result, err := provider.Histogram(context.Background(), source, datasource.HistogramRequest{Query: "*", Window: "1s"})
	if err == nil || result != nil {
		t.Fatal("mismatched timestamps and values returned partial counts")
	}
}

func TestHistogramVictoriaLogsBucketBounds(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		buckets, groups, labelBytes int
		wantOverflow                bool
	}{
		{name: "ungrouped at budget", buckets: models.MaxHistogramBuckets, groups: 1},
		{name: "ungrouped overflow", buckets: models.MaxHistogramBuckets + 1, groups: 1, wantOverflow: true},
		{name: "grouped at budget", buckets: models.MaxHistogramBuckets, groups: 11},
		{name: "grouped sparse overflow", buckets: models.MaxHistogramBuckets + 1, groups: 2, wantOverflow: true},
		{name: "flattened labels exceed budget", buckets: models.MaxHistogramBuckets, groups: 2, labelBytes: 2000, wantOverflow: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type series struct {
				Fields     map[string]string `json:"fields"`
				Timestamps []string          `json:"timestamps"`
				Values     []int64           `json:"values"`
			}
			response := struct {
				Hits []series `json:"hits"`
			}{}
			for group := range tc.groups {
				s := series{Fields: map[string]string{}}
				if group < 10 {
					s.Fields["service"] = fmt.Sprint(group) + strings.Repeat("x", tc.labelBytes)
				}
				for bucket := range tc.buckets {
					s.Timestamps = append(s.Timestamps, time.Unix(int64(bucket), 0).UTC().Format(time.RFC3339))
					s.Values = append(s.Values, 2)
				}
				response.Hits = append(response.Hits, s)
			}
			body, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
			defer server.Close()
			provider := newTestProvider(server)
			source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})
			req := datasource.HistogramRequest{Query: "*", Window: "1s"}
			if tc.groups > 1 {
				req.GroupBy = "service"
			}
			result, err := provider.Histogram(context.Background(), source, req)
			if tc.wantOverflow {
				if !errors.Is(err, models.ErrHistogramBudgetExceeded) || result != nil {
					t.Fatalf("expected overflow with no partial counts, err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			total, other := 0, 0
			for _, row := range result.Data {
				total += row.LogCount
				if row.IsOther {
					other += row.LogCount
				}
			}
			if total != tc.buckets*tc.groups*2 {
				t.Fatalf("total=%d", total)
			}
			if tc.groups == 11 && other != tc.buckets*2 {
				t.Fatalf("Other total=%d", other)
			}
		})
	}
}

func TestHistogramBoundsVictoriaLogsIntegration(t *testing.T) {
	baseURL := integrationBaseURL(t)
	runID := newTestRunID(t)
	base := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Hour)
	const groups = 12
	rows := make([]fixtureRow, (models.MaxHistogramBuckets+1)*groups)
	for i := range rows {
		rows[i] = fixtureRow{msg: "histogram budget", service: fmt.Sprint(i % groups), offset: time.Duration(i/groups) * time.Second}
	}
	ingestFixtures(t, baseURL, runID, base, rows)
	provider := newTestProvider(nil)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: baseURL, Scope: models.VictoriaLogsScope{Query: fmt.Sprintf(`test_run:=%q`, runID)}})
	end := base.Add(time.Duration(models.MaxHistogramBuckets) * time.Second)
	// Wait for the last bucket, rather than polling a limited preview for all rows.
	waitForFixtures(t, provider, source, queryWindow{start: end, end: end.Add(time.Second)}, groups)
	for _, groupBy := range []string{"", "service"} {
		for _, count := range []int{models.MaxHistogramBuckets, models.MaxHistogramBuckets + 1} {
			t.Run(fmt.Sprintf("%s/%d", groupBy, count), func(t *testing.T) {
				// The hits endpoint excludes the end instant.
				end := base.Add(time.Duration(count) * time.Second)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				result, err := provider.Histogram(ctx, source, datasource.HistogramRequest{Query: "*", Window: "1s", StartTime: &base, EndTime: &end, GroupBy: groupBy})
				if count > models.MaxHistogramBuckets {
					if !errors.Is(err, models.ErrHistogramBudgetExceeded) || result != nil {
						t.Fatalf("expected overflow, err=%v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				total, other := 0, 0
				for _, row := range result.Data {
					total += row.LogCount
					if row.IsOther {
						other += row.LogCount
					}
				}
				wantRows := count
				if groupBy != "" {
					wantRows *= 11
				}
				if total != count*groups || len(result.Data) != wantRows {
					t.Fatalf("total=%d rows=%d, want %d and %d", total, len(result.Data), count*groups, wantRows)
				}
				if groupBy != "" && other != count*2 {
					t.Fatalf("Other total=%d, want %d", other, count*2)
				}
			})
		}
	}
}
