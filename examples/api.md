# LogChef API Examples

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
