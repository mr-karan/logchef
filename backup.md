# Logging System with Clickhouse

## Project Plan

1. Setup Development Environment
   - [ ] Set up Go backend
   - [ ] Set up Clickhouse database
   - [ ] Set up Vue3 frontend
   - [ ] Configure Tailwind and PrimeVue

2. Backend Development
   - [ ] Design and implement Clickhouse schema
   - [ ] Create Go structs for log entries
   - [ ] Implement CRUD operations for logs
   - [ ] Develop API endpoints for log management
   - [ ] Implement pagination and cursor-based navigation
   - [ ] Create filtering and search functionality
   - [ ] Implement multi-tenancy support

3. Frontend Development
   - [ ] Create main layout and navigation
   - [ ] Develop log listing component
   - [ ] Implement log filtering and search UI
   - [ ] Create log detail view
   - [ ] Implement pagination and infinite scrolling
   - [ ] Add real-time log updates (if applicable)

4. Integration and Testing
   - [ ] Integrate backend and frontend
   - [ ] Write unit tests for backend functions
   - [ ] Write integration tests
   - [ ] Perform end-to-end testing

5. Performance Optimization
   - [ ] Optimize Clickhouse queries
   - [ ] Implement caching strategies
   - [ ] Fine-tune indexing and partitioning

6. Documentation and Deployment
   - [ ] Write API documentation
   - [ ] Create user guide
   - [ ] Set up CI/CD pipeline
   - [ ] Prepare production deployment

## Project Structure

```
/
├── backend/
│   ├── cmd/
│   │   └── server/
│   │       └── main.go
│   ├── internal/
│   │   ├── api/
│   │   ├── db/
│   │   │   └── clickhouse.go
│   │   ├── models/
│   │   └── services/
│   ├── migrations/
│   └── go.mod
├── frontend/
│   ├── src/
│   │   ├── components/
│   │   ├── views/
│   │   ├── store/
│   │   ├── router/
│   │   ├── App.vue
│   │   └── main.js
│   ├── public/
│   └── package.json
├── docker-compose.yml
└── README.md
```

<backend/internal/db/clickhouse.go>
```go
package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

var conn driver.Conn

func InitClickhouse() error {
	var err error
	conn, err = clickhouse.Open(&clickhouse.Options{
		Addr: []string{"localhost:9000"},
		Auth: clickhouse.Auth{
			Database: "logs_db",
			Username: "default",
			Password: "",
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout:          time.Second * 30,
		MaxOpenConns:         5,
		MaxIdleConns:         5,
		ConnMaxLifetime:      time.Hour,
		ConnOpenStrategy:     clickhouse.ConnOpenInOrder,
		BlockBufferSize:      10,
		MaxCompressionBuffer: 10240,
	})

	if err != nil {
		return fmt.Errorf("failed to connect to Clickhouse: %w", err)
	}

	if err := createLogsTable(); err != nil {
		return fmt.Errorf("failed to create logs table: %w", err)
	}

	return nil
}

func createLogsTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS logs (
		ProjectId UUID,
		Timestamp DateTime,
		Level Enum8('debug' = 1, 'info' = 2, 'warn' = 3, 'error' = 4, 'fatal' = 5),
		Message String,
		Attributes Map(String, String),
		UUID UUID
	) ENGINE = MergeTree()
	ORDER BY (ProjectId, Timestamp, UUID)
	SETTINGS index_granularity = 8192
	`

	ctx := context.Background()
	if err := conn.Exec(ctx, query); err != nil {
		return fmt.Errorf("failed to create logs table: %w", err)
	}

	log.Println("Logs table created successfully")
	return nil
}

func GetConnection() driver.Conn {
	return conn
}