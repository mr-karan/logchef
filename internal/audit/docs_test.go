package audit

import (
	"os"
	"strings"
	"testing"
)

// The operations page lists every action, so an operator can look up any
// action that reaches the table or the log line.
func TestEveryActionIsDocumented(t *testing.T) {
	page, err := os.ReadFile("../../docs/src/content/docs/operations/audit-log.md")
	if err != nil {
		t.Fatal(err)
	}
	for action := range registry {
		if !strings.Contains(string(page), "`"+string(action)+"`") {
			t.Errorf("action %s is missing from docs/src/content/docs/operations/audit-log.md", action)
		}
	}
}
