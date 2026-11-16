package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/advisor"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// shapesPerSourceDay bounds the distinct shapes one process records per
	// source per UTC day. Past it, new shapes count under the overflow hash.
	shapesPerSourceDay = 500
	// shapeRetentionDays is how long rollup rows are kept; advisor windows
	// are capped at 90 days.
	shapeRetentionDays = 90
)

// shapeRecorder remembers the shape hashes recorded on the current UTC day.
type shapeRecorder struct {
	mu   sync.Mutex
	day  string
	seen map[models.SourceID]map[string]struct{}
}

// startDay moves the recorder to day and reports whether day is new, in which
// case the caller prunes old rows.
func (r *shapeRecorder) startDay(day string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.day == day {
		return false
	}
	r.day, r.seen = day, map[models.SourceID]map[string]struct{}{}
	return true
}

// admit reports whether hash keeps its own row today: it was seen already, or
// the source has seen fewer than shapesPerSourceDay shapes.
func (r *shapeRecorder) admit(sourceID models.SourceID, hash string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	hashes := r.seen[sourceID]
	if hashes == nil {
		hashes = map[string]struct{}{}
		r.seen[sourceID] = hashes
	}
	if _, ok := hashes[hash]; ok {
		return true
	}
	if len(hashes) >= shapesPerSourceDay {
		return false
	}
	hashes[hash] = struct{}{}
	return true
}

// recordQueryShape adds a recorded query to query_shape_stats_daily. A
// failure is logged and counted; it never fails the query.
func (s *Server) recordQueryShape(parent context.Context, now time.Time, entry *models.QueryHistory) {
	// Only ClickHouse sources have a schema to tune, and only LogchefQL and
	// ClickHouse SQL are analyzed.
	if entry.SourceTable == "" {
		return
	}
	language := models.NormalizeQueryLanguage(entry.QueryLanguage)
	if language != models.QueryLanguageLogchefQL && language != models.QueryLanguageClickHouseSQL {
		return
	}
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	day := now.UTC().Format(time.DateOnly)
	usage := models.QueryShapeUsage{
		QueryLanguage:      language,
		ShapeHash:          models.ShapeHashUnanalyzed,
		Shape:              models.FilterShape{},
		QueryCount:         1,
		TotalDurationMs:    entry.DurationMs,
		TotalRows:          entry.RowCount,
		MaxLookbackSeconds: entry.LookbackSeconds,
	}
	if s.shapes.startDay(day) {
		before := now.UTC().AddDate(0, 0, -shapeRetentionDays).Format(time.DateOnly)
		if err := s.store.PruneQueryShapeStats(ctx, before); err != nil {
			s.log.Warn("failed to prune query shape stats", "error", err, "before", before)
		}
	}
	result := "unanalyzed"
	if shape, ok := advisor.ShapeOf(entry.QueryText, language, advisor.Relation{Database: entry.SourceDatabase, Table: entry.SourceTable}); ok {
		hash, err := shapeHash(shape)
		if err != nil {
			s.log.Warn("failed to hash query shape", "error", err, "source_id", entry.SourceID)
			metrics.RecordQueryShape("error")
			return
		}
		usage.ShapeHash, usage.Shape, result = hash, shape, "analyzed"
		if !s.shapes.admit(entry.SourceID, hash) {
			usage.ShapeHash, usage.Shape, result = models.ShapeHashOverflow, models.FilterShape{}, "overflow"
		}
	}
	if err := s.store.IncrementQueryShapeStats(ctx, models.QueryShapeIncrement{BucketDate: day, SourceID: entry.SourceID, Usage: usage}); err != nil {
		s.log.Warn("failed to record query shape", "error", err, "source_id", entry.SourceID)
		result = "error"
	}
	metrics.RecordQueryShape(result)
}

// shapeHash is the first 16 hex characters of SHA-256 of the shape's JSON.
func shapeHash(shape models.FilterShape) (string, error) {
	encoded, err := json.Marshal(shape)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:8]), nil
}
