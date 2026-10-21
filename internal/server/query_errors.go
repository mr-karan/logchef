package server

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	logsQueryOperation      = "Log querying"
	logchefQLQueryOperation = "LogchefQL querying"
)

type queryErrorResponse struct {
	status    int
	message   string
	errorType models.ErrorType
}

func classifyQueryError(err error, failure, operation string) queryErrorResponse {
	reqErr, isRequestError := errors.AsType[*core.QueryRequestError](err)
	admissionErr, isAdmissionError := errors.AsType[*QueryAdmissionError](err)
	validationErr, isValidationError := errors.AsType[*datasource.ValidationError](err)
	clickhouseErr, isClickhouseError := errors.AsType[*clickhouse.ValidationError](err)
	queryErr, isQueryError := errors.AsType[*clickhouse.QueryError](err)
	switch {
	case isRequestError:
		return queryErrorResponse{fiber.StatusBadRequest, reqErr.Message, models.ValidationErrorType}
	case isAdmissionError:
		return queryErrorResponse{fiber.StatusTooManyRequests, admissionErr.Message, models.ValidationErrorType}
	case errors.Is(err, context.Canceled):
		return queryErrorResponse{fiber.StatusRequestTimeout, "Request cancelled", models.ExternalServiceErrorType}
	case clickhouse.IsTimeoutError(err):
		return queryErrorResponse{fiber.StatusRequestTimeout, "Request timed out", models.ExternalServiceErrorType}
	case errors.Is(err, core.ErrSourceNotFound), errors.Is(err, models.ErrNotFound):
		return queryErrorResponse{fiber.StatusNotFound, "Source not found", models.NotFoundErrorType}
	case errors.Is(err, core.ErrLogchefQLNotSupported):
		return queryErrorResponse{fiber.StatusBadRequest, "LogchefQL is not supported for this source", models.ValidationErrorType}
	case errors.Is(err, datasource.ErrOperationNotSupported):
		return queryErrorResponse{fiber.StatusBadRequest, operation + " is not supported for this source type yet", models.ValidationErrorType}
	case errors.Is(err, models.ErrHistogramBudgetExceeded):
		return queryErrorResponse{fiber.StatusBadRequest, models.ErrHistogramBudgetExceeded.Error(), models.ValidationErrorType}
	case isValidationError:
		return queryErrorResponse{fiber.StatusBadRequest, validationErr.Message, models.ValidationErrorType}
	case isClickhouseError:
		return queryErrorResponse{fiber.StatusBadRequest, clickhouseErr.Message, models.ValidationErrorType}
	case isQueryError:
		return queryErrorResponse{fiber.StatusBadRequest, queryErr.Message, models.ValidationErrorType}
	default:
		return queryErrorResponse{fiber.StatusInternalServerError, failure, models.DatabaseErrorType}
	}
}

func (s *Server) sendQueryError(c fiber.Ctx, sourceID models.SourceID, err error, failure, operation string) error {
	response := classifyQueryError(err, failure, operation)
	if response.status >= fiber.StatusInternalServerError {
		s.log.Error("query failed", "error", err, "source_id", sourceID, "path", c.Path())
	} else if datasource.IsValidationError(err) {
		s.log.Warn("query validation failed", "error", err, "source_id", sourceID, "path", c.Path())
	}
	return SendErrorWithType(c, response.status, response.message, response.errorType)
}
