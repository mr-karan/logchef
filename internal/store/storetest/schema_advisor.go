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

func testSchemaAdvisorDismissals(t *testing.T, ctx context.Context, s store.Store) {
	src := mkSource(t, ctx, s, "advisor_dismissals")
	other := mkSource(t, ctx, s, "advisor_dismissals_other")
	admin := mkUser(t, ctx, s, "advisor-dismisser@test.dev")
	const finding = "0123456789abcdef"

	if _, err := s.GetSchemaAdvisorDismissal(ctx, src.ID, finding); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("GetSchemaAdvisorDismissal before a dismissal: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteSchemaAdvisorDismissal(ctx, src.ID, finding); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("DeleteSchemaAdvisorDismissal before a dismissal: err = %v, want ErrNotFound", err)
	}

	first := &models.SchemaAdvisorDismissal{
		SourceID: src.ID, FindingID: finding, Reason: "index exists on the replica",
		DismissedBy: &admin.ID, DismissedAt: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC),
	}
	if err := s.UpsertSchemaAdvisorDismissal(ctx, first); err != nil {
		t.Fatalf("UpsertSchemaAdvisorDismissal: %v", err)
	}
	// The same finding id on another source is a separate dismissal.
	elsewhere := &models.SchemaAdvisorDismissal{SourceID: other.ID, FindingID: finding, DismissedAt: first.DismissedAt}
	if err := s.UpsertSchemaAdvisorDismissal(ctx, elsewhere); err != nil {
		t.Fatalf("UpsertSchemaAdvisorDismissal (other source): %v", err)
	}
	got, err := s.GetSchemaAdvisorDismissal(ctx, src.ID, finding)
	if err != nil || !reflect.DeepEqual(got, first) {
		t.Fatalf("GetSchemaAdvisorDismissal = %+v / %v\nwant %+v", got, err, first)
	}

	// Dismissing again replaces the reason, author and time; it adds no row.
	second := &models.SchemaAdvisorDismissal{
		SourceID: src.ID, FindingID: finding, Reason: "",
		DismissedAt: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC),
	}
	if err := s.UpsertSchemaAdvisorDismissal(ctx, second); err != nil {
		t.Fatalf("UpsertSchemaAdvisorDismissal (replace): %v", err)
	}
	later := &models.SchemaAdvisorDismissal{
		SourceID: src.ID, FindingID: "fedcba9876543210", Reason: "accepted",
		DismissedBy: &admin.ID, DismissedAt: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC),
	}
	if err := s.UpsertSchemaAdvisorDismissal(ctx, later); err != nil {
		t.Fatalf("UpsertSchemaAdvisorDismissal: %v", err)
	}
	list, err := s.ListSchemaAdvisorDismissals(ctx, src.ID)
	if want := []models.SchemaAdvisorDismissal{*second, *later}; err != nil || !reflect.DeepEqual(list, want) {
		t.Fatalf("ListSchemaAdvisorDismissals = %+v / %v\nwant %+v", list, err, want)
	}

	// Restoring deletes one dismissal of one source.
	if err := s.DeleteSchemaAdvisorDismissal(ctx, src.ID, finding); err != nil {
		t.Fatalf("DeleteSchemaAdvisorDismissal: %v", err)
	}
	if _, err := s.GetSchemaAdvisorDismissal(ctx, src.ID, finding); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("GetSchemaAdvisorDismissal after restore: err = %v, want ErrNotFound", err)
	}
	if _, err := s.GetSchemaAdvisorDismissal(ctx, other.ID, finding); err != nil {
		t.Fatalf("restoring on one source removed the other source's dismissal: %v", err)
	}

	// Deleting the user keeps the dismissal without its author.
	if err := s.DeleteUser(ctx, admin.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if got, err = s.GetSchemaAdvisorDismissal(ctx, src.ID, later.FindingID); err != nil || got.DismissedBy != nil {
		t.Fatalf("after the user is deleted: %+v / %v, want the dismissal with no author", got, err)
	}

	// Deleting the source deletes its dismissals.
	if err := s.DeleteSource(ctx, src.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	if list, err = s.ListSchemaAdvisorDismissals(ctx, src.ID); err != nil || len(list) != 0 {
		t.Fatalf("ListSchemaAdvisorDismissals after the source is deleted = %+v / %v, want none", list, err)
	}
}
