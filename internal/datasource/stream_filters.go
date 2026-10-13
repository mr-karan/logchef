package datasource

import (
	"fmt"
	"strings"

	"github.com/mr-karan/logchef/pkg/models"
)

const (
	maxStreamFilters     = 16
	maxStreamFilterBytes = 65536
)

// ValidateStreamFilters checks the shape of extra_stream_filters. Each filter
// must be one VictoriaLogs stream selector such as {app="api"}, or a JSON
// object such as {"app":["api"]}. VictoriaLogs ANDs the filters with the
// source's immutable scope, so a filter can only narrow the result.
func ValidateStreamFilters(filters []string) error {
	if len(filters) > maxStreamFilters {
		return &ValidationError{Field: "extra_stream_filters", Message: fmt.Sprintf("too many stream filters (maximum %d)", maxStreamFilters)}
	}
	for _, filter := range filters {
		if len(filter) > maxStreamFilterBytes || !strings.HasPrefix(filter, "{") || !strings.HasSuffix(filter, "}") {
			return &ValidationError{Field: "extra_stream_filters", Message: fmt.Sprintf("each stream filter must be a {...} stream selector of at most %d bytes", maxStreamFilterBytes)}
		}
	}
	return nil
}

// ValidateSourceStreamFilters validates filters for a source of sourceType.
// Only VictoriaLogs has log streams, so other sources reject a nonempty list
// instead of ignoring it.
func ValidateSourceStreamFilters(sourceType models.SourceType, filters []string) error {
	if len(filters) > 0 && sourceType != models.SourceTypeVictoriaLogs {
		return &ValidationError{Field: "extra_stream_filters", Message: "invalid for this source: only VictoriaLogs sources support stream filters"}
	}
	return ValidateStreamFilters(filters)
}
