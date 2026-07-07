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
	"github.com/mr-karan/logchef/internal/logchefql"
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

func (p *ClickHouseProvider) Capabilities() []Capability {
	return []Capability{
		CapabilitySchemaInspection,
		CapabilityHistogram,
		CapabilityFieldValues,
		CapabilitySourceInspection,
		CapabilityAISQLGeneration,
		CapabilityLogContext,
		CapabilityExports,
	}
}

func (p *ClickHouseProvider) SupportedQueryLanguages() []models.QueryLanguage {
	return []models.QueryLanguage{
		models.QueryLanguageLogchefQL,
		models.QueryLanguageClickHouseSQL,
	}
}

func (p *ClickHouseProvider) SupportedSavedQueryEditorModes() []models.SavedQueryEditorMode {
	return []models.SavedQueryEditorMode{
		models.SavedQueryEditorModeBuilder,
		models.SavedQueryEditorModeNative,
	}
}

func (p *ClickHouseProvider) SupportedAlertEditorModes() []models.AlertEditorMode {
	return []models.AlertEditorMode{
		models.AlertEditorModeCondition,
		models.AlertEditorModeNative,
	}
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

func (p *ClickHouseProvider) UpdateSource(ctx context.Context, source *models.Source, req *models.UpdateSourceRequest) (*SourceUpdateResult, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}
	if req == nil {
		return nil, fmt.Errorf("update source request is required")
	}

	changed, err := ApplyCommonSourceUpdates(source, req)
	if err != nil {
		return nil, err
	}

	metaChanged := req.MetaTSField != nil || req.MetaSeverityField != nil
	connectionChanged := req.HasConnectionChanges()
	var client *clickhouse.Client

	if connectionChanged {
		conn, err := p.connectionFromConfig(req.Connection)
		if err != nil {
			return nil, err
		}
		// API responses never include the password, so edit flows send it blank
		// to mean "keep the existing one". A blank username means auth was
		// turned off, so the stored password is dropped too.
		if conn.Password == "" && conn.Username != "" {
			conn.Password = source.Connection.Password
		}
		if err := validateClickHouseConnection("connection.", true, conn.Host, conn.Username, conn.Password, conn.Database, conn.TableName); err != nil {
			return nil, err
		}

		tempSource := &models.Source{SourceType: models.SourceTypeClickHouse, Connection: conn}
		client, err = p.manager.CreateTemporaryClient(ctx, tempSource)
		if err != nil {
			return nil, &ValidationError{Field: "connection", Message: "Failed to connect with new credentials", Err: err}
		}
		defer client.Close()

		if err := client.Ping(ctx, conn.Database, conn.TableName); err != nil {
			return nil, &ValidationError{
				Field:   "connection",
				Message: fmt.Sprintf("Table '%s.%s' not accessible with new credentials", conn.Database, conn.TableName),
				Err:     err,
			}
		}

		source.Connection = conn
		changed = true
	}

	if metaChanged || connectionChanged {
		if client == nil {
			existingClient, err := p.manager.GetConnection(source.ID)
			if err != nil {
				return nil, fmt.Errorf("get connection for source %d: %w", source.ID, err)
			}
			client = existingClient
		}

		if err := p.validateColumnTypes(ctx, client, source.Connection.Database, source.Connection.TableName, source.MetaTSField, source.MetaSeverityField); err != nil {
			return nil, err
		}
	}

	if err := source.SyncConnectionConfig(); err != nil {
		return nil, err
	}

	return &SourceUpdateResult{
		Source:       source,
		Changed:      changed,
		Reinitialize: connectionChanged,
	}, nil
}

func (p *ClickHouseProvider) PopulateSourceDetails(ctx context.Context, source *models.Source) error {
	if source == nil {
		return fmt.Errorf("source is required")
	}

	source.Columns = nil
	source.Schema = ""
	source.Engine = ""
	source.EngineParams = nil
	source.SortKeys = nil

	if !source.IsConnected {
		return nil
	}

	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		p.log.Warn("failed to get clickhouse connection for source details", "source_id", source.ID, "error", err)
		return nil
	}

	tableInfo, err := client.GetTableInfo(ctx, source.Connection.Database, source.Connection.TableName)
	if err != nil {
		p.log.Warn("failed to get clickhouse table info", "source_id", source.ID, "error", err)
		return nil
	}

	source.Columns = tableInfo.Columns
	source.Schema = tableInfo.CreateQuery
	source.Engine = tableInfo.Engine
	source.EngineParams = tableInfo.EngineParams
	source.SortKeys = tableInfo.SortKeys
	return nil
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
	buildResult, err := qb.BuildRawQueryWithLimitPolicy(req.RawQuery, req.Limit, req.DefaultLimit, req.MaxLimit)
	if err != nil {
		return nil, fmt.Errorf("invalid query syntax: %w", err)
	}

	return client.QueryWithOptions(ctx, buildResult.SQL, clickhouse.QueryOptions{
		TimeoutSeconds: req.QueryTimeout,
		Settings: map[string]any{
			"max_execution_time":   *req.QueryTimeout,
			"max_result_rows":      buildResult.AppliedLimit,
			"result_overflow_mode": "break",
		},
		LimitApplied:     buildResult.AppliedLimit,
		MaxRows:          buildResult.AppliedLimit,
		MaxResponseBytes: req.MaxResponseBytes,
		Warnings:         queryWarningsForBuildResult(buildResult),
	})
}

func queryWarningsForBuildResult(result clickhouse.QueryBuildResult) []models.QueryWarning {
	warnings := make([]models.QueryWarning, 0, 2)
	if result.LimitAdded {
		warnings = append(warnings, models.QueryWarning{
			Code:    "LIMIT_APPLIED",
			Message: fmt.Sprintf("Showing first %d rows. Use download for larger results.", result.AppliedLimit),
		})
	}
	if result.LimitCapped {
		warnings = append(warnings, models.QueryWarning{
			Code:    "LIMIT_CAPPED",
			Message: fmt.Sprintf("Result limit capped at %d rows.", result.AppliedLimit),
		})
	}
	return warnings
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

func (p *ClickHouseProvider) GetFieldValues(ctx context.Context, source *models.Source, req FieldValuesRequest) (*FieldValuesResult, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}
	if strings.TrimSpace(req.TimestampField) == "" {
		req.TimestampField = source.MetaTSField
	}

	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to source %d: %w", source.ID, err)
	}

	result, err := client.GetFieldDistinctValues(ctx, source.Connection.Database, source.Connection.TableName, clickhouse.FieldValuesParams{
		FieldName:      req.FieldName,
		FieldType:      req.FieldType,
		TimestampField: req.TimestampField,
		StartTime:      req.StartTime,
		EndTime:        req.EndTime,
		Timezone:       req.Timezone,
		Limit:          req.Limit,
		Timeout:        req.Timeout,
		LogchefQL:      req.QueryText,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get field values: %w", err)
	}

	values := make([]FieldValueInfo, 0, len(result.Values))
	for _, value := range result.Values {
		values = append(values, FieldValueInfo{
			Value: value.Value,
			Count: value.Count,
		})
	}

	return &FieldValuesResult{
		FieldName:        result.FieldName,
		FieldType:        result.FieldType,
		IsLowCardinality: result.IsLowCard,
		Values:           values,
		TotalDistinct:    result.TotalDistinct,
	}, nil
}

func (p *ClickHouseProvider) GetAllFieldValues(ctx context.Context, source *models.Source, req AllFieldValuesRequest) (AllFieldValuesResult, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}
	if strings.TrimSpace(req.TimestampField) == "" {
		req.TimestampField = source.MetaTSField
	}

	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to source %d: %w", source.ID, err)
	}

	result, err := client.GetAllFilterableFieldValues(ctx, source.Connection.Database, source.Connection.TableName, clickhouse.AllFieldValuesParams{
		TimestampField: req.TimestampField,
		StartTime:      req.StartTime,
		EndTime:        req.EndTime,
		Timezone:       req.Timezone,
		Limit:          req.Limit,
		Timeout:        req.Timeout,
		LogchefQL:      req.QueryText,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get field values: %w", err)
	}

	mapped := make(AllFieldValuesResult, len(result))
	for fieldName, fieldResult := range result {
		if fieldResult == nil {
			continue
		}
		values := make([]FieldValueInfo, 0, len(fieldResult.Values))
		for _, value := range fieldResult.Values {
			values = append(values, FieldValueInfo{
				Value: value.Value,
				Count: value.Count,
			})
		}
		mapped[fieldName] = &FieldValuesResult{
			FieldName:        fieldResult.FieldName,
			FieldType:        fieldResult.FieldType,
			IsLowCardinality: fieldResult.IsLowCard,
			Values:           values,
			TotalDistinct:    fieldResult.TotalDistinct,
		}
	}

	return mapped, nil
}

func (p *ClickHouseProvider) InspectSource(ctx context.Context, source *models.Source) (*SourceInspection, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}

	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client for source %d: %w", source.ID, err)
	}

	tableInfo, _ := client.GetTableInfo(ctx, source.Connection.Database, source.Connection.TableName)
	ttlExpr := extractTTLFromTableInfo(ctx, client, tableInfo)
	statsDB, statsTable := getStatsTableLocation(source, tableInfo)

	tableStats, _ := client.TableStats(ctx, statsDB, statsTable)
	columnStats, _ := client.ColumnStats(ctx, statsDB, statsTable)
	ingestionStats, _ := client.IngestionStats(ctx, statsDB, statsTable, source.MetaTSField)

	return &SourceInspection{
		Details:  buildClickHouseInspectionDetails(source, tableInfo),
		Storage:  buildClickHouseStorageMetrics(tableStats),
		Activity: mapActivityStats(ingestionStats),
		Schema:   mapClickHouseSchemaInspection(tableInfo, source, ttlExpr, columnStats),
	}, nil
}

func (p *ClickHouseProvider) EvaluateAlert(ctx context.Context, source *models.Source, req AlertQueryRequest) (*models.QueryResult, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}
	if language := models.NormalizeQueryLanguage(req.Language); language != "" && language != models.QueryLanguageClickHouseSQL {
		return nil, fmt.Errorf("clickhouse alerts require %q, got %q", models.QueryLanguageClickHouseSQL, language)
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
	return p.manager.GetCachedHealth(sourceID)
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

func extractTTLFromTableInfo(ctx context.Context, client *clickhouse.Client, tableInfo *clickhouse.TableInfo) string {
	if tableInfo == nil || tableInfo.CreateQuery == "" {
		return ""
	}

	if tableInfo.Engine == "Distributed" && len(tableInfo.EngineParams) >= 3 {
		localDB, localTable := tableInfo.EngineParams[1], tableInfo.EngineParams[2]
		localTableInfo, err := client.GetTableInfo(ctx, localDB, localTable)
		if err == nil && localTableInfo != nil {
			return extractTTLFromCreateQuery(localTableInfo.CreateQuery)
		}
		return ""
	}
	return extractTTLFromCreateQuery(tableInfo.CreateQuery)
}

func extractTTLFromCreateQuery(createQuery string) string {
	if createQuery == "" {
		return ""
	}

	ttlIndex := strings.Index(strings.ToUpper(createQuery), " TTL ")
	if ttlIndex == -1 {
		return ""
	}

	return parseTTLExpression(createQuery[ttlIndex+5:])
}

func parseTTLExpression(ttlPart string) string {
	if ttlPart == "" {
		return ""
	}

	parenCount := 0
	endIndex := len(ttlPart)

	for i, char := range ttlPart {
		switch char {
		case '(':
			parenCount++
		case ')':
			parenCount--
			if parenCount == 0 {
				remaining := strings.TrimSpace(ttlPart[i+1:])
				upperRemaining := strings.ToUpper(remaining)
				if strings.HasPrefix(upperRemaining, "SETTINGS") ||
					strings.HasPrefix(upperRemaining, "DELETE") ||
					strings.HasPrefix(upperRemaining, "TO DISK") ||
					strings.HasPrefix(upperRemaining, "TO VOLUME") ||
					remaining == "" {
					endIndex = i
					break
				}
			}
		case ' ':
			if parenCount == 0 {
				remaining := strings.TrimSpace(ttlPart[i:])
				upperRemaining := strings.ToUpper(remaining)
				if strings.HasPrefix(upperRemaining, "SETTINGS") ||
					strings.HasPrefix(upperRemaining, "DELETE") ||
					strings.HasPrefix(upperRemaining, "TO DISK") ||
					strings.HasPrefix(upperRemaining, "TO VOLUME") {
					endIndex = i
					break
				}
			}
		}
	}

	ttlExpr := strings.TrimSpace(ttlPart[:endIndex])
	return strings.TrimRight(ttlExpr, ",")
}

func getStatsTableLocation(source *models.Source, tableInfo *clickhouse.TableInfo) (database, table string) {
	if tableInfo != nil && tableInfo.Engine == "Distributed" && len(tableInfo.EngineParams) >= 3 {
		return tableInfo.EngineParams[1], tableInfo.EngineParams[2]
	}
	return source.Connection.Database, source.Connection.TableName
}

func mapActivityStats(stats *clickhouse.IngestionStats) *SourceActivity {
	if stats == nil {
		return nil
	}

	hourly := make([]IngestionBucket, 0, len(stats.HourlyBuckets))
	for _, bucket := range stats.HourlyBuckets {
		hourly = append(hourly, IngestionBucket{
			Bucket: bucket.Bucket,
			Rows:   bucket.Rows,
		})
	}

	daily := make([]IngestionBucket, 0, len(stats.DailyBuckets))
	for _, bucket := range stats.DailyBuckets {
		daily = append(daily, IngestionBucket{
			Bucket: bucket.Bucket,
			Rows:   bucket.Rows,
		})
	}

	return &SourceActivity{
		Rows1h:        stats.Rows1h,
		Rows24h:       stats.Rows24h,
		Rows7d:        stats.Rows7d,
		LatestTS:      stats.LatestTS,
		HourlyBuckets: hourly,
		DailyBuckets:  daily,
	}
}

func buildClickHouseInspectionDetails(source *models.Source, tableInfo *clickhouse.TableInfo) []InspectionDetail {
	details := []InspectionDetail{
		{Key: "backend", Label: "Backend", Value: "ClickHouse"},
		{Key: "host", Label: "Host", Value: source.Connection.Host, Monospace: true},
		{Key: "database", Label: "Database", Value: source.Connection.Database, Monospace: true},
		{Key: "table", Label: "Table", Value: source.Connection.TableName, Monospace: true},
		{Key: "timestamp_field", Label: "Timestamp Field", Value: source.MetaTSField, Monospace: true},
	}

	if source.MetaSeverityField != "" {
		details = append(details, InspectionDetail{
			Key: "severity_field", Label: "Severity Field", Value: source.MetaSeverityField, Monospace: true,
		})
	}
	if tableInfo != nil && tableInfo.Engine != "" {
		details = append(details, InspectionDetail{
			Key: "engine", Label: "Engine", Value: tableInfo.Engine,
		})
	}
	return details
}

func buildClickHouseStorageMetrics(stat *clickhouse.TableStat) []InspectionMetric {
	if stat == nil {
		return nil
	}
	return []InspectionMetric{
		{Key: "rows", Label: "Rows", Value: strconv.FormatUint(stat.Rows, 10)},
		{Key: "parts", Label: "Parts", Value: strconv.FormatUint(stat.PartCount, 10)},
		{Key: "compression_ratio", Label: "Compression Ratio", Value: fmt.Sprintf("%.2fx", stat.ComprRate)},
		{Key: "compressed", Label: "Compressed Size", Value: stat.Compressed},
		{Key: "uncompressed", Label: "Uncompressed Size", Value: stat.Uncompressed},
	}
}

func mapClickHouseSchemaInspection(
	tableInfo *clickhouse.TableInfo,
	source *models.Source,
	ttlExpr string,
	columnStats []clickhouse.TableColumnStat,
) *SourceSchemaInspection {
	if tableInfo == nil && source == nil {
		return nil
	}

	schema := &SourceSchemaInspection{
		TTL: ttlExpr,
	}
	columnStatMap := make(map[string]clickhouse.TableColumnStat, len(columnStats))
	for _, stat := range columnStats {
		columnStatMap[stat.Column] = stat
	}

	if tableInfo != nil {
		schema.SortKeys = append(schema.SortKeys, tableInfo.SortKeys...)
		schema.CreateQuery = tableInfo.CreateQuery
		if len(tableInfo.ExtColumns) > 0 {
			schema.Fields = make([]SourceSchemaField, 0, len(tableInfo.ExtColumns))
			for _, col := range tableInfo.ExtColumns {
				stat := columnStatMap[col.Name]
				schema.Fields = append(schema.Fields, SourceSchemaField{
					Name:              col.Name,
					Type:              col.Type,
					IsNullable:        col.IsNullable,
					IsPrimaryKey:      col.IsPrimaryKey,
					DefaultExpression: col.DefaultExpression,
					Comment:           col.Comment,
					Compressed:        stat.Compressed,
					Uncompressed:      stat.Uncompressed,
					CompressionRatio:  stat.ComprRatio,
					AvgRowSize:        stat.AvgRowSize,
					RowCount:          stat.RowsCount,
				})
			}
			return schema
		}

		schema.Fields = make([]SourceSchemaField, 0, len(tableInfo.Columns))
		for _, col := range tableInfo.Columns {
			stat := columnStatMap[col.Name]
			schema.Fields = append(schema.Fields, SourceSchemaField{
				Name:             col.Name,
				Type:             col.Type,
				Compressed:       stat.Compressed,
				Uncompressed:     stat.Uncompressed,
				CompressionRatio: stat.ComprRatio,
				AvgRowSize:       stat.AvgRowSize,
				RowCount:         stat.RowsCount,
			})
		}
		return schema
	}

	if source != nil && len(source.Columns) > 0 {
		schema.Fields = make([]SourceSchemaField, 0, len(source.Columns))
		for _, col := range source.Columns {
			stat := columnStatMap[col.Name]
			schema.Fields = append(schema.Fields, SourceSchemaField{
				Name:             col.Name,
				Type:             col.Type,
				Compressed:       stat.Compressed,
				Uncompressed:     stat.Uncompressed,
				CompressionRatio: stat.ComprRatio,
				AvgRowSize:       stat.AvgRowSize,
				RowCount:         stat.RowsCount,
			})
		}
	}

	return schema
}

// GetLogContext fetches logs surrounding a target timestamp via the ClickHouse
// surrounding-logs query pair.
func (p *ClickHouseProvider) GetLogContext(ctx context.Context, source *models.Source, req LogContextRequest) (*models.LogContextResponse, error) {
	if source.MetaTSField == "" {
		return nil, &ValidationError{Field: "meta_ts_field", Message: "source does not have a timestamp field configured"}
	}

	if req.BeforeLimit <= 0 {
		req.BeforeLimit = 10
	}
	if req.AfterLimit <= 0 {
		req.AfterLimit = 10
	}
	if req.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}

	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting database connection for source %d: %w", source.ID, err)
	}

	result, err := client.GetSurroundingLogs(ctx, source.GetFullTableName(), source.MetaTSField, clickhouse.LogContextParams{
		TargetTime:      time.UnixMilli(req.TargetTimestamp),
		BeforeLimit:     req.BeforeLimit,
		AfterLimit:      req.AfterLimit,
		BeforeOffset:    req.BeforeOffset,
		AfterOffset:     req.AfterOffset,
		ExcludeBoundary: req.ExcludeBoundary,
	}, req.QueryTimeout)
	if err != nil {
		return nil, fmt.Errorf("error fetching log context for source %d: %w", source.ID, err)
	}

	return &models.LogContextResponse{
		TargetTimestamp: req.TargetTimestamp,
		BeforeLogs:      result.BeforeLogs,
		TargetLogs:      result.TargetLogs,
		AfterLogs:       result.AfterLogs,
		Stats:           result.Stats,
	}, nil
}

// buildLogchefQLSchema builds a LogchefQL schema (name -> type) from the
// source's columns for type-aware SQL generation. Returns nil when no column
// metadata is available; the translator handles a nil schema gracefully.
func buildLogchefQLSchema(source *models.Source) *logchefql.Schema {
	if source == nil || len(source.Columns) == 0 {
		return nil
	}

	columns := make([]logchefql.ColumnInfo, len(source.Columns))
	for i, col := range source.Columns {
		columns[i] = logchefql.ColumnInfo{
			Name: col.Name,
			Type: col.Type,
		}
	}
	return &logchefql.Schema{Columns: columns}
}

// CompileLogchefQL compiles a LogchefQL query into executable ClickHouse SQL.
// When a complete time window is supplied it returns the full SELECT ... WHERE
// ... query with the time range baked in; otherwise Query and FilterOnly both
// carry the WHERE-clause-only SQL.
func (p *ClickHouseProvider) CompileLogchefQL(ctx context.Context, source *models.Source, req LogchefQLCompileRequest) (*CompiledLogchefQL, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}

	// Schema-aware SQL generation needs column types. The source resolved by
	// the service does not carry populated columns, so fetch the schema on
	// demand when it's missing. This is best-effort: a nil schema still yields a
	// valid (if less type-aware) translation, matching the behaviour when a
	// source is disconnected.
	if len(source.Columns) == 0 {
		if columns, err := p.GetSourceSchema(ctx, source); err == nil {
			source.Columns = columns
		}
	}
	schema := buildLogchefQLSchema(source)

	translateResult := logchefql.Translate(req.Query, schema)
	compiled := &CompiledLogchefQL{
		Language:   models.QueryLanguageClickHouseSQL,
		Valid:      translateResult.Valid,
		Error:      translateResult.Error,
		Conditions: translateResult.Conditions,
		FieldsUsed: translateResult.FieldsUsed,
		FilterOnly: translateResult.SQL,
		Query:      translateResult.SQL,
	}

	if !translateResult.Valid {
		if translateResult.Error != nil {
			return compiled, translateResult.Error
		}
		return compiled, &logchefql.ParseError{Code: logchefql.ErrUnexpectedToken, Message: "invalid LogchefQL query"}
	}

	// Build the full executable SQL (with time range) only when the caller
	// supplied a complete time window.
	if req.StartTime != "" && req.EndTime != "" && req.Timezone != "" {
		fullSQL, err := logchefql.BuildFullQuery(logchefql.QueryBuildParams{
			LogchefQL:      req.Query,
			Schema:         schema,
			TableName:      source.GetFullTableName(),
			TimestampField: source.MetaTSField,
			StartTime:      req.StartTime,
			EndTime:        req.EndTime,
			Timezone:       req.Timezone,
			Limit:          req.Limit,
		})
		if err != nil {
			return compiled, err
		}
		compiled.Query = fullSQL
	}

	return compiled, nil
}
