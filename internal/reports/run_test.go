package reports

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestRunPanelsQueryDeadline(t *testing.T) {
	fast := PanelJob{ID: "fast", Run: func(context.Context) (PanelOutput, error) {
		return PanelOutput{Rows: 1}, nil
	}}
	slow := PanelJob{ID: "slow", Run: func(ctx context.Context) (PanelOutput, error) {
		<-ctx.Done()
		return PanelOutput{}, ctx.Err()
	}}
	broken := PanelJob{ID: "broken", Run: func(context.Context) (PanelOutput, error) {
		return PanelOutput{}, errors.New("syntax error")
	}}
	deadline := time.Now().Add(200 * time.Millisecond)
	start := time.Now()
	got := RunPanels(context.Background(), deadline, []PanelJob{slow, fast, broken})
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("RunPanels took %s, want it to end at the query deadline", elapsed)
	}
	if !errors.Is(got[0].Err, ErrQueryBudget) {
		t.Errorf("slow panel error = %v, want ErrQueryBudget", got[0].Err)
	}
	if got[1].Err != nil || got[1].Output.Rows != 1 {
		t.Errorf("fast panel = %+v, want its result", got[1])
	}
	if got[2].Err == nil || errors.Is(got[2].Err, ErrQueryBudget) {
		t.Errorf("broken panel error = %v, want its own error", got[2].Err)
	}
}

func TestRunPanelsPastTheDeadlineStartsNothing(t *testing.T) {
	ran := false
	job := PanelJob{ID: "p", Run: func(context.Context) (PanelOutput, error) { ran = true; return PanelOutput{}, nil }}
	got := RunPanels(context.Background(), time.Now().Add(-time.Second), []PanelJob{job})
	if ran || !errors.Is(got[0].Err, ErrQueryBudget) {
		t.Errorf("ran = %v, err = %v, want no run and ErrQueryBudget", ran, got[0].Err)
	}
}

func TestDeliveryOutcome(t *testing.T) {
	ok := []models.ReportPanelOutcome{{Status: models.ReportPanelOK}}
	failedPanel := []models.ReportPanelOutcome{{Status: models.ReportPanelOK}, {Status: models.ReportPanelFailed}}
	lockedPanel := []models.ReportPanelOutcome{{Status: models.ReportPanelOK}, {Status: models.ReportPanelLocked}}
	sent := models.ReportDelivery{Status: models.ReportDeliverySent}
	failed := models.ReportDelivery{Status: models.ReportDeliveryFailed, Reason: "smtp message rejected by server"}
	notConfigured := models.ReportDelivery{Status: models.ReportDeliveryFailed, Reason: ErrMailNotConfigured.Error()}
	unknown := models.ReportDelivery{Status: models.ReportDeliveryUnknown}
	late := models.ReportDelivery{Status: models.ReportDeliverySkipped, Reason: "deadline"}
	noAccess := models.ReportDelivery{Status: models.ReportDeliverySkipped, Reason: "no_access"}
	for _, tc := range []struct {
		name       string
		panels     []models.ReportPanelOutcome
		deliveries []models.ReportDelivery
		status     models.ReportRunStatus
		reason     models.ReportRunReason
	}{
		{"all sent", ok, []models.ReportDelivery{sent}, models.ReportRunSent, ""},
		{"a failed panel", failedPanel, []models.ReportDelivery{sent}, models.ReportRunPartial, models.ReportReasonPanelFailed},
		{"a panel locked for the owner", lockedPanel, []models.ReportDelivery{sent}, models.ReportRunPartial, models.ReportReasonPanelLocked},
		{"one delivery failed", ok, []models.ReportDelivery{sent, failed}, models.ReportRunPartial, models.ReportReasonDeliveryFailed},
		{"one delivery unknown", ok, []models.ReportDelivery{sent, unknown}, models.ReportRunPartial, models.ReportReasonDeliveryUnknown},
		{"one recipient past the deadline", ok, []models.ReportDelivery{sent, late}, models.ReportRunPartial, models.ReportReasonDeadline},
		{"only unknown", ok, []models.ReportDelivery{unknown}, models.ReportRunPartial, models.ReportReasonDeliveryUnknown},
		{"smtp not configured", ok, []models.ReportDelivery{notConfigured}, models.ReportRunFailed, models.ReportReasonSMTPNotConfigured},
		{"every delivery failed", ok, []models.ReportDelivery{failed, notConfigured}, models.ReportRunFailed, models.ReportReasonAllDeliveriesFailed},
		{"nobody reached by the deadline", ok, []models.ReportDelivery{late}, models.ReportRunFailed, models.ReportReasonTimeout},
		{"no recipient may receive it", ok, []models.ReportDelivery{noAccess}, models.ReportRunFailed, models.ReportReasonNoAccess},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, reason := DeliveryOutcome(tc.panels, tc.deliveries)
			if status != tc.status || reason != tc.reason {
				t.Errorf("DeliveryOutcome = %s/%s, want %s/%s", status, reason, tc.status, tc.reason)
			}
		})
	}
}
