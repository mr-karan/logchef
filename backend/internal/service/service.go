package service

import (
	"backend-v2/internal/sqlite"
	"backend-v2/pkg/clickhouse"
)

// Service manages the application's core functionality
type Service struct {
	sqlite     *sqlite.DB
	clickhouse *clickhouse.Pool
}

// New creates a new service instance
func New(sqliteDB *sqlite.DB) *Service {
	return &Service{
		sqlite:     sqliteDB,
		clickhouse: clickhouse.NewPool(),
	}
}
