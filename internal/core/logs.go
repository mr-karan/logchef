package core

import (
	"context"
	"errors"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func QueryLogs(ctx context.Context, ds *datasource.Service, sourceID models.SourceID, params datasource.QueryRequest) (*models.QueryResult, error) {
	result, err := ds.QueryLogs(ctx, sourceID, params)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrSourceNotFound
		}
		return nil, err
	}
	return result, nil
}

func GetSourceSchema(ctx context.Context, ds *datasource.Service, sourceID models.SourceID) ([]models.ColumnInfo, error) {
	schema, err := ds.GetSourceSchema(ctx, sourceID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
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
		if errors.Is(err, models.ErrNotFound) {
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
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrSourceNotFound
		}
		return nil, err
	}
	return result, nil
}

func GetAllFieldValues(ctx context.Context, ds *datasource.Service, sourceID models.SourceID, params AllFieldValuesParams) (AllFieldValuesResult, error) {
	result, err := ds.GetAllFieldValues(ctx, sourceID, params)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrSourceNotFound
		}
		return nil, err
	}
	return result, nil
}

// --- Log Context Functions ---

// LogContextParams defines parameters for the log context query.
type LogContextParams = datasource.LogContextRequest

// LogContextResponse structures the response for log context data.
type LogContextResponse = models.LogContextResponse

// GetLogContext retrieves surrounding logs around a specific timestamp for
// contextual analysis (grep -C for logs). Sources whose provider does not
// support log context report datasource.ErrOperationNotSupported.
func GetLogContext(ctx context.Context, ds *datasource.Service, sourceID models.SourceID, params LogContextParams) (*LogContextResponse, error) {
	result, err := ds.GetLogContext(ctx, sourceID, params)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil, ErrSourceNotFound
		}
		return nil, err
	}
	return result, nil
}
