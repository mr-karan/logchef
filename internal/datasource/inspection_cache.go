package datasource

import (
	"context"
	"strconv"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

const inspectionCacheTTL = time.Minute

type inspectionCacheEntry struct {
	revision time.Time
	created  time.Time
	value    *SourceInspection
}

type activityCacheEntry struct {
	revision time.Time
	created  time.Time
	value    *SourceActivity
}

// SourceActivityProvider is implemented by providers that can safely retrieve
// recent ingestion activity separately from metadata inspection.
type SourceActivityProvider interface {
	InspectSourceActivity(context.Context, *models.Source) (*SourceActivity, error)
}

// ErrSourceActivityUnavailable means activity cannot be queried safely for a
// source. Callers should render this as an unavailable section, not as zeros.
var ErrSourceActivityUnavailable = &sourceActivityUnavailableError{}

type sourceActivityUnavailableError struct{}

func (*sourceActivityUnavailableError) Error() string { return "source activity unavailable" }

func (s *Service) inspectionForSource(ctx context.Context, source *models.Source, provider Provider, refresh bool) (*SourceInspection, error) {
	if !refresh {
		s.inspectionMu.Lock()
		entry, ok := s.inspections[source.ID]
		s.inspectionMu.Unlock()
		if ok && entry.revision.Equal(source.UpdatedAt) && time.Since(entry.created) < inspectionCacheTTL {
			return entry.value, nil
		}
	}

	key := "inspection:" + strconv.FormatInt(int64(source.ID), 10) + ":" + source.UpdatedAt.UTC().Format(time.RFC3339Nano)
	//nolint:contextcheck // A shared fill has its own deadline so one caller cannot cancel every waiter.
	result := s.inspectionFill.DoChan(key, func() (any, error) {
		if !refresh {
			s.inspectionMu.Lock()
			entry, ok := s.inspections[source.ID]
			s.inspectionMu.Unlock()
			if ok && entry.revision.Equal(source.UpdatedAt) && time.Since(entry.created) < inspectionCacheTTL {
				return entry.value, nil
			}
		}
		fillCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		value, err := provider.InspectSource(fillCtx, source)
		if err != nil {
			return nil, err
		}
		s.inspectionMu.Lock()
		s.inspections[source.ID] = inspectionCacheEntry{revision: source.UpdatedAt, created: time.Now(), value: value}
		s.inspectionMu.Unlock()
		return value, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-result:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.(*SourceInspection), nil
	}
}

func (s *Service) activityForSource(ctx context.Context, source *models.Source, provider Provider, refresh bool) (*SourceActivity, error) {
	activityProvider, ok := provider.(SourceActivityProvider)
	if !ok {
		return nil, ErrOperationNotSupported
	}
	if !refresh {
		s.inspectionMu.Lock()
		entry, ok := s.activities[source.ID]
		s.inspectionMu.Unlock()
		if ok && entry.revision.Equal(source.UpdatedAt) && time.Since(entry.created) < inspectionCacheTTL {
			return entry.value, nil
		}
	}

	key := "activity:" + strconv.FormatInt(int64(source.ID), 10) + ":" + source.UpdatedAt.UTC().Format(time.RFC3339Nano)
	//nolint:contextcheck // A shared fill has its own deadline so one caller cannot cancel every waiter.
	result := s.activityFill.DoChan(key, func() (any, error) {
		if !refresh {
			s.inspectionMu.Lock()
			entry, ok := s.activities[source.ID]
			s.inspectionMu.Unlock()
			if ok && entry.revision.Equal(source.UpdatedAt) && time.Since(entry.created) < inspectionCacheTTL {
				return entry.value, nil
			}
		}
		fillCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		select {
		case s.activitySlots <- struct{}{}:
			defer func() { <-s.activitySlots }()
		case <-fillCtx.Done():
			return nil, fillCtx.Err()
		}
		value, err := activityProvider.InspectSourceActivity(fillCtx, source)
		if err != nil {
			return nil, err
		}
		s.inspectionMu.Lock()
		s.activities[source.ID] = activityCacheEntry{revision: source.UpdatedAt, created: time.Now(), value: value}
		s.inspectionMu.Unlock()
		return value, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-result:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.(*SourceActivity), nil
	}
}

func (s *Service) invalidateInspectionCache(sourceID models.SourceID) {
	s.inspectionMu.Lock()
	delete(s.inspections, sourceID)
	delete(s.activities, sourceID)
	s.inspectionMu.Unlock()
}
