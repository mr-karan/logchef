package main

import (
	"errors"

	ql "github.com/mr-karan/logchef/internal/logchefql"
)

type output struct {
	Query string         `json:"query,omitempty"`
	Error *ql.ParseError `json:"error,omitempty"`
}

type compilation struct {
	ClickHouse   output `json:"clickhouse"`
	VictoriaLogs output `json:"victorialogs"`
	Loki         output `json:"loki"`
}

// Keep the example range fixed so a shared query produces the same SQL tomorrow.
func compile(query, interval string) compilation {
	opts := ql.TranslateOptions{AllowAggregation: true, Interval: interval}
	sql := ql.TranslateWithOptions(query, nil, opts)
	ch := output{Error: sql.Error}
	if sql.Valid {
		params := ql.QueryBuildParams{
			TableName: "logs.events", TimestampField: "timestamp",
			StartTime: "2026-01-01 00:00:00", EndTime: "2026-01-01 01:00:00",
			Timezone: "UTC", Limit: 100,
		}
		var err error
		ch.Query, err = ql.BuildFullQueryFromTranslation(params, sql)
		if err != nil && !errors.As(err, &ch.Error) {
			ch.Error = &ql.ParseError{Message: err.Error()}
		}
	}
	vl := ql.TranslateToLogsQL(query, &ql.LogsQLTranslateOptions{DefaultTimestampField: "_time", AllowAggregation: true, Interval: interval})
	loki := ql.TranslateToLogQL(query, ql.LogQLTranslateOptions{
		Selector: []ql.LogQLMatcher{{Name: "service_name", Op: "=~", Value: ".+"}},
		Parser:   "json", StreamLabels: []string{"service_name", "level"},
	})
	return compilation{
		ClickHouse:   ch,
		VictoriaLogs: output{Query: vl.Query, Error: vl.Error},
		Loki:         output{Query: loki.Query, Error: loki.Error},
	}
}
