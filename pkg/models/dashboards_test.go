package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// validPanelsBlob returns a well-formed two-panel dashboard blob.
func validPanelsBlob() json.RawMessage {
	return json.RawMessage(`{
		"version": 1,
		"layout": [
			{"id":"p1","x":0,"y":0,"w":6,"h":2},
			{"id":"p2","x":6,"y":0,"w":6,"h":3}
		],
		"panels": [
			{"id":"p1","title":"5xx by service","type":"timeseries","team_id":1,"source_id":1,"query":"status>=500","query_language":"logchefql","options":{"group_by":"service","limit":50,"columns":[]}},
			{"id":"p2","title":"error count","type":"stat","team_id":1,"source_id":1,"query":"level=\"error\"","query_language":"logchefql","options":{}}
		]
	}`)
}

func TestValidateDashboardPanels_Valid(t *testing.T) {
	if err := ValidateDashboardPanels(validPanelsBlob()); err != nil {
		t.Fatalf("valid blob rejected: %v", err)
	}

	// A table panel with clickhouse-sql is also valid.
	blob := json.RawMessage(`{"version":1,"layout":[{"id":"t","x":0,"y":0,"w":12,"h":6}],"panels":[{"id":"t","title":"rows","type":"table","team_id":2,"source_id":3,"query":"SELECT 1","query_language":"clickhouse-sql"}]}`)
	if err := ValidateDashboardPanels(blob); err != nil {
		t.Fatalf("valid table blob rejected: %v", err)
	}

	// Zero panels is allowed (an empty dashboard); the design caps the max only.
	empty := json.RawMessage(`{"version":1,"layout":[],"panels":[]}`)
	if err := ValidateDashboardPanels(empty); err != nil {
		t.Fatalf("empty blob rejected: %v", err)
	}

	// The finer grid (#78) allows 2/8/9-wide panels alongside the original 3/4/6/12.
	fineGrid := json.RawMessage(`{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":8,"h":2},{"id":"p2","x":8,"y":0,"w":4,"h":2},{"id":"p3","x":0,"y":2,"w":2,"h":1}],"panels":[]}`)
	if err := ValidateDashboardPanels(fineGrid); err != nil {
		t.Fatalf("fine-grid widths rejected: %v", err)
	}
}

func TestValidateDashboardPanels_Violations(t *testing.T) {
	// Build a 25-panel blob to trip the count ceiling.
	tooMany := func() json.RawMessage {
		panels := make([]string, 0, 25)
		for i := 0; i < 25; i++ {
			panels = append(panels, fmt.Sprintf(`{"id":"p%d","title":"x","type":"stat","team_id":1,"source_id":1,"query":"a","query_language":"logchefql"}`, i))
		}
		return json.RawMessage(`{"version":1,"layout":[],"panels":[` + strings.Join(panels, ",") + `]}`)
	}()

	// Build a blob just over the 100KB size limit.
	oversize := json.RawMessage(`{"version":1,"layout":[],"panels":[{"id":"p1","title":"` +
		strings.Repeat("x", MaxDashboardPanelsSize) +
		`","type":"stat","team_id":1,"source_id":1,"query":"a","query_language":"logchefql"}]}`)

	tests := []struct {
		name    string
		blob    json.RawMessage
		wantSub string
	}{
		{"empty payload", json.RawMessage(``), "required"},
		{"not json", json.RawMessage(`{nope`), "not valid JSON"},
		{"oversize", oversize, "exceeds"},
		{"wrong version", json.RawMessage(`{"version":2,"panels":[]}`), "version"},
		{"too many panels", tooMany, "exceeds the max"},
		{"empty panel id", json.RawMessage(`{"version":1,"panels":[{"id":"","type":"stat","team_id":1,"source_id":1,"query_language":"logchefql"}]}`), "empty id"},
		{"duplicate panel id", json.RawMessage(`{"version":1,"panels":[{"id":"p1","type":"stat","team_id":1,"source_id":1,"query_language":"logchefql"},{"id":"p1","type":"stat","team_id":1,"source_id":1,"query_language":"logchefql"}]}`), "duplicate panel id"},
		{"bad type", json.RawMessage(`{"version":1,"panels":[{"id":"p1","type":"pie","team_id":1,"source_id":1,"query_language":"logchefql"}]}`), "invalid type"},
		{"bad team_id", json.RawMessage(`{"version":1,"panels":[{"id":"p1","type":"stat","team_id":0,"source_id":1,"query_language":"logchefql"}]}`), "invalid team_id"},
		{"bad source_id", json.RawMessage(`{"version":1,"panels":[{"id":"p1","type":"stat","team_id":1,"source_id":0,"query_language":"logchefql"}]}`), "invalid source_id"},
		{"bad query_language", json.RawMessage(`{"version":1,"panels":[{"id":"p1","type":"stat","team_id":1,"source_id":1,"query_language":"cypher"}]}`), "invalid query_language"},
		{"bad width", json.RawMessage(`{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":5,"h":2}],"panels":[]}`), "invalid width"},
		{"bad height", json.RawMessage(`{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":7}],"panels":[]}`), "invalid height"},
		{"empty layout id", json.RawMessage(`{"version":1,"layout":[{"id":"","x":0,"y":0,"w":6,"h":2}],"panels":[]}`), "empty id"},
		{"duplicate layout id", json.RawMessage(`{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2},{"id":"p1","x":0,"y":0,"w":6,"h":2}],"panels":[]}`), "duplicate layout id"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDashboardPanels(tc.blob)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}
