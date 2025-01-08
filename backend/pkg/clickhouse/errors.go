package clickhouse

import "errors"

var (
	// ErrConnectionNotFound is returned when a connection is not found
	ErrConnectionNotFound = errors.New("connection not found")

	// ErrSourceExists is returned when trying to create a source that already exists
	ErrSourceExists = errors.New("source already exists")

	// ErrInvalidSourceType is returned when the source type is not supported
	ErrInvalidSourceType = errors.New("invalid source type")
)
