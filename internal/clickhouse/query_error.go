package clickhouse

import (
	"errors"
	"regexp"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// QueryError carries a query diagnostic without the driver's error wrapper.
type QueryError struct {
	Message string
	Err     error
}

func (e *QueryError) Error() string { return e.Message }
func (e *QueryError) Unwrap() error { return e.Err }

var queryErrorAddress = regexp.MustCompile(`(?:[a-zA-Z0-9_.-]+|\[[0-9a-fA-F:.%]+\]):[0-9]+|\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b|(?:[0-9a-fA-F]*:){2,}[0-9a-fA-F:.]*`)

func publicQueryError(err error) error {
	exception, ok := errors.AsType[*clickhouse.Exception](err)
	if !ok {
		return err
	}
	// Only query diagnostics from ClickHouse's src/Common/ErrorCodes.cpp are public.
	switch exception.Code {
	case 16, 34, 35, 36, 42, 43, 46, 47, 50, 53, 59, 60, 62, 63,
		69, 70, 80, 81, 158, 184, 215, 307, 386, 396:
		message := exception.Message
		for strings.HasPrefix(message, "Received from ") {
			_, diagnostic, found := strings.Cut(message, ". DB::Exception: ")
			if !found {
				return err
			}
			message = diagnostic
		}
		message, _, _ = strings.Cut(message, "Stack trace:")
		message, _, _ = strings.Cut(message, "While executing ")
		message = strings.TrimSpace(message)
		if message == "" || queryErrorAddress.MatchString(message) {
			return err
		}
		return &QueryError{Message: message, Err: err}
	default:
		return err
	}
}
