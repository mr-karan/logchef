package clickhouse

import "testing"

func TestReadsCurrentTime(t *testing.T) {
	cases := []struct {
		query string
		want  bool
	}{
		{"SELECT count() FROM logs WHERE ts > now() - INTERVAL 5 MINUTE", true},
		{"SELECT count() FROM logs WHERE ts > NOW64(3)", true},
		{"SELECT count() FROM logs WHERE ts > current_timestamp()", true},
		{"WITH now() AS n SELECT count() FROM logs WHERE ts > n", true},
		{"SELECT count() FROM (SELECT * FROM logs WHERE toDate(ts) = today())", true},
		{"SELECT count() FROM logs WHERE x IN (SELECT y FROM t WHERE z > yesterday())", true},
		{"SELECT count() FROM logs WHERE ts >= initialQueryStartTime() - INTERVAL 5 MINUTE", true},
		{"SELECT count() FROM logs WHERE ts >= initial_query_start_time() - INTERVAL 5 MINUTE", true},
		{"SELECT count() FROM logs WHERE ts >= toDateTime64('2026-11-17 10:00:00', 3) AND ts < toDateTime64('2026-11-17 10:05:00', 3)", false},
		{"SELECT 'now()' AS label, count() FROM logs", false},
		{"SELECT count() AS now FROM logs", false},
	}
	for _, tc := range cases {
		got, err := ReadsCurrentTime(tc.query)
		if err != nil {
			t.Fatalf("%s: %v", tc.query, err)
		}
		if got != tc.want {
			t.Errorf("ReadsCurrentTime(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
	if _, err := ReadsCurrentTime("SELEC count() FROM logs"); err == nil {
		t.Error("an unparsable query should return its parse error")
	}
}
