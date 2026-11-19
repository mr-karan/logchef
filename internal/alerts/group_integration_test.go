package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

// firingServices evaluates the stored rule once and returns the services
// whose instances fire afterward.
func firingServices(t *testing.T, manager *Manager, alertID models.AlertID) []string {
	t.Helper()
	alert, err := manager.db.GetAlert(t.Context(), alertID)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.evaluateAlert(t.Context(), alert, time.Now()); err != nil {
		t.Fatal(err)
	}
	instances, err := manager.db.ListAlertInstances(t.Context(), alertID)
	if err != nil {
		t.Fatal(err)
	}
	var services []string
	for _, instance := range instances {
		if instance.State == models.AlertInstanceStateFiring {
			services = append(services, instance.Group["service"])
		}
	}
	slices.Sort(services)
	return services
}

// TestGroupedRuleClickHouse: a native SQL rule grouped by service fires two
// groups, then one resolves while the other keeps firing.
func TestGroupedRuleClickHouse(t *testing.T) {
	manager, alert, _ := clickHouseAlertManager(t)
	query := func(checkout, search int) string {
		return fmt.Sprintf("SELECT service, v AS value FROM values('service String, v UInt64', ('checkout', %d), ('search', %d))", checkout, search)
	}
	alert.GroupBy = []string{"service"}
	alert.Query = query(12, 30)
	if err := manager.db.UpdateAlert(t.Context(), alert); err != nil {
		t.Fatal(err)
	}
	if got := firingServices(t, manager, alert.ID); !slices.Equal(got, []string{"checkout", "search"}) {
		t.Fatalf("firing after the first tick = %v, want checkout and search", got)
	}
	alert.Query = query(3, 40)
	if err := manager.db.UpdateAlert(t.Context(), alert); err != nil {
		t.Fatal(err)
	}
	if got := firingServices(t, manager, alert.ID); !slices.Equal(got, []string{"search"}) {
		t.Fatalf("firing after checkout recovers = %v, want search only", got)
	}
}

// TestGroupedRuleVictoriaLogs: a LogsQL rule with stats by (service) fires one
// instance per service label.
func TestGroupedRuleVictoriaLogs(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("LOGCHEF_TEST_VICTORIALOGS_URL"))
	if baseURL == "" {
		t.Skip("set LOGCHEF_TEST_VICTORIALOGS_URL to run against VictoriaLogs")
	}
	runID := fmt.Sprintf("alert_groups_%d", time.Now().UnixNano())
	now := time.Now().UTC()
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for service, count := range map[string]int{"checkout": 12, "search": 30, "auth": 2} {
		for i := range count {
			record := map[string]any{"_time": now.Add(-time.Duration(i) * time.Second).Format(time.RFC3339Nano), "_msg": "error", "service": service, "test_run": runID}
			if err := encoder.Encode(record); err != nil {
				t.Fatal(err)
			}
		}
	}
	resp, err := http.Post(strings.TrimRight(baseURL, "/")+"/insert/jsonline?_time_field=_time&_msg_field=_msg", "application/stream+json", &body)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		t.Fatalf("ingest status %d", resp.StatusCode)
	}

	db, template, logger := newManagerTestAlert(t)
	connection, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: baseURL, Scope: models.VictoriaLogsScope{Query: fmt.Sprintf("test_run:=%q", runID)}})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "vl alerts", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: connection}
	if err := source.SyncConnectionConfig(); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	service := datasource.NewService(db, logger)
	service.Register(victorialogs.NewProvider(logger))
	if err := service.InitializeSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	alert := *template
	alert.SourceID = source.ID
	alert.QueryLanguage = models.QueryLanguageLogsQL
	alert.Query = "* | stats by (service) count() as value"
	alert.GroupBy = []string{"service"}
	if err := db.CreateAlert(t.Context(), &alert); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Options{DB: db, Logger: logger, Datasources: service, Config: config.AlertsConfig{HistoryLimit: 100}})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	for {
		// Ingested rows become searchable after a short flush delay.
		got := firingServices(t, manager, alert.ID)
		if slices.Equal(got, []string{"checkout", "search"}) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("firing = %v, want checkout and search", got)
		case <-time.After(500 * time.Millisecond):
		}
	}
}
