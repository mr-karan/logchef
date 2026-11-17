package datasource

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/mr-karan/logchef/internal/advisor"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"
)

// SchemaAdvisorProvider is implemented by providers that can analyze a
// source's table for the schema advisor (RFC 0010). Only ClickHouse does.
// at is the evaluation time that the sample and the rules measure from.
type SchemaAdvisorProvider interface {
	AdviseSchema(ctx context.Context, source *models.Source, usage advisor.Usage, at time.Time) (*models.SchemaAdvisorReport, error)
}

// ErrSchemaAdvisorBusy means the process already runs as many schema
// analyses as it allows.
var ErrSchemaAdvisorBusy = errors.New("too many schema analyses are running")

const (
	schemaAdvisorRunTimeout = 30 * time.Second
	schemaAdvisorMaxRuns    = 2
)

// CheckSchemaAdvice returns ErrOperationNotSupported when the source's
// provider has no schema advisor.
func (s *Service) CheckSchemaAdvice(source *models.Source) error {
	_, err := s.schemaAdvisor(source)
	return err
}

func (s *Service) schemaAdvisor(source *models.Source) (SchemaAdvisorProvider, error) {
	provider, err := s.ProviderForSource(source)
	if err != nil {
		return nil, err
	}
	advisorProvider, ok := provider.(SchemaAdvisorProvider)
	if !ok {
		return nil, ErrOperationNotSupported
	}
	return advisorProvider, nil
}

// RunSchemaAdvice runs refresh, one schema analysis of a source, with the
// source's advisor. refresh reads the usage, calls the provider and stores
// the report it returns. A process runs one analysis per source at a time,
// and a caller that arrives during a run gets that run's stored report, with
// its window and author. It runs at most two at once and returns
// ErrSchemaAdvisorBusy past that. A run has its own 30 s deadline, so a
// caller that leaves does not cancel it for the others.
func (s *Service) RunSchemaAdvice(ctx context.Context, source *models.Source, refresh func(context.Context, SchemaAdvisorProvider) (*models.StoredSchemaAdvisorReport, error)) (*models.StoredSchemaAdvisorReport, error) {
	provider, err := s.schemaAdvisor(source)
	if err != nil {
		return nil, err
	}
	result := s.advisorRuns.DoChan(strconv.FormatInt(int64(source.ID), 10), func() (any, error) {
		select {
		case s.advisorSlots <- struct{}{}:
			defer func() { <-s.advisorSlots }()
		default:
			return nil, ErrSchemaAdvisorBusy
		}
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), schemaAdvisorRunTimeout)
		defer cancel()
		start := time.Now()
		stored, err := refresh(runCtx, provider)
		metrics.ObserveSchemaAdvisorRunDuration(time.Since(start))
		if err != nil {
			metrics.RecordSchemaAdvisorRun("error")
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("schema analysis passed its %s deadline: %w", schemaAdvisorRunTimeout, err)
			}
			return nil, err
		}
		metrics.RecordSchemaAdvisorRun(stored.Report.Status)
		return stored, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-result:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.(*models.StoredSchemaAdvisorReport), nil
	}
}
