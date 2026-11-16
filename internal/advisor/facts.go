package advisor

import (
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

// Facts is everything Analyze reads about one source: its target table's
// metadata, its recorded usage and the sample of candidate columns.
type Facts struct {
	Metadata *clickhouse.AdvisorMetadata
	Usage    Usage
	// Sample is nil until the candidate columns are sampled, and stays nil
	// when no sample ran.
	Sample *clickhouse.AdvisorSample
}

// Usage is what the shape rollup recorded for the source in the window.
type Usage struct {
	// Shapes are the analyzed shapes, most used first.
	Shapes []ShapeUsage
	// Truncated is true when the rollup held more shapes than Shapes.
	Truncated bool
	// Queries counts every recorded query in the window, analyzed or not,
	// including those of shapes left out of Shapes.
	Queries    int64
	Unanalyzed int64
	// Overflow counts the queries recorded after a source passed its daily
	// cap of distinct shapes.
	Overflow int64
}

// ShapeUsage is how often one filter shape ran.
type ShapeUsage struct {
	Shape      models.FilterShape
	Queries    int64
	DurationMs int64
}
