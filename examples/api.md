# LogChef API Examples

## Logs API

### Query Logs
```bash
# Basic query with pagination
curl -X GET "http://localhost:8080/api/logs/{sourceId}?limit=50&offset=0"

# Query with time range
curl -X GET "http://localhost:8080/api/logs/{sourceId}?start_time=2024-01-01T00:00:00Z&end_time=2024-01-02T00:00:00Z"

# Query with filters
curl -X GET "http://localhost:8080/api/logs/{sourceId}?service_name=api-server&namespace=prod&severity_text=error"

# Full-text search
curl -X GET "http://localhost:8080/api/logs/{sourceId}?q=connection+refused"
```

Response:
```json
{
  "status": "success",
  "data": {
    "logs": [
      {
        "id": "123e4567-e89b-12d3-a456-426614174000",
        "timestamp": "2024-01-01T12:00:00Z",
        "trace_id": "abc123",
        "span_id": "def456",
        "trace_flags": 1,
        "severity_text": "error",
        "severity_number": 17,
        "service_name": "api-server",
        "namespace": "prod",
        "body": "connection refused",
        "log_attributes": {
          "host": "server-1",
          "pid": "1234"
        }
      }
    ],
    "total_count": 150,
    "has_more": true
  }
}
```

## Sources API

### Create Source
```bash
curl -X POST http://localhost:8080/api/sources \
  -H "Content-Type: application/json" \
  -d '{
    "name": "nginx-logs",
    "schema_type": "http",
    "ttl_days": 30,
    "dsn": "clickhouse://localhost:9000/logs?username=default&password=password"
  }'
```

### Get Source
```bash
curl -X GET http://localhost:8080/api/sources/{id}
```

### Update Source
```bash
curl -X PUT http://localhost:8080/api/sources/{id} \
  -H "Content-Type: application/json" \
  -d '{
    "name": "nginx-logs-updated",
    "schema_type": "http"
  }'
```

### Delete Source
```bash
curl -X DELETE http://localhost:8080/api/sources/{id}
```

Note: Replace `{id}` with the actual UUID returned from the create operation.
