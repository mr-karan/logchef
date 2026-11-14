package template

import (
	"strings"
	"testing"
	"time"
)

// The fixture is the row of RFC 0003 "Macros": a week of calendar days in
// Asia/Kolkata, whose midnight is 18:30 UTC, with day buckets.
func macroFixture(t *testing.T) Builtins {
	t.Helper()
	from := time.Date(2026, 11, 5, 18, 30, 0, 0, time.UTC)
	to := time.Date(2026, 11, 12, 18, 30, 0, 0, time.UTC)
	interval, ok := ParseInterval("24h")
	if !ok {
		t.Fatal("24h is not an interval")
	}
	return Builtins{From: &from, To: &to, Location: mustLocation(t, "Asia/Kolkata"), Interval: interval, TimestampColumn: "ts"}
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

func TestMacroForms(t *testing.T) {
	const (
		chFrom = "toDateTime64('2026-11-05 18:30:00.000', 3, 'UTC')"
		chTo   = "toDateTime64('2026-11-12 18:30:00.000', 3, 'UTC')"
	)
	tests := []struct {
		macro      string
		clickhouse string
		logsql     string
		chErr      string
		logsqlErr  string
	}{
		{macro: "__from", clickhouse: chFrom, logsql: `"2026-11-05T18:30:00Z"`},
		{macro: "__to", clickhouse: chTo, logsql: `"2026-11-12T18:30:00Z"`},
		{macro: "__time_filter", clickhouse: "`ts` >= " + chFrom + " AND `ts` < " + chTo, logsqlErr: "not available in LogsQL"},
		{macro: "__interval", clickhouse: "INTERVAL 1 DAY", logsql: "1d"},
		{macro: "__interval_s", clickhouse: "86400", logsql: "86400"},
		{macro: "__timezone", clickhouse: "'Asia/Kolkata'", logsqlErr: "not available in LogsQL"},
		{macro: "__tz_offset", chErr: "not available in ClickHouse SQL", logsql: "19800s"},
	}
	for _, tt := range tests {
		for _, dialect := range []struct {
			name    string
			dialect Dialect
			want    string
			wantErr string
		}{
			{"clickhouse", ClickHouseSQL, tt.clickhouse, tt.chErr},
			{"logsql", LogsQL, tt.logsql, tt.logsqlErr},
		} {
			t.Run(tt.macro+"/"+dialect.name, func(t *testing.T) {
				got, err := dialect.dialect.Substitute("a {{"+tt.macro+"}} b", nil, macroFixture(t))
				if dialect.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), dialect.wantErr) {
						t.Fatalf("error = %v, want it to contain %q", err, dialect.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if want := "a " + dialect.want + " b"; got != want {
					t.Fatalf("got  %s\nwant %s", got, want)
				}
			})
		}
	}
}

func TestMacroInstantsKeepMillisecondsInUTC(t *testing.T) {
	from := time.Date(2026, 11, 6, 0, 0, 0, 500_000_000, mustLocation(t, "Asia/Kolkata"))
	builtins := Builtins{From: &from}
	got, err := ClickHouseSQL.Substitute("{{__from}}", nil, builtins)
	if err != nil || got != "toDateTime64('2026-11-05 18:30:00.500', 3, 'UTC')" {
		t.Fatalf("clickhouse = %q, %v", got, err)
	}
	got, err = LogsQL.Substitute("{{__from}}", nil, builtins)
	if err != nil || got != `"2026-11-05T18:30:00.5Z"` {
		t.Fatalf("logsql = %q, %v", got, err)
	}
}

func TestMacroIntervalTable(t *testing.T) {
	tests := []struct {
		window     string
		clickhouse string
		logsql     string
		seconds    string
	}{
		{"1s", "INTERVAL 1 SECOND", "1s", "1"},
		{"30s", "INTERVAL 30 SECOND", "30s", "30"},
		{"15m", "INTERVAL 15 MINUTE", "15m", "900"},
		{"1h", "INTERVAL 1 HOUR", "1h", "3600"},
		{"12h", "INTERVAL 12 HOUR", "12h", "43200"},
		{"24h", "INTERVAL 1 DAY", "1d", "86400"},
	}
	for _, tt := range tests {
		interval, ok := ParseInterval(tt.window)
		if !ok {
			t.Fatalf("%s is not an interval", tt.window)
		}
		builtins := Builtins{Interval: interval}
		for dialect, want := range map[Dialect]string{ClickHouseSQL: tt.clickhouse + " " + tt.seconds, LogsQL: tt.logsql + " " + tt.seconds} {
			got, err := dialect.Substitute("{{__interval}} {{__interval_s}}", nil, builtins)
			if err != nil || got != want {
				t.Errorf("%s: got %q, %v, want %q", tt.window, got, err, want)
			}
		}
	}
	want := []string{"1s", "5s", "10s", "15s", "30s", "1m", "5m", "10m", "15m", "30m", "1h", "2h", "3h", "6h", "12h", "24h"}
	if got := IntervalWindows(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("windows = %v, want the histogram windows %v", got, want)
	}
	for _, window := range []string{"", "auto", "90m", "1d", "1 DAY", "24h; DROP TABLE logs"} {
		if _, ok := ParseInterval(window); ok {
			t.Errorf("ParseInterval(%q) accepted a window outside the set", window)
		}
	}
}

func TestMacroTimezoneOffsetIsFixedAtRangeStart(t *testing.T) {
	tests := []struct {
		name string
		zone string
		from time.Time
		want string
	}{
		{"west of UTC", "America/St_Johns", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), "-9000s"},
		{"UTC", "UTC", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), "0s"},
		// Berlin leaves summer time on 2026-10-25. A range that starts before the
		// change keeps the summer offset for all of its buckets.
		{"before a daylight-saving change", "Europe/Berlin", time.Date(2026, 10, 23, 22, 0, 0, 0, time.UTC), "7200s"},
		{"after a daylight-saving change", "Europe/Berlin", time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC), "3600s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			to := tt.from.Add(72 * time.Hour)
			got, err := LogsQL.Substitute("{{__tz_offset}}", nil, Builtins{From: &tt.from, To: &to, Location: mustLocation(t, tt.zone)})
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v, want %q", got, err, tt.want)
			}
		})
	}
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if got, err := LogsQL.Substitute("{{__tz_offset}}", nil, Builtins{From: &from}); err != nil || got != "0s" {
		t.Fatalf("no timezone: got %q, %v, want the UTC offset", got, err)
	}
}

// Rule 1: one pass. An expansion is appended and never scanned again.
func TestMacroRuleOnePass(t *testing.T) {
	builtins := macroFixture(t)
	t.Run("a value that holds macro text stays that text", func(t *testing.T) {
		vars := []Variable{{Name: "v", Type: TypeString, Value: "{{__to}}"}}
		for dialect, want := range map[Dialect]string{
			ClickHouseSQL: "msg = '{{__to}}' AND m = 'x {{__to}}'",
			LogsQL:        `msg = "{{__to}}" AND m = 'x {{__to}}'`,
		} {
			got, err := dialect.Substitute("msg = {{v}} AND m = 'x {{v}}'", vars, builtins)
			if err != nil || got != want {
				t.Errorf("got %q, %v, want %q", got, err, want)
			}
		}
	})
	t.Run("a macro expansion is not scanned for variables", func(t *testing.T) {
		builtins := builtins
		builtins.TimestampColumn = "{{v}}"
		got, err := ClickHouseSQL.Substitute("{{__time_filter}}", []Variable{{Name: "v", Type: TypeString, Value: "x"}}, builtins)
		if err != nil || !strings.HasPrefix(got, "`{{v}}` >= ") {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("a value next to a macro", func(t *testing.T) {
		vars := []Variable{{Name: "v", Type: TypeString, Value: "{{__interval}}"}}
		got, err := ClickHouseSQL.Substitute("{{v}} {{__interval}} {{v}}", vars, builtins)
		if want := "'{{__interval}}' INTERVAL 1 DAY '{{__interval}}'"; err != nil || got != want {
			t.Fatalf("got %q, %v, want %q", got, err, want)
		}
	})
}

// Rule 2: expressions only. A macro in a quoted context is an error, and a
// macro in a comment is left as it is.
func TestMacroRuleExpressionsOnly(t *testing.T) {
	builtins := macroFixture(t)
	quoted := []struct {
		name    string
		dialect Dialect
		query   string
	}{
		{"clickhouse string", ClickHouseSQL, "SELECT '{{__from}}'"},
		{"clickhouse double-quoted identifier", ClickHouseSQL, `SELECT "{{__from}}" FROM t`},
		{"clickhouse backtick identifier", ClickHouseSQL, "SELECT `{{__interval}}` FROM t"},
		{"clickhouse heredoc", ClickHouseSQL, "SELECT $tag${{__to}}$tag$"},
		{"clickhouse bare heredoc", ClickHouseSQL, "SELECT $${{__timezone}}$$"},
		{"logsql double-quoted string", LogsQL, `msg:"{{__from}}"`},
		{"logsql single-quoted string", LogsQL, `msg:'{{__to}}'`},
		{"logsql raw string", LogsQL, "msg:`{{__interval}}`"},
	}
	for _, tt := range quoted {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.dialect.Substitute(tt.query, nil, builtins)
			if err == nil || !strings.Contains(err.Error(), "cannot be used inside a string, a quoted identifier, a heredoc or a raw string") {
				t.Fatalf("error = %v, want the quoted-context error", err)
			}
		})
	}
	comments := []struct {
		name    string
		dialect Dialect
		query   string
	}{
		{"clickhouse line comment", ClickHouseSQL, "SELECT 1 -- {{__from}}\nFROM t"},
		{"clickhouse block comment", ClickHouseSQL, "SELECT 1 /* {{__time_filter}} /* {{__to}} */ */ FROM t"},
		{"clickhouse hash comment", ClickHouseSQL, "SELECT 1 # {{__tz_offset}}"},
		{"logsql hash comment", LogsQL, "error # {{__timezone}} {{__time_filter}}"},
	}
	for _, tt := range comments {
		t.Run(tt.name, func(t *testing.T) {
			// No inputs at all: a macro that expanded here would fail.
			got, err := tt.dialect.Substitute(tt.query, nil, Builtins{})
			if err != nil || got != tt.query {
				t.Fatalf("got %q, %v, want the query unchanged", got, err)
			}
		})
	}
}

// Rule 3: a closed set of names. A request variable cannot replace a macro.
func TestMacroRuleClosedNames(t *testing.T) {
	builtins := macroFixture(t)
	for _, name := range []string{"__from", "__to", "__time_filter", "__interval", "__interval_s", "__timezone", "__tz_offset"} {
		if !IsBuiltin(name) {
			t.Errorf("%s is not a built-in name", name)
		}
		for _, dialect := range []Dialect{ClickHouseSQL, LogsQL, LogchefQL} {
			_, err := dialect.Substitute("x", []Variable{{Name: name, Type: TypeString, Value: "1 OR 1=1"}}, builtins)
			if err == nil || !strings.Contains(err.Error(), "reserved for a built-in macro") {
				t.Errorf("%s in dialect %d: error = %v, want the reserved-name error", name, dialect, err)
			}
		}
	}
	t.Run("a stored variable with the prefix that is not a built-in keeps working", func(t *testing.T) {
		vars := []Variable{{Name: "__tenant", Type: TypeString, Value: "acme"}}
		for dialect, want := range map[Dialect]string{ClickHouseSQL: "t = 'acme'", LogsQL: `t = "acme"`, LogchefQL: `t = "acme"`} {
			got, err := dialect.Substitute("t = {{__tenant}}", vars, builtins)
			if err != nil || got != want {
				t.Errorf("got %q, %v, want %q", got, err, want)
			}
		}
		if IsBuiltin("__tenant") || !HasBuiltinPrefix("__tenant") || HasBuiltinPrefix("_tenant") {
			t.Error("prefix or built-in classification is wrong")
		}
	})
}

// Rule 4: typed inputs. The timestamp column is quoted as an identifier, and
// the timezone goes through the dialect's string formatter.
func TestMacroRuleTypedInputs(t *testing.T) {
	builtins := macroFixture(t)
	builtins.TimestampColumn = "ts` OR 1=1 --"
	got, err := ClickHouseSQL.Substitute("{{__time_filter}}", nil, builtins)
	if err != nil || !strings.HasPrefix(got, "`ts\\x60 OR 1=1 --` >= toDateTime64(") {
		t.Fatalf("got %q, %v, want the column as one quoted identifier", got, err)
	}
	builtins.TimestampColumn = ""
	if _, err := ClickHouseSQL.Substitute("{{__time_filter}}", nil, builtins); err == nil || !strings.Contains(err.Error(), "timestamp column") {
		t.Fatalf("error = %v, want the missing-column error", err)
	}
	fixed := time.FixedZone("it's", 3600)
	got, err = ClickHouseSQL.Substitute("{{__timezone}}", nil, Builtins{Location: fixed})
	if err != nil || got != "'it''s'" {
		t.Fatalf("got %q, %v, want the name escaped as a string literal", got, err)
	}
}

// Rule 5: missing inputs fail.
func TestMacroRuleMissingInputs(t *testing.T) {
	tests := []struct {
		dialect Dialect
		query   string
		want    string
	}{
		{ClickHouseSQL, "{{__from}}", "macro {{__from}} needs a time range"},
		{ClickHouseSQL, "{{__to}}", "macro {{__to}} needs a time range"},
		{ClickHouseSQL, "{{__time_filter}}", "macro {{__time_filter}} needs a time range"},
		{ClickHouseSQL, "{{__interval}}", "macro {{__interval}} needs an interval"},
		{ClickHouseSQL, "{{__interval_s}}", "macro {{__interval_s}} needs an interval"},
		{LogsQL, "{{__from}}", "macro {{__from}} needs a time range"},
		{LogsQL, "{{__to}}", "macro {{__to}} needs a time range"},
		{LogsQL, "{{__interval}}", "macro {{__interval}} needs an interval"},
		{LogsQL, "{{__interval_s}}", "macro {{__interval_s}} needs an interval"},
		{LogsQL, "{{__tz_offset}}", "macro {{__tz_offset}} needs a time range"},
	}
	for _, tt := range tests {
		_, err := tt.dialect.Substitute(tt.query, nil, Builtins{TimestampColumn: "ts"})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want %q", tt.query, err, tt.want)
		}
	}
	from := time.Date(2026, 11, 5, 18, 30, 0, 0, time.UTC)
	if _, err := ClickHouseSQL.Substitute("{{__time_filter}}", nil, Builtins{From: &from, TimestampColumn: "ts"}); err == nil {
		t.Error("a range with no end rendered {{__time_filter}}")
	}
}

// Rule 6: optional blocks come from the template. A macro counts as provided,
// and a value never opens or closes a block.
func TestMacroRuleOptionalBlocks(t *testing.T) {
	builtins := macroFixture(t)
	t.Run("a block with only a macro is kept", func(t *testing.T) {
		got, err := ClickHouseSQL.Substitute("SELECT 1 FROM t WHERE 1 [[ AND ts < {{__to}} ]]", nil, builtins)
		if want := "SELECT 1 FROM t WHERE 1  AND ts < toDateTime64('2026-11-12 18:30:00.000', 3, 'UTC') "; err != nil || got != want {
			t.Fatalf("got %q, %v, want %q", got, err, want)
		}
	})
	t.Run("a block with a macro and an unset variable drops out", func(t *testing.T) {
		for _, dialect := range []Dialect{ClickHouseSQL, LogsQL} {
			got, err := dialect.Substitute("a [[ b {{__from}} {{customer}} ]]c", nil, builtins)
			if err != nil || got != "a c" {
				t.Errorf("got %q, %v, want %q", got, err, "a c")
			}
		}
	})
	t.Run("a kept block still fails on a missing input", func(t *testing.T) {
		_, err := ClickHouseSQL.Substitute("a [[ AND {{__time_filter}} ]]", nil, Builtins{})
		if err == nil || !strings.Contains(err.Error(), "needs a time range") {
			t.Fatalf("error = %v, want the missing-range error", err)
		}
	})
	t.Run("a value never opens or closes a block", func(t *testing.T) {
		vars := []Variable{{Name: "open", Type: TypeString, Value: "[[ AND x = {{missing}}"}, {Name: "close", Type: TypeString, Value: "]]"}}
		got, err := ClickHouseSQL.Substitute("a = {{open}} AND b = {{close}} AND {{__interval_s}} > 0", vars, builtins)
		if want := "a = '[[ AND x = {{missing}}' AND b = ']]' AND 86400 > 0"; err != nil || got != want {
			t.Fatalf("got %q, %v, want %q", got, err, want)
		}
	})
}

func TestLogchefQLRejectsEveryMacro(t *testing.T) {
	for _, name := range []string{"__from", "__to", "__time_filter", "__interval", "__interval_s", "__timezone", "__tz_offset"} {
		for _, query := range []string{"ts > {{" + name + "}}", `msg = "{{` + name + `}}"`, "level = \"error\" [[ and ts > {{" + name + "}} ]]"} {
			_, err := LogchefQL.Substitute(query, nil, macroFixture(t))
			if err == nil || !strings.Contains(err.Error(), "not available in LogchefQL") {
				t.Errorf("%s: error = %v, want the LogchefQL rejection", query, err)
			}
		}
	}
}
