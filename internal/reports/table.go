package reports

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// Table is a query result ready for email: the column names, the first rows
// formatted as text, and how many rows the result held.
type Table struct {
	Columns    []string
	Rows       [][]Cell
	TotalRows  int
	Truncation models.Truncation
}

// Cell is one formatted value. Null marks a NULL, which prints as "NULL".
type Cell struct {
	Text string
	Null bool
}

// RenderTable formats the first maxRows rows of res. An instant prints in
// loc as "2006-01-02 15:04:05 MST", a ClickHouse Date prints as the date,
// and every other value prints as the query returned it.
func RenderTable(res *models.QueryResult, loc *time.Location, maxRows int) Table {
	t := Table{
		Columns:    make([]string, len(res.Columns)),
		TotalRows:  len(res.Logs),
		Truncation: res.Stats.Truncation,
	}
	kinds := make([]columnKind, len(res.Columns))
	for i, c := range res.Columns {
		t.Columns[i] = c.Name
		kinds[i] = kindOf(c.Type)
	}
	shown := min(maxRows, len(res.Logs))
	t.Rows = make([][]Cell, shown)
	for r := range shown {
		row := make([]Cell, len(res.Columns))
		for i, c := range res.Columns {
			row[i] = formatCell(res.Logs[r][c.Name], kinds[i], loc)
		}
		t.Rows[r] = row
	}
	return t
}

type columnKind int

const (
	columnOther columnKind = iota
	columnInstant
	columnDate
)

// kindOf reads a ClickHouse type, unwrapping Nullable and LowCardinality.
// VictoriaLogs marks its time fields "DateTime" too.
func kindOf(chType string) columnKind {
	for _, wrapper := range []string{"Nullable(", "LowCardinality("} {
		if strings.HasPrefix(chType, wrapper) && strings.HasSuffix(chType, ")") {
			chType = chType[len(wrapper) : len(chType)-1]
		}
	}
	switch {
	case strings.HasPrefix(chType, "DateTime"):
		return columnInstant
	case chType == "Date" || chType == "Date32":
		return columnDate
	}
	return columnOther
}

func formatCell(v any, kind columnKind, loc *time.Location) Cell {
	switch v := v.(type) {
	case nil:
		return Cell{Text: "NULL", Null: true}
	case time.Time:
		if kind == columnDate {
			return Cell{Text: v.Format(time.DateOnly)}
		}
		return Cell{Text: formatInstant(v, loc)}
	case string:
		if kind == columnInstant {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return Cell{Text: formatInstant(t, loc)}
			}
		}
		return Cell{Text: v}
	case fmt.Stringer:
		return Cell{Text: v.String()}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return Cell{Text: fmt.Sprint(v)}
	}
	return Cell{Text: string(raw)}
}

func formatInstant(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02 15:04:05 MST")
}
