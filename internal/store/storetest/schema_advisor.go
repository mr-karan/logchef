package storetest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func testSchemaAdvisorReports(t *testing.T, ctx context.Context, s store.Store) {
	src := mkSource(t, ctx, s, "advisor_reports")
	admin := mkUser(t, ctx, s, "advisor-admin@test.dev")

	if _, err := s.GetSchemaAdvisorReport(ctx, src.ID); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("GetSchemaAdvisorReport before a run: err = %v, want ErrNotFound", err)
	}

	first := &models.StoredSchemaAdvisorReport{
		SourceID:    src.ID,
		GeneratedAt: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC),
		GeneratedBy: &admin.ID,
		WindowDays:  30,
		Report: models.SchemaAdvisorReport{
			Status: models.AdvisorStatusOK, ClickHouseVersion: "26.8.1",
			Target:          &models.AdvisorTarget{Kind: models.AdvisorTargetTable, Database: "logs", Table: "advisor_reports", Engine: "MergeTree"},
			Inputs:          []models.AdvisorInput{{Name: "system.tables", State: models.AdvisorInputOK, ReadRows: 1, ReadBytes: 512}},
			QueriesAnalyzed: 120,
			Findings: []models.SchemaFinding{{
				ID: "0123456789abcdef", Rule: "skip_index_bloom", Impact: models.ImpactMedium, Title: "Add a bloom filter index on request_id",
				Column: "request_id", Evidence: []models.AdvisorEvidence{{Label: "Queries with = or IN", Value: "30 of 120 (25%)", Source: "logchef"}},
				DDL: []string{"ALTER TABLE `logs`.`advisor_reports` ADD INDEX ..."}, Candidate: true, Gain: "fewer granules", Cost: "index size",
			}},
			Notes: []string{"usage record is incomplete"},
		},
	}
	if err := s.UpsertSchemaAdvisorReport(ctx, first); err != nil {
		t.Fatalf("UpsertSchemaAdvisorReport: %v", err)
	}
	got, err := s.GetSchemaAdvisorReport(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetSchemaAdvisorReport: %v", err)
	}
	if !reflect.DeepEqual(got, first) {
		t.Fatalf("GetSchemaAdvisorReport = %+v\nwant %+v", got, first)
	}

	// A refresh replaces the row, and a scheduled run has no user.
	second := &models.StoredSchemaAdvisorReport{
		SourceID:    src.ID,
		GeneratedAt: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC),
		WindowDays:  7,
		Report:      models.SchemaAdvisorReport{Status: models.AdvisorStatusUnavailable, Reason: "target_not_readable", Inputs: []models.AdvisorInput{}, Findings: []models.SchemaFinding{}},
	}
	if err := s.UpsertSchemaAdvisorReport(ctx, second); err != nil {
		t.Fatalf("UpsertSchemaAdvisorReport (replace): %v", err)
	}
	if got, err = s.GetSchemaAdvisorReport(ctx, src.ID); err != nil || !reflect.DeepEqual(got, second) {
		t.Fatalf("GetSchemaAdvisorReport after replace = %+v / %v\nwant %+v", got, err, second)
	}

	// Deleting the user who ran it keeps the report without its author.
	first.GeneratedAt = second.GeneratedAt.Add(time.Hour)
	if err := s.UpsertSchemaAdvisorReport(ctx, first); err != nil {
		t.Fatalf("UpsertSchemaAdvisorReport: %v", err)
	}
	if err := s.DeleteUser(ctx, admin.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if got, err = s.GetSchemaAdvisorReport(ctx, src.ID); err != nil || got.GeneratedBy != nil {
		t.Fatalf("after the user is deleted: %+v / %v, want the report with no author", got, err)
	}

	// Deleting the source deletes its report.
	if err := s.DeleteSource(ctx, src.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	if _, err := s.GetSchemaAdvisorReport(ctx, src.ID); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("GetSchemaAdvisorReport after the source is deleted: err = %v, want ErrNotFound", err)
	}
}
