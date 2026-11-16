package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// The usage of a source whose rollup holds more shapes than one run reads
// is truncated, while its totals still count every row.
func TestSchemaAdviceUsageTruncated(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	const source = models.SourceID(7)
	const day = "2026-11-10"
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		for i := range schemaAdviceShapeLimit + 1 {
			shape := models.FilterShape{{Column: fmt.Sprintf("c%d", i), Kind: models.MatchEq}}
			if err := tx.IncrementQueryShapeStats(ctx, models.QueryShapeIncrement{BucketDate: day, SourceID: source, Usage: models.QueryShapeUsage{
				QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: fmt.Sprintf("%016x", i), Shape: shape, QueryCount: 1, TotalDurationMs: 5,
			}}); err != nil {
				return err
			}
		}
		return tx.IncrementQueryShapeStats(ctx, models.QueryShapeIncrement{BucketDate: day, SourceID: source, Usage: models.QueryShapeUsage{
			QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: models.ShapeHashOverflow, Shape: models.FilterShape{}, QueryCount: 9,
		}})
	})
	if err != nil {
		t.Fatal(err)
	}

	usage, err := schemaAdviceUsage(ctx, db, source, day)
	if err != nil {
		t.Fatal(err)
	}
	if !usage.Truncated || len(usage.Shapes) != schemaAdviceShapeLimit || usage.Queries != schemaAdviceShapeLimit+1+9 || usage.Overflow != 9 {
		t.Fatalf("usage: truncated %v, %d shapes, %d queries, %d overflow; want truncated, %d shapes, %d queries, 9 overflow",
			usage.Truncated, len(usage.Shapes), usage.Queries, usage.Overflow, schemaAdviceShapeLimit, schemaAdviceShapeLimit+10)
	}

	usage, err = schemaAdviceUsage(ctx, db, source+1, day)
	if err != nil || usage.Truncated || len(usage.Shapes) != 0 || usage.Queries != 0 {
		t.Fatalf("usage of a source with no rollup = %+v / %v, want empty", usage, err)
	}
}

func TestFitSchemaReport(t *testing.T) {
	finding := func(id string, evidence int) models.SchemaFinding {
		f := models.SchemaFinding{ID: id, Rule: "skip_index_bloom", Impact: models.ImpactLow, Title: id, DDL: []string{}, Evidence: []models.AdvisorEvidence{}}
		for range evidence {
			f.Evidence = append(f.Evidence, models.AdvisorEvidence{Label: "label", Value: strings.Repeat("v", 1000), Source: "logchef"})
		}
		return f
	}
	report := func(findings ...models.SchemaFinding) *models.SchemaAdvisorReport {
		return &models.SchemaAdvisorReport{Status: models.AdvisorStatusOK, Inputs: []models.AdvisorInput{}, Findings: findings}
	}

	small := report(finding("a", 3))
	if err := fitSchemaReport(small); err != nil || len(small.Findings[0].Evidence) != 3 || len(small.Notes) != 0 {
		t.Fatalf("a small report changed: %+v / %v", small, err)
	}

	// 4 findings of about 100 KiB each: the lowest-ranked two lose their
	// evidence, the first two keep it.
	big := report(finding("first", 100), finding("second", 100), finding("third", 100), finding("fourth", 100))
	if err := fitSchemaReport(big); err != nil {
		t.Fatal(err)
	}
	var kept []int
	for _, f := range big.Findings {
		kept = append(kept, len(f.Evidence))
	}
	if fmt.Sprint(kept) != "[100 100 0 0]" {
		t.Fatalf("evidence kept per finding = %v, want [100 100 0 0]", kept)
	}
	if len(big.Notes) != 1 {
		t.Fatalf("notes = %q, want one note on the removed evidence", big.Notes)
	}
	encoded, err := json.Marshal(big)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxSchemaAdviceReportBytes {
		t.Fatalf("fitted report is %d bytes", len(encoded))
	}

	huge := report(finding("ddl", 0))
	huge.Findings[0].DDL = []string{strings.Repeat("x", maxSchemaAdviceReportBytes)}
	if err := fitSchemaReport(huge); err == nil {
		t.Fatal("a report over the limit without evidence was accepted")
	}
}
