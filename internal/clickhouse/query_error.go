package clickhouse

import (
	"errors"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// QueryError carries a query diagnostic without the driver's error wrapper.
type QueryError struct {
	Message string
	Err     error
}

func (e *QueryError) Error() string { return e.Message }
func (e *QueryError) Unwrap() error { return e.Err }

func publicQueryError(err error) error {
	exception, ok := errors.AsType[*clickhouse.Exception](err)
	if !ok {
		return err
	}
	// Only query diagnostics from ClickHouse's src/Common/ErrorCodes.cpp are public.
	switch exception.Code {
	case 16, 34, 35, 36, 42, 43, 46, 47, 50, 53, 59, 60, 62, 63,
		69, 70, 80, 81, 158, 184, 215, 307, 386, 396:
		return &QueryError{Message: exception.Message, Err: err}
	default:
		return err
	}
}
