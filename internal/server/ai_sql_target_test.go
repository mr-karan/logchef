package server

import (
	"testing"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestDeriveAITarget(t *testing.T) {
	tests := []struct {
		name       string
		sourceType models.SourceType
		mode       string
		want       ai.TargetLanguage
	}{
		{"clickhouse native", models.SourceTypeClickHouse, "native", ai.TargetClickHouseSQL},
		{"clickhouse logchefql", models.SourceTypeClickHouse, "logchefql", ai.TargetLogchefQL},
		{"victorialogs native", models.SourceTypeVictoriaLogs, "native", ai.TargetLogsQL},
		{"victorialogs logchefql", models.SourceTypeVictoriaLogs, "logchefql", ai.TargetLogchefQL},
		// Back-compat: empty/absent mode is treated as native.
		{"clickhouse empty mode defaults native", models.SourceTypeClickHouse, "", ai.TargetClickHouseSQL},
		{"victorialogs empty mode defaults native", models.SourceTypeVictoriaLogs, "", ai.TargetLogsQL},
		// Unrecognized mode is treated as native, not logchefql.
		{"clickhouse unknown mode defaults native", models.SourceTypeClickHouse, "builder", ai.TargetClickHouseSQL},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveAITarget(tc.sourceType, tc.mode)
			if got != tc.want {
				t.Fatalf("deriveAITarget(%q, %q) = %q, want %q", tc.sourceType, tc.mode, got, tc.want)
			}
		})
	}
}
