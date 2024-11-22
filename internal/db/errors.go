package db

import "errors"

var (
	ErrConnectionNotFound   = errors.New("connection not found")
	ErrConnectionUnhealthy = errors.New("connection is unhealthy")
)
