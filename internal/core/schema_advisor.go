package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/mr-karan/logchef/internal/advisor"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// Limits of a schema analysis (RFC 0010).
const (
	DefaultSchemaAdviceWindowDays = 30
	minSchemaAdviceWindowDays     = 7
	// maxSchemaAdviceWindowDays matches the 90 days the shape rollup keeps.
	maxSchemaAdviceWindowDays = 90
	// schemaAdviceShapeLimit bounds the shape rows one run reads.
	schemaAdviceShapeLimit     = 5000
	schemaAdviceStaleAfter     = 7 * 24 * time.Hour
	maxSchemaAdviceReportBytes = 256 << 10
	maxDismissalReasonBytes    = 500
)

// ErrSchemaAdviceRunFailed means ClickHouse could not be analyzed: it did not
// answer, or the run passed its deadline. The stored report is kept.
var ErrSchemaAdviceRunFailed = errors.New("schema analysis failed")

// ErrSchemaFindingNotFound means the stored report holds no finding with the
// id, or the finding is not dismissed.
var ErrSchemaFindingNotFound = errors.New("schema finding not found")

var findingIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// SchemaAdvice is a source's stored schema advisor report and its dismissed
// findings. Before the first analysis every report field is null.
type SchemaAdvice struct {
	SourceID    models.SourceID             `json:"source_id"`
	GeneratedAt *time.Time                  `json:"generated_at"`
	GeneratedBy *models.UserID              `json:"generated_by"`
	WindowDays  *int                        `json:"window_days"`
	Stale       bool                        `json:"stale"`
	Report      *models.SchemaAdvisorReport `json:"report"`
	// Dismissals can name findings that the current report no longer holds.
	Dismissals []models.SchemaAdvisorDismissal `json:"dismissals"`
}

// GetSchemaAdvice returns the stored schema advisor report of a ClickHouse
// source. The principal must be a global admin and hold sources:read.
func GetSchemaAdvice(ctx context.Context, db store.SchemaAdvisorStore, ds *datasource.Service, p access.Principal, sourceID models.SourceID) (*SchemaAdvice, error) {
	if err := requireSchemaAdvisorAccess(p); err != nil {
		return nil, err
	}
	if _, err := schemaAdviceSource(ctx, ds, sourceID); err != nil {
		return nil, err
	}
	dismissals, err := db.ListSchemaAdvisorDismissals(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	stored, err := db.GetSchemaAdvisorReport(ctx, sourceID)
	if errors.Is(err, models.ErrNotFound) {
		return &SchemaAdvice{SourceID: sourceID, Dismissals: dismissals}, nil
	}
	if err != nil {
		return nil, err
	}
	return schemaAdviceOf(stored, dismissals, time.Now()), nil
}

// RefreshSchemaAdvice analyzes a ClickHouse source over the last windowDays
// days of recorded usage and replaces its stored report. The principal must
// be a global admin and hold sources:read. A run that fails keeps the stored
// report. A caller that joins a running analysis of the source gets that
// run's report, window and author.
func RefreshSchemaAdvice(ctx context.Context, db store.Store, ds *datasource.Service, p access.Principal, sourceID models.SourceID, windowDays int) (*SchemaAdvice, error) {
	if err := requireSchemaAdvisorAccess(p); err != nil {
		return nil, err
	}
	if windowDays < minSchemaAdviceWindowDays || windowDays > maxSchemaAdviceWindowDays {
		return nil, &ValidationError{Field: "window_days", Message: fmt.Sprintf("window_days must be between %d and %d", minSchemaAdviceWindowDays, maxSchemaAdviceWindowDays)}
	}
	source, err := schemaAdviceSource(ctx, ds, sourceID)
	if err != nil {
		return nil, err
	}
	stored, err := ds.RunSchemaAdvice(ctx, source, func(ctx context.Context, provider datasource.SchemaAdvisorProvider) (*models.StoredSchemaAdvisorReport, error) {
		today := time.Now().UTC()
		since := today.AddDate(0, 0, -(windowDays - 1)).Format(time.DateOnly)
		retentionSince := today.AddDate(0, 0, -(maxSchemaAdviceWindowDays - 1)).Format(time.DateOnly)
		usage, err := schemaAdviceUsage(ctx, db, sourceID, since, retentionSince)
		if err != nil {
			return nil, err
		}
		report, err := provider.AdviseSchema(ctx, source, usage)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrSchemaAdviceRunFailed, err)
		}
		if err := fitSchemaReport(report); err != nil {
			return nil, err
		}
		stored := &models.StoredSchemaAdvisorReport{
			SourceID:    sourceID,
			GeneratedAt: time.Now().UTC(),
			GeneratedBy: &p.User.ID,
			WindowDays:  windowDays,
			Report:      *report,
		}
		if err := db.UpsertSchemaAdvisorReport(ctx, stored); err != nil {
			return nil, err
		}
		return stored, nil
	})
	if err != nil {
		// The run's own deadline is a failed analysis; the caller leaving is not.
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrSchemaAdviceRunFailed) {
			return nil, fmt.Errorf("%w: %w", ErrSchemaAdviceRunFailed, err)
		}
		return nil, err
	}
	dismissals, err := db.ListSchemaAdvisorDismissals(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return schemaAdviceOf(stored, dismissals, time.Now()), nil
}

// DismissSchemaFinding dismisses a finding of the source's stored report, or
// replaces the reason of its dismissal. It returns the dismissal it
// replaced (nil for a new one), the new dismissal and the finding's rule.
// The principal must be a global admin and hold sources:write.
func DismissSchemaFinding(ctx context.Context, db store.Store, ds *datasource.Service, p access.Principal, sourceID models.SourceID, findingID, reason string) (before, after *models.SchemaAdvisorDismissal, rule string, err error) {
	if err := requireSchemaDismissalAccess(p, findingID); err != nil {
		return nil, nil, "", err
	}
	if len(reason) > maxDismissalReasonBytes {
		return nil, nil, "", &ValidationError{Field: "reason", Message: fmt.Sprintf("reason must be at most %d bytes", maxDismissalReasonBytes)}
	}
	if _, err := schemaAdviceSource(ctx, ds, sourceID); err != nil {
		return nil, nil, "", err
	}
	err = withSchemaDismissalLock(ctx, db, sourceID, func(tx store.StoreOps) error {
		stored, err := tx.GetSchemaAdvisorReport(ctx, sourceID)
		if errors.Is(err, models.ErrNotFound) {
			return ErrSchemaFindingNotFound
		}
		if err != nil {
			return err
		}
		i := slices.IndexFunc(stored.Report.Findings, func(f models.SchemaFinding) bool { return f.ID == findingID })
		if i < 0 {
			return ErrSchemaFindingNotFound
		}
		rule = stored.Report.Findings[i].Rule
		if before, err = tx.GetSchemaAdvisorDismissal(ctx, sourceID, findingID); errors.Is(err, models.ErrNotFound) {
			before = nil
		} else if err != nil {
			return err
		}
		after = &models.SchemaAdvisorDismissal{
			SourceID:    sourceID,
			FindingID:   findingID,
			Reason:      reason,
			DismissedBy: &p.User.ID,
			DismissedAt: time.Now().UTC(),
		}
		return tx.UpsertSchemaAdvisorDismissal(ctx, after)
	})
	if err != nil {
		return nil, nil, "", err
	}
	return before, after, rule, nil
}

// RestoreSchemaFinding deletes the dismissal of a finding and returns it. A
// finding that is not dismissed is ErrSchemaFindingNotFound. The principal
// must be a global admin and hold sources:write.
func RestoreSchemaFinding(ctx context.Context, db store.Store, ds *datasource.Service, p access.Principal, sourceID models.SourceID, findingID string) (*models.SchemaAdvisorDismissal, error) {
	if err := requireSchemaDismissalAccess(p, findingID); err != nil {
		return nil, err
	}
	if _, err := schemaAdviceSource(ctx, ds, sourceID); err != nil {
		return nil, err
	}
	var dismissal *models.SchemaAdvisorDismissal
	err := withSchemaDismissalLock(ctx, db, sourceID, func(tx store.StoreOps) error {
		var err error
		if dismissal, err = tx.GetSchemaAdvisorDismissal(ctx, sourceID, findingID); err != nil {
			return err
		}
		return tx.DeleteSchemaAdvisorDismissal(ctx, sourceID, findingID)
	})
	if errors.Is(err, models.ErrNotFound) {
		return nil, ErrSchemaFindingNotFound
	}
	if err != nil {
		return nil, err
	}
	return dismissal, nil
}

// withSchemaDismissalLock runs fn in a transaction that holds the source
// row, so the dismissal it reads is the one its write replaces (RFC 0009,
// snapshots come from the mutation).
func withSchemaDismissalLock(ctx context.Context, db store.Store, sourceID models.SourceID, fn func(tx store.StoreOps) error) error {
	return db.WithTx(ctx, func(tx store.StoreOps) error {
		if _, err := tx.GetSourceForUpdate(ctx, sourceID); errors.Is(err, models.ErrNotFound) {
			return ErrSourceNotFound
		} else if err != nil {
			return err
		}
		return fn(tx)
	})
}

func requireSchemaDismissalAccess(p access.Principal, findingID string) error {
	if err := p.RequireGlobalAdmin(); err != nil {
		return err
	}
	if err := p.Require(models.TokenScopeSourcesWrite); err != nil {
		return err
	}
	if !findingIDPattern.MatchString(findingID) {
		return &ValidationError{Field: "finding_id", Message: "finding_id must be 16 hexadecimal characters"}
	}
	return nil
}

func requireSchemaAdvisorAccess(p access.Principal) error {
	if err := p.RequireGlobalAdmin(); err != nil {
		return err
	}
	return p.Require(models.TokenScopeSourcesRead)
}

// schemaAdviceSource loads a source that has a schema advisor.
func schemaAdviceSource(ctx context.Context, ds *datasource.Service, sourceID models.SourceID) (*models.Source, error) {
	source, err := LoadSource(ctx, ds, sourceID)
	if err != nil {
		return nil, err
	}
	if err := ds.CheckSchemaAdvice(source); errors.Is(err, datasource.ErrOperationNotSupported) {
		return nil, &ValidationError{Field: "source_id", Message: "Schema advice is available for ClickHouse sources only", Err: err}
	} else if err != nil {
		return nil, err
	}
	return source, nil
}

// schemaAdviceUsage reads the shape rollup of a source since a date. The
// totals cover every row, while the shapes stop at the most used
// schemaAdviceShapeLimit; usage is truncated when the list reaches it. The
// retention coverage covers every row since retentionSince.
func schemaAdviceUsage(ctx context.Context, db store.QueryHistoryStore, sourceID models.SourceID, since, retentionSince string) (advisor.Usage, error) {
	shapes, err := db.ListQueryShapeStats(ctx, sourceID, since, schemaAdviceShapeLimit)
	if err != nil {
		return advisor.Usage{}, err
	}
	coverage, err := db.QueryShapeCoverage(ctx, sourceID, since)
	if err != nil {
		return advisor.Usage{}, err
	}
	retention, err := db.QueryShapeCoverage(ctx, sourceID, retentionSince)
	if err != nil {
		return advisor.Usage{}, err
	}
	usage := advisor.Usage{
		Shapes:     make([]advisor.ShapeUsage, 0, len(shapes)),
		Truncated:  len(shapes) >= schemaAdviceShapeLimit,
		Queries:    coverage.QueryCount,
		Unanalyzed: coverage.UnanalyzedCount,
		Overflow:   coverage.OverflowCount,
		Days:       coverage.FilteredDays,
		Retention:  retention,
	}
	for _, shape := range shapes {
		usage.Shapes = append(usage.Shapes, advisor.ShapeUsage{Shape: shape.Shape, Queries: shape.QueryCount, DurationMs: shape.TotalDurationMs})
	}
	return usage, nil
}

// fitSchemaReport bounds report to maxSchemaAdviceReportBytes. Past the
// limit it removes the evidence of the lowest-impact findings first;
// findings arrive sorted by impact.
func fitSchemaReport(report *models.SchemaAdvisorReport) error {
	encoded, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode schema advisor report: %w", err)
	}
	if len(encoded) <= maxSchemaAdviceReportBytes {
		return nil
	}
	report.Notes = append(report.Notes, "evidence removed from the lowest-impact findings to fit the report size limit")
	for i := len(report.Findings) - 1; i >= 0 && len(encoded) > maxSchemaAdviceReportBytes; i-- {
		if len(report.Findings[i].Evidence) == 0 {
			continue
		}
		report.Findings[i].Evidence = []models.AdvisorEvidence{}
		if encoded, err = json.Marshal(report); err != nil {
			return fmt.Errorf("encode schema advisor report: %w", err)
		}
	}
	if len(encoded) > maxSchemaAdviceReportBytes {
		return fmt.Errorf("schema advisor report is %d bytes without evidence, over the %d byte limit", len(encoded), maxSchemaAdviceReportBytes)
	}
	return nil
}

func schemaAdviceOf(stored *models.StoredSchemaAdvisorReport, dismissals []models.SchemaAdvisorDismissal, now time.Time) *SchemaAdvice {
	generatedAt, windowDays, report := stored.GeneratedAt, stored.WindowDays, stored.Report
	return &SchemaAdvice{
		SourceID:    stored.SourceID,
		GeneratedAt: &generatedAt,
		GeneratedBy: stored.GeneratedBy,
		WindowDays:  &windowDays,
		Stale:       now.Sub(generatedAt) > schemaAdviceStaleAfter,
		Report:      &report,
		Dismissals:  dismissals,
	}
}
