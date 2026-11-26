package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The logchef-setup skill tells an agent to create a dashboard with an exact
// body and exact headers. The body must pass the schema 2 validator that
// POST /dashboards runs, and the features header must declare every feature
// the body uses, or the agent gets back a dashboard it cannot edit.
func TestSetupSkillDashboardExample(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", ".agents", "skills", "logchef-setup", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill := string(raw)

	var req CreateDashboardRequest
	dec := json.NewDecoder(strings.NewReader(fencedBlockAfter(t, skill, "(`dashboard.json`):", "json")))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		t.Fatalf("the dashboard body does not decode as a create request: %v", err)
	}
	if req.Name == "" || len(req.Panels) == 0 {
		t.Fatalf("the dashboard body has no name or no panels: %+v", req)
	}
	if _, err := CanonicalDashboardPanels(req.Panels); err != nil {
		t.Fatalf("the dashboard body fails the schema 2 validator: %v", err)
	}

	schema := curlHeader(t, skill, DashboardSchemaHeader)
	features := curlHeader(t, skill, DashboardFeaturesHeader)
	client, err := ParseDashboardClient(&schema, features)
	if err != nil || client.Schema != DashboardPanelsVersion2 {
		t.Fatalf("%s: %q is not a schema 2 client (%v)", DashboardSchemaHeader, schema, err)
	}
	for name := range strings.SplitSeq(features, ",") {
		if !KnownDashboardFeature(strings.TrimSpace(name)) {
			t.Errorf("%s declares %q, which this server does not know", DashboardFeaturesHeader, name)
		}
	}
	used, err := DashboardFeatures(req.Panels)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range used {
		if !client.Supports(name) {
			t.Errorf("the dashboard body uses %q, which %s %q does not declare", name, DashboardFeaturesHeader, features)
		}
	}
}

// fencedBlockAfter returns the body of the first fenced block of lang that
// follows marker.
func fencedBlockAfter(t *testing.T, doc, marker, lang string) string {
	t.Helper()
	_, rest, ok := strings.Cut(doc, marker)
	if !ok {
		t.Fatalf("the skill has no %q", marker)
	}
	_, rest, ok = strings.Cut(rest, "```"+lang+"\n")
	if !ok {
		t.Fatalf("no ```%s block follows %q", lang, marker)
	}
	block, _, ok := strings.Cut(rest, "\n```")
	if !ok {
		t.Fatalf("the ```%s block after %q is not closed", lang, marker)
	}
	return block
}

// curlHeader returns the value of the one -H "name: value" in doc.
func curlHeader(t *testing.T, doc, name string) string {
	t.Helper()
	matches := regexp.MustCompile(`-H "`+regexp.QuoteMeta(name)+`: ([^"]*)"`).FindAllStringSubmatch(doc, -1)
	if len(matches) != 1 {
		t.Fatalf("the skill sends %s %d times, want once", name, len(matches))
	}
	return matches[0][1]
}
