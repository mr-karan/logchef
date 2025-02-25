package clickhouse

import "errors"

// Common errors for the clickhouse package
var (
	// ErrSourceNotConnected is returned when a source is not connected
	ErrSourceNotConnected = errors.New("source not connected")

	// ErrQueryTimeout is returned when a query times out
	ErrQueryTimeout = errors.New("query timeout exceeded")

	// ErrInvalidQuery is returned when a query is invalid
	ErrInvalidQuery = errors.New("invalid query")

	// ErrConnectionFailed is returned when a connection cannot be established
	ErrConnectionFailed = errors.New("connection failed")

	// ErrSourceExists is returned when trying to create a source that already exists
	ErrSourceExists = errors.New("source already exists")

	// ErrInvalidSourceType is returned when the source type is not supported
	ErrInvalidSourceType = errors.New("invalid source type")
)
