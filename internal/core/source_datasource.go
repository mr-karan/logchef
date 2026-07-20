package core

import (
	"context"
	"errors"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

type sourceErrorWrap struct {
	message string
	err     error
}

func (e *sourceErrorWrap) Error() string {
	return e.message
}

func (e *sourceErrorWrap) Unwrap() error {
	return e.err
}

func normalizeDatasourceError(err error) error {
	if err == nil {
		return nil
	}

	var validationErr *datasource.ValidationError
	if errors.As(err, &validationErr) {
		return &ValidationError{
			Field:   validationErr.Field,
			Message: validationErr.Message,
			Err:     validationErr.Err,
		}
	}

	if errors.Is(err, datasource.ErrSourceAlreadyExists) {
		return &sourceErrorWrap{
			message: err.Error(),
			err:     ErrSourceAlreadyExists,
		}
	}

	return err
}

func CreateSourceFromRequest(ctx context.Context, ds *datasource.Service, req *models.CreateSourceRequest) (*models.Source, error) {
	source, err := ds.CreateSource(ctx, req)
	if err != nil {
		return nil, normalizeDatasourceError(err)
	}
	return source, nil
}

func ValidateSourceConnection(ctx context.Context, ds *datasource.Service, req *models.ValidateConnectionRequest) (*models.ConnectionValidationResult, error) {
	result, err := ds.ValidateConnection(ctx, req)
	if err != nil {
		return nil, normalizeDatasourceError(err)
	}
	return result, nil
}
