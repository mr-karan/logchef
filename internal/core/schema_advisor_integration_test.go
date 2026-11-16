package core

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// A refresh against ClickHouse reads the seeded rollup, samples the column
// that queries filter on with =, and finds that it needs a bloom filter.
func TestRefreshSchemaAdviceClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := ch.Open(&ch.Options{Addr: []string{addr}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	table := fmt.Sprintf("logchef_advisor_refresh_%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS default."+table) })
	if err := conn.Exec(ctx, "CREATE TABLE default."+table+" (ts DateTime('UTC'), tenant String, request_id String) ENGINE = MergeTree ORDER BY (tenant, ts)"); err != nil {
		t.Fatal(err)
	}
	// Four rows a second over the last 8 days: every 10-minute sample slice
	// of the last 7 days holds 2,400 distinct request ids, 16,800 in all.
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -8)
	insert := fmt.Sprintf("INSERT INTO default.%s SELECT toDateTime('%s', 'UTC') + intDiv(number, 4), 'tenant-a', concat('req-', toString(number)) FROM numbers(%d)",
		table, start.Format(time.DateTime), 4*86400*8)
	if err := conn.Exec(ctx, insert); err != nil {
		t.Fatal(err)
	}

	db := newTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := clickhouse.NewManager(log)
	t.Cleanup(func() { _ = manager.Close() })
	ds := datasource.NewService(db, log)
	ds.Register(datasource.NewClickHouseProvider(manager, log))
	source := &models.Source{
		Name: table, SourceType: models.SourceTypeClickHouse, MetaTSField: "ts",
		Connection: models.ConnectionInfo{Host: addr, Database: "default", TableName: table},
	}
	if err := source.SyncConnectionConfig(); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := ds.InitializeSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	admin := &models.User{Email: "advisor-admin@example.com", FullName: "Admin", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err := db.IncrementQueryShapeStats(ctx, models.QueryShapeIncrement{
		BucketDate: time.Now().UTC().Format(time.DateOnly), SourceID: source.ID,
		Usage: models.QueryShapeUsage{
			QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: "1111111111111111",
			Shape:      models.FilterShape{{Column: "request_id", Kind: models.MatchEq}},
			QueryCount: 200, TotalDurationMs: 200 * 800,
		},
	}); err != nil {
		t.Fatal(err)
	}

	advice, err := RefreshSchemaAdvice(ctx, db, ds, access.SessionPrincipal(admin), source.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	report := advice.Report
	var bloom *models.SchemaFinding
	for i := range report.Findings {
		if report.Findings[i].Rule == "skip_index_bloom" && report.Findings[i].Column == "request_id" {
			bloom = &report.Findings[i]
		}
	}
	if bloom == nil {
		t.Fatalf("no bloom filter finding for request_id: status %q, notes %q, inputs %+v, findings %+v", report.Status, report.Notes, report.Inputs, report.Findings)
	}
	stored, err := GetSchemaAdvice(ctx, db, ds, access.SessionPrincipal(admin), source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Report == nil || len(stored.Report.Findings) != len(report.Findings) || *stored.GeneratedBy != admin.ID || *stored.WindowDays != 30 {
		t.Fatalf("stored advice = %+v, want the refreshed report", stored)
	}
}
