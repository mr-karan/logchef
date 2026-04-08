package core

import (
	"context"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

func QueryLogs(ctx context.Context, ds *datasource.Service, sourceID models.SourceID, params datasource.QueryRequest) (*models.QueryResult, error) {
	result, err := ds.QueryLogs(ctx, sourceID, params)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return nil, ErrSourceNotFound
		}
		return nil, err
	}
	return result, nil
}

func GetSourceSchema(ctx context.Context, ds *datasource.Service, sourceID models.SourceID) ([]models.ColumnInfo, error) {
	schema, err := ds.GetSourceSchema(ctx, sourceID)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return nil, ErrSourceNotFound
		}
		return nil, err
	}
	return schema, nil
}

type HistogramParams = datasource.HistogramRequest
type HistogramResponse = datasource.HistogramResult

func GetHistogramData(ctx context.Context, ds *datasource.Service, sourceID models.SourceID, params HistogramParams) (*HistogramResponse, error) {
	result, err := ds.Histogram(ctx, sourceID, params)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return nil, ErrSourceNotFound
		}
		return nil, err
	}
	return result, nil
}

type LogContextParams = datasource.LogContextRequest
type LogContextResponse = datasource.LogContextResult

func GetLogContext(ctx context.Context, ds *datasource.Service, sourceID models.SourceID, params LogContextParams) (*LogContextResponse, error) {
	result, err := ds.LogContext(ctx, sourceID, params)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return nil, ErrSourceNotFound
		}
		return nil, err
	}
	return result, nil
}

type FieldValuesParams = datasource.FieldValuesRequest
type FieldValuesResult = datasource.FieldValuesResult
type AllFieldValuesParams = datasource.AllFieldValuesRequest
type AllFieldValuesResult = datasource.AllFieldValuesResult

func GetFieldValues(ctx context.Context, ds *datasource.Service, sourceID models.SourceID, params FieldValuesParams) (*FieldValuesResult, error) {
	result, err := ds.GetFieldValues(ctx, sourceID, params)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return nil, ErrSourceNotFound
		}
		return nil, err
	}
	return result, nil
}

func GetAllFieldValues(ctx context.Context, ds *datasource.Service, sourceID models.SourceID, params AllFieldValuesParams) (AllFieldValuesResult, error) {
	result, err := ds.GetAllFieldValues(ctx, sourceID, params)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return nil, ErrSourceNotFound
		}
		return nil, err
	}
	return result, nil
}
