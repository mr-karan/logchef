package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

func sharesConfig(maxPayloadBytes int) config.SharesConfig {
	return config.SharesConfig{DefaultTTL: time.Hour, MaxQueryTextBytes: maxPayloadBytes}
}

func (w *savedViewWorld) sharePath() string {
	return fmt.Sprintf("/api/v1/teams/%d/sources/%d/query-shares", w.teamA.ID, w.src.ID)
}

func sharePayload(query, layout string) string {
	payload := fmt.Sprintf(`{"version":2,"mode":"logchefql","query":%q,"limit":100,"time_range":{"relative":"15m"}`, query)
	if layout != "" {
		payload += `,"layout":` + layout
	}
	return `{"payload":` + payload + `}}`
}

// createShare creates a share as u and returns its token.
func (w *savedViewWorld) createShare(u *models.User, body string) string {
	w.env.t.Helper()
	var share models.QueryShareResponse
	if err := json.Unmarshal(w.want(u, http.MethodPost, w.sharePath(), body, http.StatusCreated), &share); err != nil {
		w.env.t.Fatal(err)
	}
	return share.Token
}

// openShare reads a share as u and returns its payload.
func (w *savedViewWorld) openShare(u *models.User, token string) models.QuerySharePayload {
	w.env.t.Helper()
	var share models.QueryShareResponse
	if err := json.Unmarshal(w.want(u, http.MethodGet, "/api/v1/query-shares/"+token, "", http.StatusOK), &share); err != nil {
		w.env.t.Fatal(err)
	}
	var payload models.QuerySharePayload
	if err := json.Unmarshal(share.Payload, &payload); err != nil {
		w.env.t.Fatal(err)
	}
	return payload
}

func TestQueryShareLayoutRoundTrips(t *testing.T) {
	w := newSavedViewWorld(t)
	w.env.s.config.Shares = sharesConfig(4096)
	token := w.createShare(w.member, sharePayload("level=error", `{"version":1,"columns":[{"name":"msg","width":320},{"name":"level"}],"sort":[{"column":"ts","desc":false}],"display_mode":"compact"}`))

	// A member of another team that reaches the source opens it.
	got := w.openShare(w.b, token)
	width, compact := 320, models.DisplayModeCompact
	want := &models.SavedViewDefinition{
		Version:     1,
		Columns:     []models.SavedViewColumn{{Name: "msg", Width: &width}, {Name: "level"}},
		Sort:        []models.SavedViewSort{{Column: "ts", Desc: false}},
		DisplayMode: &compact,
	}
	if !reflect.DeepEqual(got.Layout, want) {
		t.Fatalf("layout = %+v, want %+v", got.Layout, want)
	}
	if got.Query != "level=error" {
		t.Fatalf("query = %q", got.Query)
	}
}

func TestQueryShareRejectsInvalidLayout(t *testing.T) {
	w := newSavedViewWorld(t)
	w.env.s.config.Shares = sharesConfig(4096)
	for name, layout := range map[string]string{
		"filter":         `{"version":1,"columns":[{"name":"msg"}],"filter":"level=error"}`,
		"time range":     `{"version":1,"time_range":"1h"}`,
		"unknown key":    `{"version":1,"view_id":12}`,
		"wrong version":  `{"version":2}`,
		"narrow column":  `{"version":1,"columns":[{"name":"msg","width":10}]}`,
		"empty columns":  `{"version":1,"columns":[]}`,
		"not an object":  `"compact"`,
		"duplicate name": `{"version":1,"columns":[{"name":"msg"},{"name":"msg"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			status, data := w.as(w.member, http.MethodPost, w.sharePath(), sharePayload("level=error", layout))
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", status, data)
			}
		})
	}
}

func TestQueryShareWithoutLayoutOpens(t *testing.T) {
	w := newSavedViewWorld(t)
	w.env.s.config.Shares = sharesConfig(4096)

	created := w.createShare(w.member, sharePayload("level=error", ""))
	if got := w.openShare(w.member2, created); got.Layout != nil {
		t.Fatalf("layout = %+v, want none", got.Layout)
	}

	// A share stored before shares carried a layout.
	now := time.Now().UTC()
	old := &models.QueryShare{
		Token: "pre-layout-share-token", SourceID: w.src.ID, TeamID: &w.teamA.ID, CreatedBy: w.member.ID,
		Payload:   json.RawMessage(`{"version":1,"mode":"sql","query":"SELECT 1","limit":50,"time_range":{"relative":"1h"}}`),
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if err := w.env.db.CreateQueryShare(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if got := w.openShare(w.member2, old.Token); got.Layout != nil || got.Query != "SELECT 1" {
		t.Fatalf("payload = %+v", got)
	}
}

// The query and the layout share the payload's byte budget.
func TestQueryShareLayoutCountsTowardPayloadLimit(t *testing.T) {
	w := newSavedViewWorld(t)
	const limit = 4096
	w.env.s.config.Shares = sharesConfig(limit)
	layout := `{"version":1,"columns":[{"name":"msg","width":320},{"name":"level"}]}`

	// Leaves the payload just under the limit without the layout.
	query := strings.Repeat("a", limit-len(sharePayload("", ""))+len(`{"payload":}`)-1)
	w.createShare(w.member, sharePayload(query, ""))

	resp := w.env.do(http.MethodPost, w.sharePath(), sharePayload(query, layout), w.cookies[w.member.ID])
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", resp.status, resp.body)
	}
	if want := fmt.Sprintf("Shared query payload cannot exceed %d bytes", limit); !strings.Contains(string(resp.body), want) {
		t.Fatalf("body = %s, want the payload limit message", resp.body)
	}
}

// RFC 0007 unverified claim 5: the largest valid layout fits the default
// 1 MiB payload limit beside a small query.
func TestQueryShareLargestLayoutFitsDefaultLimit(t *testing.T) {
	w := newSavedViewWorld(t)
	w.env.s.config.Shares = sharesConfig(1024 * 1024)
	def := models.SavedViewDefinition{Version: 1}
	width := models.MaxSavedViewColumnWidth
	for i := range models.MaxSavedViewColumns {
		def.Columns = append(def.Columns, models.SavedViewColumn{Name: fmt.Sprintf("%03d", i), Width: &width})
	}
	for i := range models.MaxSavedViewSortEntries {
		def.Sort = append(def.Sort, models.SavedViewSort{Column: fmt.Sprintf("%d%s", i, strings.Repeat("s", models.MaxSavedViewColumnNameBytes-1)), Desc: true})
	}
	// Grow the column names one byte at a time until one more would break
	// the definition's byte limit.
	for i := 0; ; i = (i + 1) % len(def.Columns) {
		def.Columns[i].Name += "c"
		if def.Validate() != nil {
			def.Columns[i].Name = def.Columns[i].Name[:len(def.Columns[i].Name)-1]
			break
		}
	}
	layout, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	if len(layout) < models.MaxSavedViewDefinitionBytes-1 {
		t.Fatalf("layout is %d bytes, not near the %d byte limit", len(layout), models.MaxSavedViewDefinitionBytes)
	}
	token := w.createShare(w.member, sharePayload("level=error", string(layout)))
	if got := w.openShare(w.member, token); got.Layout == nil || len(got.Layout.Columns) != models.MaxSavedViewColumns {
		t.Fatalf("layout did not round-trip")
	}
}
