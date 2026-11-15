package victorialogs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestFormatTimezoneOffset asserts formatTimezoneOffset returns a
// VictoriaLogs-parseable duration string (not a clock-offset string like
// "+05:30") for the `offset` param on /select/logsql/hits, including
// half-hour and negative zones. Empirically verified against a running
// VictoriaLogs instance: offset=19800s (Asia/Kolkata, UTC+5:30) aligns
// buckets to IST midnight (00:00 IST == 18:30 UTC), and offset=-9000s
// (America/St_Johns, UTC-2:30 in July/NDT) aligns buckets to NDT midnight
// (00:00 NDT == 02:30 UTC).
func TestFormatTimezoneOffset(t *testing.T) {
	t.Parallel()

	reference := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		timezone string
		want     string
	}{
		{name: "UTC returns empty", timezone: "UTC", want: ""},
		{name: "empty timezone returns empty", timezone: "", want: ""},
		{name: "unknown timezone returns empty", timezone: "Not/AZone", want: ""},
		{name: "positive offset Asia/Kolkata (+05:30)", timezone: "Asia/Kolkata", want: "19800s"},
		{name: "half-hour negative offset America/St_Johns (-02:30 NDT in July)", timezone: "America/St_Johns", want: "-9000s"},
		{name: "quarter-hour offset Asia/Kathmandu (+05:45)", timezone: "Asia/Kathmandu", want: "20700s"},
		{name: "negative whole-hour offset America/New_York (-04:00 EDT in July)", timezone: "America/New_York", want: "-14400s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatTimezoneOffset(tt.timezone, &reference, nil)
			if got != tt.want {
				t.Fatalf("formatTimezoneOffset(%q) = %q, want %q", tt.timezone, got, tt.want)
			}
			if got != "" {
				if _, err := time.ParseDuration(got); err != nil {
					t.Fatalf("formatTimezoneOffset(%q) = %q is not a valid Go/VL duration: %v", tt.timezone, got, err)
				}
			}
		})
	}
}

// Field values and live tail need log rows, so they reject an aggregation;
// a compile that opts in reads that the generator cannot compile it yet.
func TestLogchefQLAggregationOptIn(t *testing.T) {
	source := &models.Source{SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time"}
	_, err := compileQueryForVictoriaLogs("x=1 | count()", models.QueryLanguageLogchefQL, source)
	if parseErr, ok := errors.AsType[*logchefql.ParseError](err); !ok || parseErr.Code != logchefql.ErrAggregationNotAllowed {
		t.Fatalf("field values error=%v, want %s", err, logchefql.ErrAggregationNotAllowed)
	}

	provider := &Provider{}
	for allow, want := range map[bool]string{false: logchefql.ErrAggregationNotAllowed, true: logchefql.ErrAggregationNotSupported} {
		compiled, err := provider.CompileLogchefQL(context.Background(), source, datasource.LogchefQLCompileRequest{Query: "x=1 | count()", AllowAggregation: allow})
		if err == nil || compiled == nil || compiled.Valid || compiled.Error == nil || compiled.Error.Code != want {
			t.Fatalf("allow=%v: compiled=%+v err=%v, want %s", allow, compiled, err, want)
		}
	}
}
