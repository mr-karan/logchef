package datasource

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

type inspectionCacheProvider struct {
	Provider
	mu      sync.Mutex
	calls   int
	err     error
	entered chan struct{}
	release <-chan struct{}
}

func (p *inspectionCacheProvider) InspectSource(_ context.Context, _ *models.Source) (*SourceInspection, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if p.entered != nil {
		p.entered <- struct{}{}
	}
	if p.release != nil {
		<-p.release
	}
	if p.err != nil {
		return nil, p.err
	}
	return &SourceInspection{Details: []InspectionDetail{{Label: "Backend", Value: "test"}}}, nil
}

func (p *inspectionCacheProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type activityCacheProvider struct {
	inspectionCacheProvider
	activityCalls   int
	activityErr     error
	activityEntered chan struct{}
	activityRelease <-chan struct{}
}

func (p *activityCacheProvider) InspectSourceActivity(_ context.Context, _ *models.Source) (*SourceActivity, error) {
	p.mu.Lock()
	p.activityCalls++
	p.mu.Unlock()
	if p.activityEntered != nil {
		p.activityEntered <- struct{}{}
	}
	if p.activityRelease != nil {
		<-p.activityRelease
	}
	if p.activityErr != nil {
		return nil, p.activityErr
	}
	return &SourceActivity{Rows1h: 1, Rows24h: 2}, nil
}

func (p *activityCacheProvider) activityCallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.activityCalls
}

func newInspectionCacheService() *Service {
	return &Service{
		inspections:   make(map[models.SourceID]inspectionCacheEntry),
		activities:    make(map[models.SourceID]activityCacheEntry),
		activitySlots: make(chan struct{}, 2),
	}
}

func testInspectionSource(id models.SourceID) *models.Source {
	return &models.Source{ID: id, Timestamps: models.Timestamps{UpdatedAt: time.Now().UTC()}}
}

func TestInspectionCacheReuseRefreshAndFailures(t *testing.T) {
	t.Run("successful cache reuse", func(t *testing.T) {
		service, provider := newInspectionCacheService(), &inspectionCacheProvider{}
		source := testInspectionSource(1)
		for range 2 {
			if _, err := service.inspectionForSource(context.Background(), source, provider, false); err != nil {
				t.Fatal(err)
			}
		}
		if provider.callCount() != 1 {
			t.Fatalf("provider calls = %d, want 1", provider.callCount())
		}
	})
	t.Run("refresh bypasses cache", func(t *testing.T) {
		service, provider := newInspectionCacheService(), &inspectionCacheProvider{}
		source := testInspectionSource(2)
		if _, err := service.inspectionForSource(context.Background(), source, provider, false); err != nil {
			t.Fatal(err)
		}
		if _, err := service.inspectionForSource(context.Background(), source, provider, true); err != nil {
			t.Fatal(err)
		}
		if provider.callCount() != 2 {
			t.Fatalf("provider calls = %d, want 2", provider.callCount())
		}
	})
	t.Run("failures are not cached", func(t *testing.T) {
		service := newInspectionCacheService()
		provider := &inspectionCacheProvider{err: errors.New("test failure")}
		source := testInspectionSource(3)
		for range 2 {
			if _, err := service.inspectionForSource(context.Background(), source, provider, false); err == nil {
				t.Fatal("expected provider failure")
			}
		}
		if provider.callCount() != 2 {
			t.Fatalf("provider calls = %d, want 2", provider.callCount())
		}
	})
}

func TestInspectionCacheSingleflightAndLargeSourceIDs(t *testing.T) {
	t.Run("concurrent identical calls fill once", func(t *testing.T) {
		service := newInspectionCacheService()
		release := make(chan struct{})
		provider := &inspectionCacheProvider{entered: make(chan struct{}, 1), release: release}
		source := testInspectionSource(4)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = service.inspectionForSource(context.Background(), source, provider, false)
		}()
		<-provider.entered
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = service.inspectionForSource(context.Background(), source, provider, false)
			}()
		}
		close(release)
		wg.Wait()
		if provider.callCount() != 1 {
			t.Fatalf("provider calls = %d, want 1", provider.callCount())
		}
	})
	t.Run("large IDs have distinct cache entries", func(t *testing.T) {
		service, provider := newInspectionCacheService(), &inspectionCacheProvider{}
		for _, id := range []models.SourceID{0x110000, 0x110001} {
			if _, err := service.inspectionForSource(context.Background(), testInspectionSource(id), provider, false); err != nil {
				t.Fatal(err)
			}
		}
		if provider.callCount() != 2 || len(service.inspections) != 2 {
			t.Fatalf("calls=%d entries=%d, want distinct fills and entries", provider.callCount(), len(service.inspections))
		}
	})
}

func TestActivityCacheReuseRefreshFailuresAndSingleflight(t *testing.T) {
	t.Run("successful cache reuse", func(t *testing.T) {
		service, provider := newInspectionCacheService(), &activityCacheProvider{}
		source := testInspectionSource(5)
		for range 2 {
			if _, err := service.activityForSource(context.Background(), source, provider, false); err != nil {
				t.Fatal(err)
			}
		}
		if provider.activityCallCount() != 1 {
			t.Fatalf("activity provider calls = %d, want 1", provider.activityCallCount())
		}
	})
	t.Run("refresh bypasses cache", func(t *testing.T) {
		service, provider := newInspectionCacheService(), &activityCacheProvider{}
		source := testInspectionSource(6)
		if _, err := service.activityForSource(context.Background(), source, provider, false); err != nil {
			t.Fatal(err)
		}
		if _, err := service.activityForSource(context.Background(), source, provider, true); err != nil {
			t.Fatal(err)
		}
		if provider.activityCallCount() != 2 {
			t.Fatalf("activity provider calls = %d, want 2", provider.activityCallCount())
		}
	})
	t.Run("failures are not cached", func(t *testing.T) {
		service := newInspectionCacheService()
		provider := &activityCacheProvider{activityErr: errors.New("test failure")}
		source := testInspectionSource(7)
		for range 2 {
			if _, err := service.activityForSource(context.Background(), source, provider, false); err == nil {
				t.Fatal("expected activity provider failure")
			}
		}
		if provider.activityCallCount() != 2 {
			t.Fatalf("activity provider calls = %d, want 2", provider.activityCallCount())
		}
	})
	t.Run("concurrent identical calls fill once", func(t *testing.T) {
		service := newInspectionCacheService()
		release := make(chan struct{})
		provider := &activityCacheProvider{
			activityEntered: make(chan struct{}, 1),
			activityRelease: release,
		}
		source := testInspectionSource(8)
		start := make(chan struct{})
		ready := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ready <- struct{}{}
				<-start
				_, _ = service.activityForSource(context.Background(), source, provider, false)
			}()
		}
		for range 8 {
			<-ready
		}
		close(start)
		<-provider.activityEntered
		close(release)
		wg.Wait()
		if provider.activityCallCount() != 1 {
			t.Fatalf("activity provider calls = %d, want 1", provider.activityCallCount())
		}
	})
}
