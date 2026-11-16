package advisor

import (
	"fmt"
	"regexp"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/queryescape"
	"github.com/mr-karan/logchef/pkg/models"
)

const ruleCodec = "codec"

// dateTimeType matches DateTime and DateTime64, with or without a precision
// and a time zone.
var dateTimeType = regexp.MustCompile(`^DateTime(64)?(\(.*\))?$`)

// codecFindings is R5: an explicit codec for a large column that uses the
// server's default. Trigger A is the source's timestamp column, trigger B a
// String column that compresses poorly.
func codecFindings(a *analysis) []finding {
	var out []finding
	for _, column := range a.meta.Columns {
		if column.Codec != "" {
			continue
		}
		if _, ok := a.column(column.Name); !ok {
			continue
		}
		share, _ := a.tableShare(column)
		var codec, title, cost string
		switch {
		case column.Name == a.ts && dateTimeType.MatchString(column.Type) && column.CompressedBytes >= codecTimestampMinBytes:
			codec = "CODEC(Delta, ZSTD(1))"
			title = fmt.Sprintf("Compress the timestamp column %s with Delta and ZSTD", column.Name)
		case column.Type == "String" && column.CompressedBytes >= codecStringMinBytes && share >= codecStringMinShare &&
			float64(column.UncompressedBytes) < codecStringMaxRatio*float64(column.CompressedBytes):
			codec = "CODEC(ZSTD(1))"
			title = fmt.Sprintf("Compress %s with ZSTD", column.Name)
			cost = " This gains nothing where the server default is already ZSTD, as in ClickHouse Cloud, and Logchef cannot tell the two apart."
		default:
			continue
		}
		evidence := a.sizeEvidence(column)
		evidence = append(evidence,
			models.AdvisorEvidence{Label: "Uncompressed to compressed ratio", Value: fmt.Sprintf("%.1f", float64(column.UncompressedBytes)/float64(column.CompressedBytes)), Source: clickhouse.AdvisorInputColumns},
			models.AdvisorEvidence{Label: "Current codec", Value: "none set, so the server default applies", Source: clickhouse.AdvisorInputColumns},
		)
		out = append(out, finding{share: share, SchemaFinding: models.SchemaFinding{
			ID:       a.findingID(ruleCodec, column.Name, nil, ""),
			Rule:     ruleCodec,
			Impact:   impactFor(share),
			Title:    title,
			Column:   column.Name,
			Evidence: evidence,
			DDL:      []string{fmt.Sprintf("%s MODIFY COLUMN %s %s;", a.alterTable(), queryescape.ClickHouseIdentifier(column.Name), codec)},
			Gain:     "A smaller column on disk.",
			Cost: "More CPU on insert and merge. The ALTER changes metadata only: existing parts keep the old codec until a merge rewrites them." +
				cost,
		}})
	}
	return out
}
