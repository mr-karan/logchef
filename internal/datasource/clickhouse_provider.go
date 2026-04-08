package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

type ClickHouseProvider struct {
	manager *clickhouse.Manager
	log     *slog.Logger
}

func NewClickHouseProvider(manager *clickhouse.Manager, log *slog.Logger) *ClickHouseProvider {
	return &ClickHouseProvider{
		manager: manager,
		log:     log.With("component", "clickhouse_provider"),
	}
}

func (p *ClickHouseProvider) Type() models.SourceType {
	return models.SourceTypeClickHouse
}

func (p *ClickHouseProvider) PrepareSource(ctx context.Context, req *models.CreateSourceRequest) (*models.Source, error) {
	if req == nil {
		return nil, fmt.Errorf("create source request is required")
	}

	if err := ValidateCommonSourceFields(req.Name, req.Description, req.TTLDays); err != nil {
		return nil, err
	}

	conn, err := p.connectionFromConfig(req.Connection)
	if err != nil {
		return nil, err
	}
	if err := validateClickHouseConnection("connection.", true, conn.Host, conn.Username, conn.Password, conn.Database, conn.TableName); err != nil {
		return nil, err
	}

	metaTSField := strings.TrimSpace(req.MetaTSField)
	if metaTSField == "" {
		metaTSField = "timestamp"
	}
	if metaTSField == "" {
		return nil, &ValidationError{Field: "meta_ts_field", Message: "meta timestamp field is required"}
	}
	if !IsValidIdentifier(metaTSField) {
		return nil, &ValidationError{Field: "meta_ts_field", Message: "meta timestamp field contains invalid characters"}
	}

	metaSeverityField := strings.TrimSpace(req.MetaSeverityField)
	if metaSeverityField != "" && !IsValidIdentifier(metaSeverityField) {
		return nil, &ValidationError{Field: "meta_severity_field", Message: "meta severity field contains invalid characters"}
	}

	source := &models.Source{
		Name:              req.Name,
		MetaIsAutoCreated: req.MetaIsAutoCreated,
		SourceType:        models.SourceTypeClickHouse,
		MetaTSField:       metaTSField,
		MetaSeverityField: metaSeverityField,
		Connection:        conn,
		Description:       req.Description,
		TTLDays:           req.TTLDays,
		Timestamps: models.Timestamps{
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	if err := source.SyncConnectionConfig(); err != nil {
		return nil, err
	}

	client, err := p.manager.CreateTemporaryClient(ctx, source)
	if err != nil {
		return nil, &ValidationError{Field: "connection", Message: "Failed to connect to the database", Err: err}
	}
	defer client.Close()

	if req.MetaIsAutoCreated {
		schemaToExecute := req.Schema
		if schemaToExecute == "" {
			schemaToExecute = models.OTELLogsTableSchema
			schemaToExecute = strings.ReplaceAll(schemaToExecute, "{{database_name}}", conn.Database)
			schemaToExecute = strings.ReplaceAll(schemaToExecute, "{{table_name}}", conn.TableName)
			if req.TTLDays >= 0 {
				schemaToExecute = strings.ReplaceAll(schemaToExecute, "{{ttl_day}}", strconv.Itoa(req.TTLDays))
			} else {
				schemaToExecute = strings.ReplaceAll(schemaToExecute, " TTL toDateTime(timestamp) + INTERVAL {{ttl_day}} DAY", "")
			}
		}

		if _, err := client.Query(ctx, schemaToExecute); err != nil {
			return nil, &ValidationError{Field: "connection.table_name", Message: "Failed to create table in ClickHouse", Err: err}
		}
	} else {
		if err := client.Ping(ctx, conn.Database, conn.TableName); err != nil {
			return nil, &ValidationError{Field: "connection.table_name", Message: fmt.Sprintf("Table '%s.%s' not found", conn.Database, conn.TableName), Err: err}
		}
		if err := p.validateColumnTypes(ctx, client, conn.Database, conn.TableName, metaTSField, metaSeverityField); err != nil {
			return nil, err
		}
	}

	return source, nil
}

func (p *ClickHouseProvider) ValidateConnection(ctx context.Context, req *models.ValidateConnectionRequest) (*models.ConnectionValidationResult, error) {
	if req == nil {
		return nil, fmt.Errorf("validate connection request is required")
	}

	conn, err := p.connectionFromConfig(req.Connection)
	if err != nil {
		return nil, err
	}
	if err := validateClickHouseConnection("", false, conn.Host, conn.Username, conn.Password, conn.Database, conn.TableName); err != nil {
		return nil, err
	}

	tempSource := &models.Source{SourceType: models.SourceTypeClickHouse, Connection: conn}
	client, err := p.manager.CreateTemporaryClient(ctx, tempSource)
	if err != nil {
		return nil, &ValidationError{Field: "connection", Message: "Failed to connect to the database", Err: err}
	}
	defer client.Close()

	if strings.TrimSpace(req.TimestampField) != "" {
		if strings.TrimSpace(conn.TableName) == "" {
			return nil, &ValidationError{Field: "table_name", Message: "table name is required to validate columns"}
		}
		if !IsValidIdentifier(strings.TrimSpace(req.TimestampField)) {
			return nil, &ValidationError{Field: "timestamp_field", Message: "timestamp field contains invalid characters"}
		}
		if strings.TrimSpace(req.SeverityField) != "" && !IsValidIdentifier(strings.TrimSpace(req.SeverityField)) {
			return nil, &ValidationError{Field: "severity_field", Message: "severity field contains invalid characters"}
		}
		if err := client.Ping(ctx, conn.Database, conn.TableName); err != nil {
			return nil, &ValidationError{Field: "table_name", Message: fmt.Sprintf("Connection successful, but table '%s.%s' not found or inaccessible", conn.Database, conn.TableName), Err: err}
		}
		if err := p.validateColumnTypes(ctx, client, conn.Database, conn.TableName, strings.TrimSpace(req.TimestampField), strings.TrimSpace(req.SeverityField)); err != nil {
			return nil, err
		}
		return &models.ConnectionValidationResult{Message: "Connection and column types validated successfully"}, nil
	}

	if strings.TrimSpace(conn.TableName) != "" {
		if err := client.Ping(ctx, conn.Database, conn.TableName); err != nil {
			return nil, &ValidationError{Field: "table_name", Message: fmt.Sprintf("Connection successful, but table '%s.%s' not found or inaccessible", conn.Database, conn.TableName), Err: err}
		}
	}

	return &models.ConnectionValidationResult{Message: "Connection successful"}, nil
}

func (p *ClickHouseProvider) QueryLogs(ctx context.Context, source *models.Source, req QueryRequest) (*models.QueryResult, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}

	if req.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}

	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting database connection for source %d: %w", source.ID, err)
	}

	qb := clickhouse.NewExtendedQueryBuilder(source.GetFullTableName(), req.MaxLimit)
	builtQuery, err := qb.BuildRawQuery(req.RawQuery, req.Limit)
	if err != nil {
		return nil, fmt.Errorf("invalid query syntax: %w", err)
	}

	return client.QueryWithTimeout(ctx, builtQuery, req.QueryTimeout)
}

func (p *ClickHouseProvider) GetSourceSchema(ctx context.Context, source *models.Source) ([]models.ColumnInfo, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}

	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting database connection for source %d: %w", source.ID, err)
	}

	tableInfo, err := client.GetTableInfo(ctx, source.Connection.Database, source.Connection.TableName)
	if err != nil {
		return nil, fmt.Errorf("error retrieving schema for source %d: %w", source.ID, err)
	}

	return tableInfo.Columns, nil
}

func (p *ClickHouseProvider) Histogram(ctx context.Context, source *models.Source, req HistogramRequest) (*HistogramResult, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}
	if source.MetaTSField == "" {
		return nil, fmt.Errorf("source %d does not have a timestamp field configured", source.ID)
	}
	if req.Query == "" {
		return nil, fmt.Errorf("query parameter is required for histogram data")
	}
	if req.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}

	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting database connection for source %d: %w", source.ID, err)
	}

	window, err := parseTimeWindow(req.Window)
	if err != nil {
		return nil, err
	}

	result, err := client.GetHistogramData(ctx, source.GetFullTableName(), source.MetaTSField, clickhouse.HistogramParams{
		Window:       window,
		Query:        req.Query,
		GroupBy:      req.GroupBy,
		Timezone:     req.Timezone,
		QueryTimeout: req.QueryTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("error generating histogram for source %d: %w", source.ID, err)
	}

	data := make([]HistogramBucket, 0, len(result.Data))
	for _, row := range result.Data {
		data = append(data, HistogramBucket{
			Bucket:     row.Bucket,
			LogCount:   row.LogCount,
			GroupValue: row.GroupValue,
		})
	}

	return &HistogramResult{
		Granularity: result.Granularity,
		Data:        data,
	}, nil
}

func (p *ClickHouseProvider) LogContext(ctx context.Context, source *models.Source, req LogContextRequest) (*LogContextResult, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}
	if source.MetaTSField == "" {
		return nil, fmt.Errorf("source %d does not have a timestamp field configured", source.ID)
	}
	if req.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}

	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting database connection for source %d: %w", source.ID, err)
	}

	result, err := client.GetSurroundingLogs(
		ctx,
		source.GetFullTableName(),
		source.MetaTSField,
		clickhouse.LogContextParams{
			TargetTime:      time.UnixMilli(req.TargetTimestamp),
			BeforeLimit:     req.BeforeLimit,
			AfterLimit:      req.AfterLimit,
			BeforeOffset:    req.BeforeOffset,
			AfterOffset:     req.AfterOffset,
			ExcludeBoundary: req.ExcludeBoundary,
		},
		req.QueryTimeout,
	)
	if err != nil {
		return nil, fmt.Errorf("error retrieving log context for source %d: %w", source.ID, err)
	}

	return &LogContextResult{
		TargetTimestamp: req.TargetTimestamp,
		BeforeLogs:      result.BeforeLogs,
		TargetLogs:      result.TargetLogs,
		AfterLogs:       result.AfterLogs,
		Stats:           result.Stats,
	}, nil
}

func (p *ClickHouseProvider) EvaluateAlert(ctx context.Context, source *models.Source, req AlertQueryRequest) (*models.QueryResult, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}
	if req.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}

	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting database connection for source %d: %w", source.ID, err)
	}

	return client.QueryWithTimeout(ctx, req.Query, req.QueryTimeout)
}

func (p *ClickHouseProvider) InitializeSource(ctx context.Context, source *models.Source) error {
	return p.manager.AddSource(ctx, source)
}

func (p *ClickHouseProvider) RemoveSource(sourceID models.SourceID) error {
	return p.manager.RemoveSource(sourceID)
}

func (p *ClickHouseProvider) CheckSourceConnectionStatus(ctx context.Context, source *models.Source) bool {
	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		return false
	}
	return client.Ping(ctx, source.Connection.Database, source.Connection.TableName) == nil
}

func (p *ClickHouseProvider) GetSourceHealth(ctx context.Context, sourceID models.SourceID) models.SourceHealth {
	return p.manager.GetHealth(ctx, sourceID)
}

func (p *ClickHouseProvider) connectionFromConfig(raw json.RawMessage) (models.ConnectionInfo, error) {
	var conn models.ConnectionInfo
	if len(raw) == 0 {
		return conn, &ValidationError{Field: "connection", Message: "connection is required"}
	}
	if err := json.Unmarshal(raw, &conn); err != nil {
		return conn, &ValidationError{Field: "connection", Message: "invalid clickhouse connection payload", Err: err}
	}
	return conn, nil
}

func (p *ClickHouseProvider) validateColumnTypes(ctx context.Context, client *clickhouse.Client, database, tableName, tsField, severityField string) error {
	tsQuery := fmt.Sprintf(
		`SELECT type FROM system.columns WHERE database = '%s' AND table = '%s' AND name = '%s'`,
		database, tableName, tsField,
	)
	tsResult, err := client.Query(ctx, tsQuery)
	if err != nil {
		p.log.Error("failed to query timestamp column type during validation", "error", err, "database", database, "table", tableName, "ts_field", tsField)
		return &ValidationError{Field: "meta_ts_field", Message: "Failed to query timestamp column type", Err: err}
	}
	if len(tsResult.Logs) == 0 {
		return &ValidationError{Field: "meta_ts_field", Message: fmt.Sprintf("Timestamp field '%s' not found in table '%s.%s'", tsField, database, tableName)}
	}
	tsType, ok := tsResult.Logs[0]["type"].(string)
	if !ok {
		return &ValidationError{Field: "meta_ts_field", Message: fmt.Sprintf("Failed to determine type of timestamp field '%s'", tsField)}
	}
	if !strings.HasPrefix(tsType, "DateTime") {
		return &ValidationError{Field: "meta_ts_field", Message: fmt.Sprintf("Timestamp field '%s' must be DateTime or DateTime64, found %s", tsField, tsType)}
	}

	if severityField == "" {
		return nil
	}

	sevQuery := fmt.Sprintf(
		`SELECT type FROM system.columns WHERE database = '%s' AND table = '%s' AND name = '%s'`,
		database, tableName, severityField,
	)
	sevResult, err := client.Query(ctx, sevQuery)
	if err != nil {
		p.log.Error("failed to query severity column type during validation", "error", err, "database", database, "table", tableName, "severity_field", severityField)
		return &ValidationError{Field: "meta_severity_field", Message: "Failed to query severity column type", Err: err}
	}
	if len(sevResult.Logs) == 0 {
		return &ValidationError{Field: "meta_severity_field", Message: fmt.Sprintf("Severity field '%s' not found in table '%s.%s'", severityField, database, tableName)}
	}
	sevType, ok := sevResult.Logs[0]["type"].(string)
	if !ok {
		return &ValidationError{Field: "meta_severity_field", Message: fmt.Sprintf("Failed to determine type of severity field '%s'", severityField)}
	}
	if sevType != "String" && !strings.Contains(sevType, "LowCardinality(String)") {
		return &ValidationError{Field: "meta_severity_field", Message: fmt.Sprintf("Severity field '%s' must be String or LowCardinality(String), found %s", severityField, sevType)}
	}

	return nil
}

func parseTimeWindow(window string) (clickhouse.TimeWindow, error) {
	windowMap := map[string]clickhouse.TimeWindow{
		"1s": clickhouse.TimeWindow1s, "5s": clickhouse.TimeWindow5s,
		"10s": clickhouse.TimeWindow10s, "15s": clickhouse.TimeWindow15s, "30s": clickhouse.TimeWindow30s,
		"1m": clickhouse.TimeWindow1m, "5m": clickhouse.TimeWindow5m,
		"10m": clickhouse.TimeWindow10m, "15m": clickhouse.TimeWindow15m, "30m": clickhouse.TimeWindow30m,
		"1h": clickhouse.TimeWindow1h, "2h": clickhouse.TimeWindow2h, "3h": clickhouse.TimeWindow3h,
		"6h": clickhouse.TimeWindow6h, "12h": clickhouse.TimeWindow12h,
		"24h": clickhouse.TimeWindow24h, "1d": clickhouse.TimeWindow24h,
	}

	if tw, ok := windowMap[window]; ok {
		return tw, nil
	}
	return "", fmt.Errorf("invalid histogram window: %s", window)
}
