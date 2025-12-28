package backends

import (
	"context"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

var _ BackendClient = (*ClickHouseAdapter)(nil)

type ClickHouseAdapter struct {
	client *clickhouse.Client
}

func NewClickHouseAdapter(client *clickhouse.Client) *ClickHouseAdapter {
	return &ClickHouseAdapter{client: client}
}

func (a *ClickHouseAdapter) Query(ctx context.Context, query string, timeoutSeconds *int) (*models.QueryResult, error) {
	return a.client.QueryWithTimeout(ctx, query, timeoutSeconds)
}

func (a *ClickHouseAdapter) GetTableInfo(ctx context.Context, database, table string) (*TableInfo, error) {
	info, err := a.client.GetTableInfo(ctx, database, table)
	if err != nil {
		return nil, err
	}

	return &TableInfo{
		Database:     info.Database,
		Name:         info.Name,
		Engine:       info.Engine,
		EngineParams: info.EngineParams,
		Columns:      info.Columns,
		SortKeys:     info.SortKeys,
		CreateQuery:  info.CreateQuery,
	}, nil
}

func (a *ClickHouseAdapter) GetHistogramData(ctx context.Context, tableName, timestampField string, params HistogramParams) (*HistogramResult, error) {
	chParams := clickhouse.HistogramParams{
		Query:        params.Query,
		GroupBy:      params.GroupBy,
		Timezone:     params.Timezone,
		QueryTimeout: params.TimeoutSeconds,
	}

	window, err := parseTimeWindow(params.Window)
	if err != nil {
		return nil, err
	}
	chParams.Window = window

	result, err := a.client.GetHistogramData(ctx, tableName, timestampField, chParams)
	if err != nil {
		return nil, err
	}

	data := make([]HistogramData, len(result.Data))
	for i, d := range result.Data {
		data[i] = HistogramData{
			Bucket:     d.Bucket,
			LogCount:   d.LogCount,
			GroupValue: d.GroupValue,
		}
	}

	return &HistogramResult{
		Granularity: result.Granularity,
		Data:        data,
	}, nil
}

func (a *ClickHouseAdapter) GetSurroundingLogs(ctx context.Context, tableName, timestampField string, params LogContextParams, timeoutSeconds *int) (*LogContextResult, error) {
	chParams := clickhouse.LogContextParams{
		TargetTime:      params.TargetTime,
		BeforeLimit:     params.BeforeLimit,
		AfterLimit:      params.AfterLimit,
		BeforeOffset:    params.BeforeOffset,
		AfterOffset:     params.AfterOffset,
		ExcludeBoundary: params.ExcludeBoundary,
	}

	result, err := a.client.GetSurroundingLogs(ctx, tableName, timestampField, chParams, timeoutSeconds)
	if err != nil {
		return nil, err
	}

	return &LogContextResult{
		BeforeLogs: result.BeforeLogs,
		TargetLogs: result.TargetLogs,
		AfterLogs:  result.AfterLogs,
		Stats:      result.Stats,
	}, nil
}

func (a *ClickHouseAdapter) GetFieldDistinctValues(ctx context.Context, database, table string, params FieldValuesParams) (*FieldValuesResult, error) {
	chParams := clickhouse.FieldValuesParams{
		FieldName:      params.FieldName,
		FieldType:      params.FieldType,
		TimestampField: params.TimestampField,
		StartTime:      params.TimeRange.Start,
		EndTime:        params.TimeRange.End,
		Timezone:       params.Timezone,
		Limit:          params.Limit,
		Timeout:        params.TimeoutSeconds,
		LogchefQL:      params.FilterQuery,
	}

	result, err := a.client.GetFieldDistinctValues(ctx, database, table, chParams)
	if err != nil {
		return nil, err
	}

	values := make([]FieldValueInfo, len(result.Values))
	for i, v := range result.Values {
		values[i] = FieldValueInfo{
			Value: v.Value,
			Count: v.Count,
		}
	}

	return &FieldValuesResult{
		FieldName:        result.FieldName,
		FieldType:        result.FieldType,
		IsLowCardinality: result.IsLowCard,
		Values:           values,
		TotalDistinct:    result.TotalDistinct,
	}, nil
}

func (a *ClickHouseAdapter) GetAllFilterableFieldValues(ctx context.Context, database, table string, params AllFieldValuesParams) (map[string]*FieldValuesResult, error) {
	chParams := clickhouse.AllFieldValuesParams{
		TimestampField: params.TimestampField,
		StartTime:      params.TimeRange.Start,
		EndTime:        params.TimeRange.End,
		Timezone:       params.Timezone,
		Limit:          params.Limit,
		Timeout:        params.TimeoutSeconds,
		LogchefQL:      params.FilterQuery,
	}

	results, err := a.client.GetAllFilterableFieldValues(ctx, database, table, chParams)
	if err != nil {
		return nil, err
	}

	converted := make(map[string]*FieldValuesResult, len(results))
	for name, result := range results {
		values := make([]FieldValueInfo, len(result.Values))
		for i, v := range result.Values {
			values[i] = FieldValueInfo{
				Value: v.Value,
				Count: v.Count,
			}
		}
		converted[name] = &FieldValuesResult{
			FieldName:        result.FieldName,
			FieldType:        result.FieldType,
			IsLowCardinality: result.IsLowCard,
			Values:           values,
			TotalDistinct:    result.TotalDistinct,
		}
	}

	return converted, nil
}

func (a *ClickHouseAdapter) Ping(ctx context.Context, database, table string) error {
	return a.client.Ping(ctx, database, table)
}

func (a *ClickHouseAdapter) Close() error {
	return a.client.Close()
}

func (a *ClickHouseAdapter) Reconnect(ctx context.Context) error {
	return a.client.Reconnect(ctx)
}

func (a *ClickHouseAdapter) UnwrapClient() *clickhouse.Client {
	return a.client
}

func parseTimeWindow(window string) (clickhouse.TimeWindow, error) {
	windowMap := map[string]clickhouse.TimeWindow{
		"1s":  clickhouse.TimeWindow1s,
		"5s":  clickhouse.TimeWindow5s,
		"10s": clickhouse.TimeWindow10s,
		"15s": clickhouse.TimeWindow15s,
		"30s": clickhouse.TimeWindow30s,
		"1m":  clickhouse.TimeWindow1m,
		"5m":  clickhouse.TimeWindow5m,
		"10m": clickhouse.TimeWindow10m,
		"15m": clickhouse.TimeWindow15m,
		"30m": clickhouse.TimeWindow30m,
		"1h":  clickhouse.TimeWindow1h,
		"2h":  clickhouse.TimeWindow2h,
		"3h":  clickhouse.TimeWindow3h,
		"6h":  clickhouse.TimeWindow6h,
		"12h": clickhouse.TimeWindow12h,
		"24h": clickhouse.TimeWindow24h,
		"1d":  clickhouse.TimeWindow24h,
	}

	if tw, ok := windowMap[window]; ok {
		return tw, nil
	}
	return "", clickhouse.ErrInvalidQuery
}
