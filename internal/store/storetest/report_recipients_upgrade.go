package storetest

import (
	"context"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// ReportOwnerRecipientsSeed is portable SQL that seeds reports at the
// schema version before add_report_owner_recipients (RFC 0008 unit R8): a
// report with an owner and no recipient rows, one with a team row, and one
// without an owner.
var ReportOwnerRecipientsSeed = []string{
	`INSERT INTO users (id, email, full_name, role, status) VALUES
		(1, 'owner@test.dev', 'Owner', 'member', 'active'),
		(2, 'other@test.dev', 'Other', 'member', 'active')`,
	`INSERT INTO teams (id, name) VALUES (1, 'payments')`,
	`INSERT INTO sources (id, name, _meta_is_auto_created, _meta_ts_field, source_type, connection_config, identity_key)
		VALUES (1, 'logs', FALSE, 'timestamp', 'clickhouse', '{}', 'clickhouse:ch:9000/logs/app')`,
	`INSERT INTO saved_queries (id, source_id, name, query_language, editor_mode, query_content, created_by)
		VALUES (1, 1, 'errors', 'clickhouse-sql', 'native', '{}', 1)`,
	`INSERT INTO reports (id, name, owner_id, source_kind, saved_query_id, schedule_json, timezone, time_range) VALUES
		(1, 'owner only', 1, 'saved_query', 1, '{"frequency":"daily","hour":9,"minute":0}', 'UTC', '24h'),
		(2, 'has a team', 2, 'saved_query', 1, '{"frequency":"daily","hour":9,"minute":0}', 'UTC', '24h'),
		(3, 'no owner', NULL, 'saved_query', 1, '{"frequency":"daily","hour":9,"minute":0}', 'UTC', '24h')`,
	`INSERT INTO report_recipients (report_id, kind, team_id, email, added_by) VALUES (2, 'team', 1, '', 2)`,
}

// CheckReportOwnerRecipients asserts what the migration did to
// ReportOwnerRecipientsSeed: the report without rows gets its owner as one
// user row, and the others keep what they had.
func CheckReportOwnerRecipients(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()
	rows := func(id models.ReportID) []models.ReportRecipient {
		got, err := s.ListReportRecipients(ctx, id)
		if err != nil {
			t.Fatalf("recipients of report %d: %v", id, err)
		}
		return got
	}
	if got := rows(1); len(got) != 1 || got[0].Kind != models.ReportRecipientUser || got[0].UserID == nil || *got[0].UserID != 1 || got[0].AddedBy == nil || *got[0].AddedBy != 1 {
		t.Errorf("report without rows = %+v, want its owner as one user row", got)
	}
	if got := rows(2); len(got) != 1 || got[0].Kind != models.ReportRecipientTeam {
		t.Errorf("report with a team row = %+v, want only that row", got)
	}
	if got := rows(3); len(got) != 0 {
		t.Errorf("report without an owner = %+v, want no rows", got)
	}
}
