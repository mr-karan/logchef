package clickhouse

// Schema advisor metadata: the facts about a source's table that the advisor
// rules read, each from one budgeted query that reports what it read.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/mr-karan/logchef/internal/queryescape"
	"github.com/mr-karan/logchef/pkg/models"
)

// advisorBudget bounds one advisor read. ClickHouse checks read limits per
// processed chunk, so a read can overshoot them on each thread and server.
type advisorBudget struct {
	seconds int
	rows    int64
	bytes   int64
	memory  int64
}

var (
	advisorSystemBudget   = advisorBudget{seconds: 5, rows: 1_000_000, bytes: 256 << 20, memory: 256 << 20}
	advisorPartsBudget    = advisorBudget{seconds: 5, rows: 5_000_000, bytes: 1 << 30, memory: 512 << 20}
	advisorQueryLogBudget = advisorBudget{seconds: 5, rows: 20_000_000, bytes: 4 << 30, memory: 512 << 20}
	advisorSampleBudget   = advisorBudget{seconds: 10, rows: 50_000_000, bytes: 4 << 30, memory: 1 << 30}
)

const (
	advisorMaxColumns    = 500
	advisorMaxIndexes    = 200
	advisorMaxPartitions = 2000
	advisorMaxHops       = 3

	// AdvisorMaxSampleColumns is the most columns one sample measures.
	AdvisorMaxSampleColumns = 20
	advisorSampleSlices     = 7
	advisorSliceLength      = 10 * time.Minute
	// AdvisorSliceRowLimit is the LIMIT of one sample slice. A slice that
	// returns this many rows is truncated.
	AdvisorSliceRowLimit = 150_000
	// A column is saturated only when this many slices hold at least
	// advisorSaturationRows rows each.
	advisorSaturationSlices = 5
	advisorSaturationRows   = 20_000
)

// Names of the advisor inputs.
const (
	AdvisorInputTables   = "system.tables"
	AdvisorInputColumns  = "system.columns"
	AdvisorInputIndexes  = "system.data_skipping_indices"
	AdvisorInputParts    = "system.parts"
	AdvisorInputQueryLog = "system.query_log"
	AdvisorInputSample   = "sample"
)

// Reasons a source has no advisor target.
const (
	AdvisorViewNotResolvable = "view_not_resolvable"
	AdvisorUnsupportedEngine = "unsupported_engine"
	AdvisorTargetNotReadable = "target_not_readable"
)

// AdvisorTable is one row of system.tables.
type AdvisorTable struct {
	Database     string
	Name         string
	Engine       string
	EngineFull   string
	SortingKey   string
	PrimaryKey   string
	PartitionKey string
	CreateQuery  string
	TotalRows    *uint64
	TotalBytes   *uint64
}

// AdvisorColumn is one row of system.columns.
type AdvisorColumn struct {
	Name              string
	Type              string
	DefaultKind       string
	DefaultExpression string
	Codec             string
	InSortingKey      bool
	InPartitionKey    bool
	InPrimaryKey      bool
	CompressedBytes   uint64
	UncompressedBytes uint64
}

// AdvisorIndex is one row of system.data_skipping_indices.
type AdvisorIndex struct {
	Name              string
	Type              string
	Expr              string
	Granularity       uint64
	CompressedBytes   uint64
	UncompressedBytes uint64
}

// AdvisorPartition sums the active parts of one partition. MinTime and
// MaxTime are zero when the partition key gives no time column.
type AdvisorPartition struct {
	Partition string
	Parts     uint64
	Rows      uint64
	Bytes     uint64
	MinTime   time.Time
	MaxTime   time.Time
}

// AdvisorQueryLog summarizes 7 days of finished SELECTs on the source from
// the system.query_log of the server that answered.
type AdvisorQueryLog struct {
	Queries          uint64
	MedianDurationMs float64
	P95DurationMs    float64
	MedianReadRows   float64
	MedianReadBytes  float64
}

// AdvisorPassThrough is the set of target columns that a view source exposes
// under the same name. All is true for a table, a Distributed table and
// SELECT *.
type AdvisorPassThrough struct {
	All     bool
	Columns []string
}

// Has reports whether the source exposes the target column name.
func (p AdvisorPassThrough) Has(name string) bool {
	return p.All || slices.Contains(p.Columns, name)
}

func (p AdvisorPassThrough) through(inner AdvisorPassThrough) AdvisorPassThrough {
	if p.All {
		return inner
	}
	kept := make([]string, 0, len(p.Columns))
	for _, column := range p.Columns {
		if inner.Has(column) {
			kept = append(kept, column)
		}
	}
	return AdvisorPassThrough{Columns: kept}
}

// AdvisorSampleTable is the table a sample reads: the target, except that a
// Distributed source is read through itself with leaf budgets.
type AdvisorSampleTable struct {
	Database string
	Table    string
	Leaf     bool
}

// AdvisorMetadata holds what the advisor read about a source's table. A fact
// whose input is not ok is empty, and no rule may use it.
type AdvisorMetadata struct {
	ServerVersion string
	// Unavailable is the reason no target resolved, or empty.
	Unavailable string
	Target      *models.AdvisorTarget
	Table       *AdvisorTable
	PassThrough AdvisorPassThrough
	Sample      AdvisorSampleTable

	Columns          []AdvisorColumn
	ColumnsTruncated bool
	Indexes          []AdvisorIndex
	// IndexesTruncated is true when the table has more skip indexes than
	// were read.
	IndexesTruncated bool
	Partitions       []AdvisorPartition
	// PartitionsTruncated is true when the table has more partitions than
	// were read.
	PartitionsTruncated bool
	// OldestData is the earliest time in the table's data, or nil when the
	// age of the data is unknown.
	OldestData *time.Time
	QueryLog   *AdvisorQueryLog

	Inputs []models.AdvisorInput
}

// Input returns the input of the given name.
func (m *AdvisorMetadata) Input(name string) (models.AdvisorInput, bool) {
	for _, input := range m.Inputs {
		if input.Name == name {
			return input, true
		}
	}
	return models.AdvisorInput{}, false
}

// AdvisorMetadata reads the metadata of a source's table for the schema
// advisor: it resolves the target table, then reads system.columns,
// system.data_skipping_indices, system.parts and system.query_log, each under
// its own budget. A failed read is an input state, not an error. The error
// is ctx's when ctx ends.
func (c *Client) AdvisorMetadata(ctx context.Context, database, table, tsColumn string) (*AdvisorMetadata, error) {
	meta := &AdvisorMetadata{}
	if version, err := c.conn.ServerVersion(); err == nil {
		meta.ServerVersion = fmt.Sprintf("%d.%d.%d", version.Version.Major, version.Version.Minor, version.Version.Patch)
	}

	tables := models.AdvisorInput{Name: AdvisorInputTables, State: models.AdvisorInputOK}
	lookup := func(database, table string) (*AdvisorTable, error) {
		row, read, err := c.advisorTable(ctx, database, table)
		tables.ReadRows += read.ReadRows
		tables.ReadBytes += read.ReadBytes
		return row, err
	}
	resolved, err := resolveAdvisorTarget(lookup, database, table)
	tables.State, tables.Detail = advisorInputState(err)
	meta.Inputs = append(meta.Inputs, tables)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	meta.Unavailable = resolved.reason
	if tables.State != models.AdvisorInputOK || resolved.reason != "" {
		return meta, nil
	}
	meta.Target, meta.Table, meta.PassThrough, meta.Sample = resolved.target, resolved.table, resolved.pass, resolved.sample
	target := resolved.target

	columns, input := c.advisorColumns(ctx, target.Database, target.Table)
	meta.Inputs = append(meta.Inputs, input)
	if input.State != models.AdvisorInputOK {
		return meta, ctx.Err()
	}
	if len(columns) == 0 {
		// Without a grant on the table, system.columns returns no row.
		meta.Unavailable = AdvisorTargetNotReadable
		return meta, nil
	}
	if len(columns) > advisorMaxColumns {
		columns, meta.ColumnsTruncated = columns[:advisorMaxColumns], true
	}
	meta.Columns = columns

	var indexInput, partsInput, logInput models.AdvisorInput
	meta.Indexes, indexInput = c.advisorIndexes(ctx, target.Database, target.Table)
	if len(meta.Indexes) > advisorMaxIndexes {
		meta.Indexes, meta.IndexesTruncated = meta.Indexes[:advisorMaxIndexes], true
	}
	meta.Partitions, partsInput = c.advisorPartitions(ctx, target.Database, target.Table)
	if len(meta.Partitions) > advisorMaxPartitions {
		meta.Partitions, meta.PartitionsTruncated = meta.Partitions[:advisorMaxPartitions], true
	}
	if partsInput.State == models.AdvisorInputOK {
		meta.OldestData = oldestData(meta.Columns, meta.Partitions, meta.PartitionsTruncated, tsColumn, meta.PassThrough)
	}
	meta.QueryLog, logInput = c.advisorQueryLog(ctx, database+"."+table)
	meta.Inputs = append(meta.Inputs, indexInput, partsInput, logInput)
	return meta, ctx.Err()
}

// advisorResolution is where a source's data lives, or why that is unknown.
type advisorResolution struct {
	reason string
	target *models.AdvisorTarget
	table  *AdvisorTable
	pass   AdvisorPassThrough
	sample AdvisorSampleTable
}

// resolveAdvisorTarget follows a View or Distributed source to the table
// that owns its data. A chain of views reaches a base table; a Distributed
// table reaches its local table. lookup returns nil for a table that
// system.tables does not show, as one the user holds no grant on.
func resolveAdvisorTarget(lookup func(database, table string) (*AdvisorTable, error), database, table string) (advisorResolution, error) {
	pass := AdvisorPassThrough{All: true}
	viewed := false
	visited := map[[2]string]bool{}
	for hop := 0; ; hop++ {
		key := [2]string{database, table}
		if visited[key] || hop > advisorMaxHops {
			return advisorResolution{reason: AdvisorViewNotResolvable}, nil
		}
		visited[key] = true
		row, err := lookup(database, table)
		if err != nil {
			return advisorResolution{}, err
		}
		if row == nil {
			return advisorResolution{reason: AdvisorTargetNotReadable}, nil
		}
		switch {
		case strings.HasSuffix(row.Engine, "MergeTree"):
			kind := models.AdvisorTargetTable
			if viewed {
				kind = models.AdvisorTargetViewBase
			}
			return advisorResolution{
				target: &models.AdvisorTarget{Kind: kind, Database: row.Database, Table: row.Name, Engine: row.Engine},
				table:  row,
				pass:   pass,
				sample: AdvisorSampleTable{Database: row.Database, Table: row.Name},
			}, nil
		case row.Engine == "View":
			base, through, ok := viewBase(row.CreateQuery, row.Database)
			if !ok {
				return advisorResolution{reason: AdvisorViewNotResolvable}, nil
			}
			viewed, pass = true, pass.through(through)
			database, table = base[0], base[1]
		case row.Engine == "Distributed" && !viewed && hop == 0:
			return resolveDistributed(lookup, row)
		default:
			return advisorResolution{reason: AdvisorUnsupportedEngine}, nil
		}
	}
}

// resolveDistributed reads the local table of a Distributed source. Any
// ALTER on it needs ON CLUSTER, and the sample reads the source.
func resolveDistributed(lookup func(database, table string) (*AdvisorTable, error), source *AdvisorTable) (advisorResolution, error) {
	params := parseEngineParams(source.EngineFull)
	if len(params) < 3 || strings.ContainsAny(params[1]+params[2], "()") {
		return advisorResolution{reason: AdvisorUnsupportedEngine}, nil
	}
	local, err := lookup(params[1], params[2])
	if err != nil {
		return advisorResolution{}, err
	}
	if local == nil {
		return advisorResolution{reason: AdvisorTargetNotReadable}, nil
	}
	if !strings.HasSuffix(local.Engine, "MergeTree") {
		return advisorResolution{reason: AdvisorUnsupportedEngine}, nil
	}
	return advisorResolution{
		target: &models.AdvisorTarget{
			Kind: models.AdvisorTargetDistributedLocal, Database: local.Database, Table: local.Name,
			Cluster: params[0], Engine: local.Engine,
		},
		table:  local,
		pass:   AdvisorPassThrough{All: true},
		sample: AdvisorSampleTable{Database: source.Database, Table: source.Name, Leaf: true},
	}, nil
}

// viewBase returns the one table a view reads and the columns it passes
// through under the same name. A view with a JOIN, a set operation, a
// subquery or a table function in FROM has no base.
func viewBase(createQuery, viewDatabase string) (base [2]string, pass AdvisorPassThrough, ok bool) {
	stmts, err := clickhouseparser.NewParser(createQuery).ParseStmts()
	if err != nil || len(stmts) != 1 {
		return base, pass, false
	}
	view, isView := stmts[0].(*clickhouseparser.CreateView)
	if !isView || view.SubQuery == nil || view.SubQuery.Select == nil {
		return base, pass, false
	}
	query := view.SubQuery.Select
	table, ok := singleTable(query)
	if !ok {
		return base, pass, false
	}
	name, nameOK := IdentName(table.Table)
	database := viewDatabase
	if table.Database != nil {
		var dbOK bool
		if database, dbOK = IdentName(table.Database); !dbOK {
			return base, pass, false
		}
	}
	if !nameOK || table.Database == nil && withNames(query.With, name) {
		return base, pass, false
	}
	return [2]string{database, name}, viewPassThrough(query.SelectItems), true
}

// singleTable returns the one table that a SELECT reads, with no JOIN, set
// operation, subquery or table function.
func singleTable(query *clickhouseparser.SelectQuery) (*clickhouseparser.TableIdentifier, bool) {
	if query.InnerQuery != nil || query.UnionAll != nil || query.UnionDistinct != nil || query.Except != nil || query.Intersect != nil || query.From == nil {
		return nil, false
	}
	from, isTable := query.From.Expr.(*clickhouseparser.JoinTableExpr)
	if !isTable || from.Table == nil {
		return nil, false
	}
	tableExpr := from.Table.Expr
	if aliased, isAlias := tableExpr.(*clickhouseparser.AliasExpr); isAlias {
		tableExpr = aliased.Expr
	}
	table, isIdent := tableExpr.(*clickhouseparser.TableIdentifier)
	return table, isIdent && table.Table != nil
}

// withNames reports a WITH name equal to name, which a bare FROM name reads
// instead of the table.
func withNames(with *clickhouseparser.WithClause, name string) bool {
	if with == nil {
		return false
	}
	for _, cte := range with.CTEs {
		for _, expr := range []clickhouseparser.Expr{cte.Expr, cte.Alias} {
			if ident, ok := expr.(*clickhouseparser.Ident); ok && identIs(ident, name) {
				return true
			}
		}
	}
	return false
}

// viewPassThrough lists the select items that expose a column under its own
// name: *, or a bare identifier with no alias or an alias of the same name.
func viewPassThrough(items []*clickhouseparser.SelectItem) AdvisorPassThrough {
	pass := AdvisorPassThrough{}
	for _, item := range items {
		ident, isIdent := item.Expr.(*clickhouseparser.Ident)
		if !isIdent {
			continue
		}
		if ident.Name == "*" && ident.QuoteType == 0 {
			if item.Alias == nil && len(item.Modifiers) == 0 {
				return AdvisorPassThrough{All: true}
			}
			continue
		}
		name, ok := IdentName(ident)
		if !ok {
			continue
		}
		if item.Alias != nil {
			if alias, aliasOK := IdentName(item.Alias); !aliasOK || alias != name {
				continue
			}
		}
		pass.Columns = append(pass.Columns, name)
	}
	return pass
}

// oldestData is the earliest time in the table, known only when the
// partition key holds the source's timestamp column and every partition read
// gives a time. Part modification times measure merges and are not used.
func oldestData(columns []AdvisorColumn, partitions []AdvisorPartition, truncated bool, tsColumn string, pass AdvisorPassThrough) *time.Time {
	if truncated || len(partitions) == 0 || !pass.Has(tsColumn) {
		return nil
	}
	inKey := false
	for _, column := range columns {
		if column.Name == tsColumn {
			inKey = column.InPartitionKey
		}
	}
	if !inKey {
		return nil
	}
	var oldest time.Time
	for _, partition := range partitions {
		if partition.MinTime.Unix() <= 0 {
			return nil
		}
		if oldest.IsZero() || partition.MinTime.Before(oldest) {
			oldest = partition.MinTime
		}
	}
	return &oldest
}

func (c *Client) advisorTable(ctx context.Context, database, table string) (*AdvisorTable, advisorRead, error) {
	var row *AdvisorTable
	read, err := c.queryBudgeted(ctx, advisorSystemBudget, false, func(rows driver.Rows) error {
		if !rows.Next() {
			return rows.Err()
		}
		row = &AdvisorTable{}
		if err := rows.Scan(&row.Database, &row.Name, &row.Engine, &row.EngineFull, &row.SortingKey, &row.PrimaryKey,
			&row.PartitionKey, &row.CreateQuery, &row.TotalRows, &row.TotalBytes); err != nil {
			return fmt.Errorf("scan system.tables: %w", err)
		}
		return rows.Err()
	}, `SELECT database, name, engine, engine_full, sorting_key, primary_key, partition_key, create_table_query, total_rows, total_bytes
		FROM system.tables WHERE database = ? AND name = ?`, database, table)
	return row, read, err
}

func (c *Client) advisorColumns(ctx context.Context, database, table string) ([]AdvisorColumn, models.AdvisorInput) {
	var columns []AdvisorColumn
	read, err := c.queryBudgeted(ctx, advisorSystemBudget, false, func(rows driver.Rows) error {
		for rows.Next() {
			var column AdvisorColumn
			var sorting, partition, primary uint8
			if err := rows.Scan(&column.Name, &column.Type, &column.DefaultKind, &column.DefaultExpression, &column.Codec,
				&sorting, &partition, &primary, &column.CompressedBytes, &column.UncompressedBytes); err != nil {
				return fmt.Errorf("scan system.columns: %w", err)
			}
			column.InSortingKey, column.InPartitionKey, column.InPrimaryKey = sorting == 1, partition == 1, primary == 1
			columns = append(columns, column)
		}
		return rows.Err()
	}, fmt.Sprintf(`SELECT name, type, default_kind, default_expression, compression_codec,
			is_in_sorting_key, is_in_partition_key, is_in_primary_key, data_compressed_bytes, data_uncompressed_bytes
		FROM system.columns WHERE database = ? AND table = ? ORDER BY position LIMIT %d`, advisorMaxColumns+1), database, table)
	return columns, read.withState(AdvisorInputColumns, err)
}

func (c *Client) advisorIndexes(ctx context.Context, database, table string) ([]AdvisorIndex, models.AdvisorInput) {
	var indexes []AdvisorIndex
	read, err := c.queryBudgeted(ctx, advisorSystemBudget, false, func(rows driver.Rows) error {
		for rows.Next() {
			var index AdvisorIndex
			if err := rows.Scan(&index.Name, &index.Type, &index.Expr, &index.Granularity, &index.CompressedBytes, &index.UncompressedBytes); err != nil {
				return fmt.Errorf("scan system.data_skipping_indices: %w", err)
			}
			indexes = append(indexes, index)
		}
		return rows.Err()
	}, fmt.Sprintf(`SELECT name, type_full, expr, granularity, data_compressed_bytes, data_uncompressed_bytes
		FROM system.data_skipping_indices WHERE database = ? AND table = ? ORDER BY name LIMIT %d`, advisorMaxIndexes+1), database, table)
	input := read.withState(AdvisorInputIndexes, err)
	if input.State != models.AdvisorInputOK {
		return nil, input
	}
	return indexes, input
}

func (c *Client) advisorPartitions(ctx context.Context, database, table string) ([]AdvisorPartition, models.AdvisorInput) {
	var partitions []AdvisorPartition
	read, err := c.queryBudgeted(ctx, advisorPartsBudget, false, func(rows driver.Rows) error {
		for rows.Next() {
			var partition AdvisorPartition
			if err := rows.Scan(&partition.Partition, &partition.Parts, &partition.Rows, &partition.Bytes, &partition.MinTime, &partition.MaxTime); err != nil {
				return fmt.Errorf("scan system.parts: %w", err)
			}
			partitions = append(partitions, partition)
		}
		return rows.Err()
	}, fmt.Sprintf(`SELECT partition, count() AS parts, sum(rows), sum(bytes_on_disk), min(min_time), max(max_time)
		FROM system.parts WHERE database = ? AND table = ? AND active
		GROUP BY partition ORDER BY partition LIMIT %d`, advisorMaxPartitions+1), database, table)
	input := read.withState(AdvisorInputParts, err)
	if input.State != models.AdvisorInputOK {
		return nil, input
	}
	return partitions, input
}

// advisorQueryLog reads only the server that answers: system.query_log is
// local to it.
func (c *Client) advisorQueryLog(ctx context.Context, qualifiedTable string) (*AdvisorQueryLog, models.AdvisorInput) {
	var summary AdvisorQueryLog
	read, err := c.queryBudgeted(ctx, advisorQueryLogBudget, false, func(rows driver.Rows) error {
		if rows.Next() {
			if err := rows.Scan(&summary.Queries, &summary.MedianDurationMs, &summary.P95DurationMs, &summary.MedianReadRows, &summary.MedianReadBytes); err != nil {
				return fmt.Errorf("scan system.query_log: %w", err)
			}
		}
		return rows.Err()
	}, `SELECT count(), quantile(0.5)(query_duration_ms), quantile(0.95)(query_duration_ms), quantile(0.5)(read_rows), quantile(0.5)(read_bytes)
		FROM system.query_log
		WHERE event_date >= today() - 7 AND type = 'QueryFinish' AND query_kind = 'Select' AND has(tables, ?)`, qualifiedTable)
	input := read.withState(AdvisorInputQueryLog, err)
	if input.State != models.AdvisorInputOK {
		return nil, input
	}
	for _, value := range []*float64{&summary.MedianDurationMs, &summary.P95DurationMs, &summary.MedianReadRows, &summary.MedianReadBytes} {
		if math.IsNaN(*value) {
			*value = 0
		}
	}
	return &summary, input
}

// AdvisorSample holds distinct counts of candidate columns over 7 slices of
// the last 7 days, one per day at a different hour, each 10 minutes long.
type AdvisorSample struct {
	Slices  []AdvisorSlice
	Columns []AdvisorCardinality
	Input   models.AdvisorInput
}

// AdvisorSlice is one slice of the sample. Distinct follows the sampled
// columns in order.
type AdvisorSlice struct {
	Start     time.Time
	Rows      uint64
	Truncated bool
	Distinct  []uint64
}

// AdvisorCardinality is the sampled cardinality of one column. Distinct is
// a lower bound when a slice was truncated. Saturated means the sample saw
// every value the slices hold, so Distinct also bounds them from above.
type AdvisorCardinality struct {
	Column           string
	Distinct         uint64
	MaxSliceDistinct uint64
	Saturated        bool
}

// AdvisorSample measures the distinct counts of columns. It runs only when
// the timestamp column is in the target's sorting or partition key, so each
// slice's time filter prunes; otherwise the input is skipped. A failed read
// is an input state; the error is ctx's when ctx ends.
func (c *Client) AdvisorSample(ctx context.Context, meta *AdvisorMetadata, tsColumn string, columns []string, now time.Time) (*AdvisorSample, error) {
	if len(columns) == 0 || len(columns) > AdvisorMaxSampleColumns {
		return nil, fmt.Errorf("a sample measures 1 to %d columns, got %d", AdvisorMaxSampleColumns, len(columns))
	}
	sample := &AdvisorSample{Input: models.AdvisorInput{Name: AdvisorInputSample, State: models.AdvisorInputSkipped}}
	if !sampleable(meta, tsColumn) {
		sample.Input.Detail = "the timestamp column is in neither the sorting key nor the partition key"
		return sample, nil
	}
	starts := sliceStarts(now)
	query := sampleQuery(meta.Sample, tsColumn, columns, starts)
	results := map[uint8][]uint64{}
	read, err := c.queryBudgeted(ctx, advisorSampleBudget, meta.Sample.Leaf, func(rows driver.Rows) error {
		for rows.Next() {
			var slice uint8
			values := make([]uint64, len(columns)+1)
			dests := []any{&slice}
			for i := range values {
				dests = append(dests, &values[i])
			}
			if err := rows.Scan(dests...); err != nil {
				return fmt.Errorf("scan sample: %w", err)
			}
			results[slice] = values
		}
		return rows.Err()
	}, query)
	sample.Input = read.withState(AdvisorInputSample, err)
	if sample.Input.State != models.AdvisorInputOK {
		return sample, ctx.Err()
	}
	sample.Slices, sample.Columns = summarizeSample(columns, starts, results)
	return sample, nil
}

func sampleable(meta *AdvisorMetadata, tsColumn string) bool {
	if meta == nil || meta.Target == nil || !meta.PassThrough.Has(tsColumn) {
		return false
	}
	for _, column := range meta.Columns {
		if column.Name == tsColumn {
			return column.InSortingKey || column.InPartitionKey
		}
	}
	return false
}

// sliceStarts places one slice on each of the 7 days before now's UTC date,
// each at a different hour.
func sliceStarts(now time.Time) []time.Time {
	today := now.UTC().Truncate(24 * time.Hour)
	starts := make([]time.Time, advisorSampleSlices)
	for i := range starts {
		day := today.AddDate(0, 0, -(i + 1))
		starts[i] = day.Add(time.Duration((i+1)*3) * time.Hour)
	}
	return starts
}

// sampleQuery numbers slices from 1: WITH ROLLUP writes the total row with
// slice 0. ClickHouse aliases are visible across the whole query, so every
// name it adds has a prefix no source column is expected to use: an alias
// named like a sampled column would replace it.
func sampleQuery(table AdvisorSampleTable, tsColumn string, columns []string, starts []time.Time) string {
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = fmt.Sprintf("%s AS __logchef_c%d", queryescape.ClickHouseIdentifier(column), i)
	}
	ts := queryescape.ClickHouseIdentifier(tsColumn)
	from := queryescape.ClickHouseIdentifier(table.Database) + "." + queryescape.ClickHouseIdentifier(table.Table)
	branches := make([]string, len(starts))
	for i, start := range starts {
		begin := fmt.Sprintf("toDateTime('%s', 'UTC')", start.UTC().Format(time.DateTime))
		branches[i] = fmt.Sprintf("SELECT %d AS __logchef_slice, %s FROM %s WHERE %s >= %s AND %s < %s + INTERVAL %d MINUTE LIMIT %d",
			i+1, strings.Join(quoted, ", "), from, ts, begin, ts, begin, int(advisorSliceLength.Minutes()), AdvisorSliceRowLimit)
	}
	aggregates := make([]string, len(columns))
	for i := range columns {
		aggregates[i] = fmt.Sprintf("uniqCombined(__logchef_c%d) AS __logchef_u%d", i, i)
	}
	return fmt.Sprintf("SELECT __logchef_slice, count() AS __logchef_rows, %s FROM (%s) GROUP BY __logchef_slice WITH ROLLUP",
		strings.Join(aggregates, ", "), strings.Join(branches, " UNION ALL "))
}

// summarizeSample turns the rows of sampleQuery into slices and per-column
// cardinality. results maps a slice number to its row count followed by one
// distinct count per column; slice 0 is the total.
func summarizeSample(columns []string, starts []time.Time, results map[uint8][]uint64) (sliceStats []AdvisorSlice, cardinality []AdvisorCardinality) {
	sliceStats = make([]AdvisorSlice, len(starts))
	truncated, full := false, 0
	for i, start := range starts {
		sliceStats[i] = AdvisorSlice{Start: start, Distinct: make([]uint64, len(columns))}
		if values, ok := results[uint8(i+1)]; ok {
			sliceStats[i].Rows = values[0]
			copy(sliceStats[i].Distinct, values[1:])
		}
		sliceStats[i].Truncated = sliceStats[i].Rows >= AdvisorSliceRowLimit
		truncated = truncated || sliceStats[i].Truncated
		if sliceStats[i].Rows >= advisorSaturationRows {
			full++
		}
	}
	total := results[0]
	cardinality = make([]AdvisorCardinality, len(columns))
	for i, column := range columns {
		card := AdvisorCardinality{Column: column}
		if total != nil {
			card.Distinct = total[i+1]
		}
		for _, slice := range sliceStats {
			card.MaxSliceDistinct = max(card.MaxSliceDistinct, slice.Distinct[i])
		}
		card.Saturated = !truncated && full >= advisorSaturationSlices &&
			float64(card.Distinct) <= 1.5*float64(card.MaxSliceDistinct)
		cardinality[i] = card
	}
	return sliceStats, cardinality
}

// advisorRead is the work ClickHouse reported for one budgeted query.
type advisorRead struct {
	ReadRows  uint64
	ReadBytes uint64
}

func (r advisorRead) withState(name string, err error) models.AdvisorInput {
	state, detail := advisorInputState(err)
	return models.AdvisorInput{Name: name, State: state, Detail: detail, ReadRows: r.ReadRows, ReadBytes: r.ReadBytes}
}

// advisorInputState maps the error of one advisor read to its state.
func advisorInputState(err error) (state models.AdvisorInputState, detail string) {
	switch {
	case err == nil:
		return models.AdvisorInputOK, ""
	case IsAccessDeniedError(err):
		return models.AdvisorInputDenied, "the ClickHouse user lacks the grant for this read"
	case hasExceptionCode(err, chExceptionUnknownTable):
		return models.AdvisorInputMissing, "the table does not exist on the server that answered"
	case IsReadLimitExceededError(err), IsQueryTimeoutError(err), hasExceptionCode(err, chExceptionMemoryLimit):
		return models.AdvisorInputLimited, "the read stopped at its budget"
	}
	detail = err.Error()
	if queryErr, ok := errors.AsType[*QueryError](err); ok {
		detail = queryErr.Message
	}
	return models.AdvisorInputError, limitMessage(detail)
}

// queryBudgeted runs one advisor read under budget and returns the rows and
// bytes ClickHouse reported reading. The budget goes to the server as
// settings, capped by the source's own; leaf sets the same caps on each
// shard of a Distributed read.
func (c *Client) queryBudgeted(ctx context.Context, budget advisorBudget, leaf bool, consume func(driver.Rows) error, query string, args ...any) (advisorRead, error) {
	progress := &queryProgress{}
	settings, err := c.budgetSettings(ctx, budget, leaf)
	if err != nil {
		return advisorRead{}, err
	}
	// The driver would replace max_execution_time with the run's deadline
	// plus five seconds; withQueryTimeout hides it and still cancels.
	queryCtx, cancel := withQueryTimeout(clickhouse.Context(ctx, clickhouse.WithProgress(progress.add)), budget.seconds)
	defer cancel()
	err = c.executeQueryWithHooks(queryCtx, query, func(hookCtx context.Context) (err error) {
		rows, err := c.queryOwnWithSettings(hookCtx, budget.seconds, settings, query, args...)
		if err != nil {
			return err
		}
		defer func() {
			if err != nil {
				cancel()
			}
			if closeErr := rows.Close(); err == nil {
				err = closeErr
			}
		}()
		return consume(rows)
	})
	progress.mu.Lock()
	read := advisorRead{ReadRows: progress.rows, ReadBytes: progress.bytes}
	progress.mu.Unlock()
	return read, err
}

func (c *Client) budgetSettings(ctx context.Context, budget advisorBudget, leaf bool) (clickhouse.Settings, error) {
	settings := clickhouse.Settings{
		"max_rows_to_read":  lowestLimit(budget.rows, settingInt(c.querySettings["max_rows_to_read"])),
		"max_bytes_to_read": lowestLimit(budget.bytes, settingInt(c.querySettings["max_bytes_to_read"])),
		"max_memory_usage":  lowestLimit(budget.memory, settingInt(c.querySettings["max_memory_usage"])),
		"max_threads":       2,
	}
	if !leaf {
		return settings, nil
	}
	settings["max_rows_to_read_leaf"] = lowestLimit(budget.rows, settingInt(c.querySettings["max_rows_to_read_leaf"]))
	settings["max_bytes_to_read_leaf"] = lowestLimit(budget.bytes, settingInt(c.querySettings["max_bytes_to_read_leaf"]))
	modes, err := c.throwModes.get(ctx)
	if err != nil {
		return nil, err
	}
	// A positive leaf timeout replaces max_execution_time on each shard, so a
	// larger one from the source must not stand. Servers before 23.11 know
	// neither it nor its overflow mode.
	if _, known := modes["timeout_overflow_mode_leaf"]; known {
		settings["max_execution_time_leaf"] = lowestLimit(int64(budget.seconds), settingInt(c.querySettings["max_execution_time_leaf"]))
	}
	return settings, nil
}
