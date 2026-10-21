package clickhouse

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	vm "github.com/VictoriaMetrics/metrics"

	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestManagerRemoveSourceDropsItsMetricSeries(t *testing.T) {
	m := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	source := &models.Source{ID: 9101, Name: "removed-source"}
	metrics.RecordClickHouseConnectionStatus(source, true)

	if err := m.RemoveSource(source.ID); err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}

	var buf bytes.Buffer
	vm.WritePrometheus(&buf, false)
	if strings.Contains(buf.String(), `source_id="9101"`) {
		t.Fatalf("series for removed source still exposed:\n%s", buf.String())
	}
}
