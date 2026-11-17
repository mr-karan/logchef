package datasource

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/advisor"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

// AdviseSchema reads the metadata of the source's target table, samples the
// columns whose cardinality a rule needs, and runs the rules.
func (p *ClickHouseProvider) AdviseSchema(ctx context.Context, source *models.Source, usage advisor.Usage, at time.Time) (*models.SchemaAdvisorReport, error) {
	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("get client for source %d: %w", source.ID, err)
	}
	meta, err := client.AdvisorMetadata(ctx, source.Connection.Database, source.Connection.TableName, source.MetaTSField)
	if err != nil {
		return nil, err
	}
	// A denied, limited or missing table read makes the report unavailable.
	// Any other failure, such as a refused connection, means ClickHouse could
	// not answer, and the stored report must stay.
	for _, name := range []string{clickhouse.AdvisorInputTables, clickhouse.AdvisorInputColumns} {
		if input, ok := meta.Input(name); ok && input.State == models.AdvisorInputError {
			return nil, fmt.Errorf("read %s: %s", name, input.Detail)
		}
	}
	facts := advisor.Facts{Metadata: meta, TimestampColumn: source.MetaTSField, Usage: usage, Now: at}
	if columns := advisor.SampleColumns(facts); len(columns) > 0 {
		if facts.Sample, err = client.AdvisorSample(ctx, meta, source.MetaTSField, columns, facts.Now); err != nil {
			return nil, err
		}
	}
	report := advisor.Analyze(facts)
	return &report, nil
}
