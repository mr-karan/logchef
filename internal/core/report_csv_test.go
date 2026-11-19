package core

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestReportAttachesTheSavedQueryAsCSV(t *testing.T) {
	e := newReportEnv(t)
	e.rr.MaxAttachmentBytes = 5 << 20
	rows := make([]map[string]any, 60)
	for i := range rows {
		rows[i] = map[string]any{"status": 500 + i%4, "host": fmt.Sprintf("api-%d", i)}
	}
	e.query = func(context.Context, datasource.QueryRequest) (*models.QueryResult, error) {
		return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "status", Type: "UInt16"}, {Name: "host", Type: "String"}}, Logs: rows}, nil
	}
	in := e.input()
	in.AttachCSV = true
	saved, err := CreateReport(context.Background(), e.db, access.SessionPrincipal(e.owner), in)
	if err != nil {
		t.Fatalf("CreateReport with attach_csv: %v", err)
	}
	r := saved.Report
	if !e.report(r.ID).AttachCSV {
		t.Fatal("attach_csv was not stored")
	}

	run := e.run(r, e.owner, ReportRunToMe)
	if run.Status != models.ReportRunSent {
		t.Fatalf("run = %s/%s", run.Status, run.Reason)
	}
	msg := e.mailer.messages()[0].msg
	if len(msg.Attachments) != 1 || !strings.HasSuffix(msg.Attachments[0].Name, ".csv") || !strings.HasPrefix(msg.Attachments[0].ContentType, "text/csv") {
		t.Fatalf("attachments = %+v", msg.Attachments)
	}
	records, err := csv.NewReader(bytes.NewReader(msg.Attachments[0].Data)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"status", "host"}}
	for _, row := range rows {
		want = append(want, []string{fmt.Sprint(row["status"]), row["host"].(string)})
	}
	if !slices.EqualFunc(records, want, slices.Equal) {
		t.Errorf("csv holds %d records, want every one of the %d result rows; first %q", len(records), len(want), records[:min(3, len(records))])
	}
	if !strings.Contains(msg.Text, "The attached CSV holds 60 rows.") {
		t.Errorf("text body lacks the attachment line")
	}

	// Past the cap the file ends on a whole row, and the email says how many
	// rows it holds.
	e.rr.MaxAttachmentBytes = 100
	run = e.run(r, e.owner, ReportRunToMe)
	if run.Status != models.ReportRunSent {
		t.Fatalf("capped run = %s/%s", run.Status, run.Reason)
	}
	msg = e.mailer.messages()[1].msg
	data := msg.Attachments[0].Data
	records, err = csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(data) > 100 || !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatalf("capped file is %d bytes, parse error %v", len(data), err)
	}
	line := fmt.Sprintf("The attached CSV holds the first %d of 60 rows", len(records)-1)
	if !strings.Contains(msg.Text, line) || !strings.Contains(msg.HTML, line) {
		t.Errorf("email lacks %q", line)
	}
}

func TestReportWithoutCSVHasNoAttachment(t *testing.T) {
	e := newReportEnv(t)
	r := e.create()
	e.run(r, e.owner, ReportRunToMe)
	if msg := e.mailer.messages()[0].msg; len(msg.Attachments) != 0 || strings.Contains(msg.Text, "attached CSV") {
		t.Errorf("a report without attach_csv sent %d attachments", len(msg.Attachments))
	}
}
