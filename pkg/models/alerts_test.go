package models

import "testing"

func TestResolveAlertMetadataAllowsConditionModeForLogsQL(t *testing.T) {
	language, mode, err := ResolveAlertMetadata(QueryLanguageLogsQL, AlertEditorModeCondition)
	if err != nil {
		t.Fatalf("ResolveAlertMetadata returned error: %v", err)
	}
	if language != QueryLanguageLogsQL {
		t.Fatalf("unexpected language: %q", language)
	}
	if mode != AlertEditorModeCondition {
		t.Fatalf("unexpected mode: %q", mode)
	}
}
