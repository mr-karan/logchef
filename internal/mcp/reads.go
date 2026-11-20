package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/pkg/models"
)

// ReadCall is one tool call that reads logs, as the caller's audit log
// records it.
type ReadCall struct {
	Tool   string
	Source access.AuthorizedSource
	Query  string
	// Language is empty for a query in the source's native language.
	Language models.QueryLanguage
	Limit    int
	// Fields are the fields whose values the call reads.
	Fields []string
	// Anchor is the timestamp that a log context call reads around.
	Anchor *time.Time
}

// ReadResult is how a tool call that reads logs ended.
type ReadResult struct {
	// From and To are nil when the call did not get a valid time range.
	From, To *time.Time
	Rows     int64
	Err      error
}

// StartRead starts the record of a tool call that reads logs, once the call
// has authorized its source. The tool calls finish exactly once, when the
// call ends.
type StartRead func(ctx context.Context, call ReadCall) (finish func(ReadResult))

// logRead is the record of one running tool call that reads logs. The
// handler sets what it learns, and its deferred end finishes the record.
type logRead struct {
	ReadResult
	finish func(ReadResult)
}

func (t *tools) startRead(ctx context.Context, req mcp.CallToolRequest, call ReadCall) *logRead {
	call.Tool = req.Params.Name
	return &logRead{finish: t.deps.StartRead(ctx, call)}
}

// end finishes the record. panicked is what the handler's deferred recover
// returned: the read then fails, and the panic goes on to the server's
// recovery.
func (r *logRead) end(panicked any) {
	if panicked != nil {
		r.Err = fmt.Errorf("tool panicked: %v", panicked)
	}
	r.finish(r.ReadResult)
	if panicked != nil {
		panic(panicked)
	}
}

// rfc3339Window is the time range of a native query tool, nil unless both
// ends are valid RFC3339.
func rfc3339Window(start, end string) (from, to *time.Time) {
	s, startErr := time.Parse(time.RFC3339, start)
	e, endErr := time.Parse(time.RFC3339, end)
	if startErr != nil || endErr != nil {
		return nil, nil
	}
	return &s, &e
}

// logchefQLWindow is the time range of a LogchefQL tool, nil unless valid.
func logchefQLWindow(start, end, timezone string) (from, to *time.Time) {
	from, to, err := core.ParseLogchefQLTimeRange(start, end, timezone)
	if err != nil {
		return nil, nil
	}
	return from, to
}
