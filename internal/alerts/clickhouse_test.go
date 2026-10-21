package alerts

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/util"
	"github.com/mr-karan/logchef/pkg/models"
)

func clickHouseAlertManager(t *testing.T) (*Manager, *models.Alert, *clickhouse.Client) {
	t.Helper()
	address := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if address == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	db, alert, logger := newManagerTestAlert(t)
	source, err := db.GetSource(t.Context(), alert.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	source.Connection = models.ConnectionInfo{Host: address, Database: "system", TableName: "one"}
	if err := source.SyncConnectionConfig(); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	connections := clickhouse.NewManager(logger)
	t.Cleanup(func() { _ = connections.Close() })
	service := datasource.NewService(db, logger)
	service.Register(datasource.NewClickHouseProvider(connections, logger))
	if err := service.InitializeSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	client, err := connections.GetConnection(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Options{DB: db, Logger: logger, Datasources: service, Config: config.AlertsConfig{Enabled: true, EvaluationInterval: time.Hour, HistoryLimit: 100}})
	t.Cleanup(manager.Stop)
	return manager, alert, client
}

func TestManagerEvaluatesNumericTypesClickHouse(t *testing.T) {
	manager, alert, _ := clickHouseAlertManager(t)
	for _, expression := range []string{"1", "toInt16(1)", "toFloat32(1)", "toDecimal32(1, 2)", "toDecimal64(1, 2)", "toDecimal128(1, 2)", "toDecimal256(1, 2)", "toInt128(1)", "toUInt256(1)"} {
		t.Run(expression, func(t *testing.T) {
			alert.Query = "SELECT " + expression + " AS value"
			alert.ThresholdValue = 0.5
			if err := manager.evaluateAlert(t.Context(), alert); err != nil {
				t.Fatal(err)
			}
			saved, err := manager.db.GetAlert(t.Context(), alert.ID)
			if err != nil || saved.LastState != models.AlertStateFiring {
				t.Fatalf("state = %+v, %v; want firing", saved, err)
			}
			history, err := manager.db.ListAlertHistory(t.Context(), alert.ID, 10)
			if err != nil || len(history) == 0 || history[0].Value == nil || *history[0].Value != 1 {
				t.Fatalf("history = %+v, %v; want value 1", history, err)
			}
			if err := manager.handleResolved(t.Context(), saved, 0); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestManagerStopCancelsClickHouseQuery(t *testing.T) {
	manager, alert, client := clickHouseAlertManager(t)
	marker := fmt.Sprintf("alert_stop_%d", time.Now().UnixNano())
	alert.Query = "SELECT sleep(3) AS value /* " + marker + " */"
	if err := manager.db.UpdateAlert(t.Context(), alert); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	manager.Start(ctx)
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := client.Query(ctx, "SELECT count() AS value FROM system.processes WHERE startsWith(query, 'SELECT sleep(3)') AND position(query, '"+marker+"') > 0")
		if err != nil {
			t.Fatal(err)
		}
		count, err := util.ExtractFirstNumeric(result)
		if err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("ClickHouse did not report an in-flight evaluation")
		}
	}
	stopped := make(chan struct{})
	go func() { manager.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		cancel()
		<-stopped
		t.Fatal("Stop did not cancel in-flight ClickHouse query within one second")
	}
}
