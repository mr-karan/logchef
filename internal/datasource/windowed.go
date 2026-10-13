package datasource

import (
	"context"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

type WindowedRequest struct {
	QueryRequest
	Cursor        string
	Count         bool
	Step          string
	WindowIndex   *int
	SkipWindow    *int
	StreamFilters []string
}

type SearchWindow struct {
	Index int       `json:"index"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// WindowPosition marks the rows already read from a search window: every row
// newer than Time, and the first Skip rows at exactly Time in the provider's
// deterministic tie order. The zero value is the start of a window.
type WindowPosition struct {
	Time time.Time
	Skip int
}

type WindowedPlan struct {
	Request     WindowedRequest
	Windows     []SearchWindow
	Index       int
	Position    WindowPosition
	Fingerprint string
	Concurrency int
}

type WindowEventType string

const (
	WindowPlan  WindowEventType = "plan"
	WindowState WindowEventType = "window"
	WindowRows  WindowEventType = "rows"
	WindowCount WindowEventType = "count"
	WindowEnd   WindowEventType = "end"
)

type WindowStatus string

const (
	WindowRunning  WindowStatus = "running"
	WindowComplete WindowStatus = "complete"
	WindowPartial  WindowStatus = "partial"
	WindowFailed   WindowStatus = "failed"
	WindowSkipped  WindowStatus = "skipped"
)

type WindowedEvent struct {
	Type     WindowEventType    `json:"type"`
	Window   *SearchWindow      `json:"window,omitempty"`
	Status   WindowStatus       `json:"status,omitempty"`
	Windows  []SearchWindow     `json:"windows,omitempty"`
	Logs     []map[string]any   `json:"logs,omitempty"`
	Buckets  []HistogramBucket  `json:"buckets,omitempty"`
	Stats    *models.QueryStats `json:"stats,omitempty"`
	Cursor   string             `json:"cursor,omitempty"`
	Complete bool               `json:"complete"`
	Message  string             `json:"message,omitempty"`
}

type WindowedProvider interface {
	PrepareWindowedQuery(*models.Source, WindowedRequest) (*WindowedPlan, error)
	StreamWindowedQuery(context.Context, *models.Source, *WindowedPlan, func(WindowedEvent) error) error
}

type StreamFieldsProvider interface {
	StreamFields(context.Context, *models.Source) ([]string, error)
}

func (s *Service) StreamFields(ctx context.Context, sourceID models.SourceID) ([]string, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	streamFields, ok := provider.(StreamFieldsProvider)
	if !ok {
		return nil, ErrOperationNotSupported
	}
	return streamFields.StreamFields(ctx, source)
}

func (s *Service) PrepareWindowedQuery(ctx context.Context, sourceID models.SourceID, req WindowedRequest) (*WindowedPlan, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	windowed, ok := provider.(WindowedProvider)
	if !ok {
		return nil, ErrOperationNotSupported
	}
	return windowed.PrepareWindowedQuery(source, req)
}

func (s *Service) StreamWindowedQuery(ctx context.Context, sourceID models.SourceID, plan *WindowedPlan, emit func(WindowedEvent) error) error {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return err
	}
	windowed, ok := provider.(WindowedProvider)
	if !ok {
		return ErrOperationNotSupported
	}
	return windowed.StreamWindowedQuery(ctx, source, plan, emit)
}
