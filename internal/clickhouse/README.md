# ClickHouse Package

This package provides a clean, simple interface for managing connections to ClickHouse databases and executing queries.

## Overview

The package is designed with simplicity and maintainability in mind, following Go best practices. It provides:

1. Connection management for multiple ClickHouse sources
2. Query execution with proper context handling
3. Time series and log context query support
4. Query building with basic SQL injection protection

## Components

### Manager

The `Manager` handles multiple ClickHouse connections:

```go
// Create a new manager
manager := clickhouse.NewManager(logger)

// Add a source
err := manager.AddSource(source)

// Get a connection
client, err := manager.GetConnection(sourceID)

// Execute a log query
result, err := manager.QueryLogs(ctx, sourceID, params)

// Get time series data
timeSeries, err := manager.GetTimeSeries(ctx, sourceID, params)

// Get log context
context, err := manager.GetLogContext(ctx, sourceID, params)

// Close all connections
err := manager.Close()
```

### Client

The `Client` represents a single connection to a ClickHouse database:

```go
// Create a new client
client, err := clickhouse.NewClient(opts, logger)

// Execute a query
result, err := client.Query(ctx, query)

// Check health
err := client.CheckHealth(ctx)

// Close the connection
err := client.Close()
```

### QueryBuilder

The `QueryBuilder` helps construct valid ClickHouse queries:

```go
// Create a new query builder
qb := clickhouse.NewQueryBuilder(tableName)

// Build a raw query
query, err := qb.BuildRawQuery(rawSQL, limit)

// Build a time series query
query := qb.BuildTimeSeriesQuery(startTime, endTime, interval)

// Build log context queries
before, target, after := qb.BuildLogContextQueries(targetTime, beforeLimit, afterLimit)
```

## Error Handling

The package provides specific error types for common failure scenarios:

```go
if errors.Is(err, clickhouse.ErrSourceNotConnected) {
    // Handle connection error
}

if errors.Is(err, clickhouse.ErrInvalidQuery) {
    // Handle invalid query
}
```

## Usage Example

```go
// Create a manager
manager := clickhouse.NewManager(logger)

// Add a source
source := &models.Source{
    ID: "source1",
    Connection: models.ConnectionInfo{
        Host:     "localhost:9000",
        Database: "logs_db",
        TableName: "logs",
    },
}
if err := manager.AddSource(source); err != nil {
    log.Fatalf("Failed to add source: %v", err)
}

// Execute a query
ctx := context.Background()
params := clickhouse.LogQueryParams{
    RawSQL: "SELECT * FROM logs WHERE level = 'error'",
    Limit:  100,
}
result, err := manager.QueryLogs(ctx, "source1", params)
if err != nil {
    log.Fatalf("Query failed: %v", err)
}

// Process results
for _, row := range result.Data {
    fmt.Printf("Log: %v\n", row)
}

// Close connections when done
defer manager.Close()
```
