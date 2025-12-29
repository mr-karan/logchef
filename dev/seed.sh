#!/bin/bash
set -e

API_URL="${LOGCHEF_API_URL:-http://localhost:8125/api/v1}"
DB_PATH="${LOGCHEF_DB_PATH:-../local.db}"

log() {
  echo "$(date '+%Y-%m-%d %H:%M:%S') | $1"
}

log "=== LogChef Dev Environment Seed ==="

if ! command -v sqlite3 &> /dev/null; then
  log "ERROR: sqlite3 is required. Install it first."
  exit 1
fi

if ! command -v curl &> /dev/null; then
  log "ERROR: curl is required. Install it first."
  exit 1
fi

if [ ! -f "$DB_PATH" ]; then
  log "ERROR: Database not found at $DB_PATH"
  log "Make sure LogChef backend has started at least once to create the database."
  exit 1
fi

log "Bootstrapping admin user and API token..."

TOKEN="logchef_1_devsetuptoken00000000000000"
TOKEN_HASH="6d86767ef4a9f4fc202e0ae56d2102c3be9b1353c95519c5ed4622c4cf66dc9b"

sqlite3 "$DB_PATH" "INSERT OR IGNORE INTO users (id, email, full_name, role, status) VALUES (1, 'dev@localhost', 'Dev Admin', 'admin', 'active');"
log "Created user: dev@localhost"

sqlite3 "$DB_PATH" "INSERT OR IGNORE INTO api_tokens (user_id, name, token_hash, prefix) VALUES (1, 'Dev Setup Token', '$TOKEN_HASH', 'logchef_1_de...');"
log "Created API token"

log "Waiting for backend to be ready..."
for i in {1..30}; do
  if curl -s "$API_URL/health" > /dev/null 2>&1; then
    break
  fi
  sleep 1
done

TEAM_ID=$(sqlite3 "$DB_PATH" "SELECT id FROM teams WHERE name='Dev Team' LIMIT 1;" 2>/dev/null)
if [ -n "$TEAM_ID" ]; then
  log "Dev Team already exists with ID: $TEAM_ID"
else
  log "Creating Dev Team..."
  TEAM_RESP=$(curl -s -X POST "$API_URL/admin/teams" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name": "Dev Team", "description": "Local development team"}')
  TEAM_ID=$(echo "$TEAM_RESP" | grep -o '"id":[0-9]*' | head -1 | cut -d':' -f2)

  if [ -z "$TEAM_ID" ]; then
    log "Failed to create team: $TEAM_RESP"
    exit 1
  fi
  log "Created team with ID: $TEAM_ID"
fi

HTTP_EXISTS=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM sources WHERE name='HTTP Access Logs';" 2>/dev/null || echo "0")
if [ "$HTTP_EXISTS" -gt 0 ]; then
  log "HTTP source already exists"
  HTTP_ID=$(sqlite3 "$DB_PATH" "SELECT id FROM sources WHERE name='HTTP Access Logs' LIMIT 1;")
else
  log "Creating HTTP Logs source..."
  HTTP_RESP=$(curl -s -X POST "$API_URL/admin/sources" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "HTTP Access Logs",
    "description": "Demo HTTP access logs (vector -c http.toml)",
    "meta_ts_field": "timestamp",
    "connection": {
      "host": "localhost:9000",
      "database": "default",
      "table_name": "http"
    }
  }')
HTTP_ID=$(echo "$HTTP_RESP" | grep -o '"id":[0-9]*' | head -1 | cut -d':' -f2)

  if [ -n "$HTTP_ID" ]; then
    log "Created HTTP source with ID: $HTTP_ID"
    curl -s -X POST "$API_URL/teams/$TEAM_ID/sources" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d "{\"source_id\": $HTTP_ID}" > /dev/null
    log "Linked HTTP source to team"
  else
    log "Failed to create HTTP source: $HTTP_RESP"
  fi
fi

SYSLOG_EXISTS=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM sources WHERE name='Syslog Logs';" 2>/dev/null || echo "0")
if [ "$SYSLOG_EXISTS" -gt 0 ]; then
  log "Syslog source already exists"
  SYSLOG_ID=$(sqlite3 "$DB_PATH" "SELECT id FROM sources WHERE name='Syslog Logs' LIMIT 1;")
else
  log "Creating Syslog source..."
SYSLOG_RESP=$(curl -s -X POST "$API_URL/admin/sources" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Syslog Logs",
    "description": "Demo syslog data (vector -c syslog.toml)",
    "meta_ts_field": "timestamp",
    "meta_severity_field": "lvl",
    "connection": {
      "host": "localhost:9000",
      "database": "default",
      "table_name": "syslogs"
    }
  }')
SYSLOG_ID=$(echo "$SYSLOG_RESP" | grep -o '"id":[0-9]*' | head -1 | cut -d':' -f2)

  if [ -n "$SYSLOG_ID" ]; then
    log "Created Syslog source with ID: $SYSLOG_ID"
    curl -s -X POST "$API_URL/teams/$TEAM_ID/sources" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d "{\"source_id\": $SYSLOG_ID}" > /dev/null
    log "Linked Syslog source to team"
  else
    log "Failed to create Syslog source: $SYSLOG_RESP"
  fi
fi

VL_EXISTS=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM sources WHERE name='VictoriaLogs Demo';" 2>/dev/null || echo "0")
if [ "$VL_EXISTS" -gt 0 ]; then
  log "VictoriaLogs source already exists"
  VL_ID=$(sqlite3 "$DB_PATH" "SELECT id FROM sources WHERE name='VictoriaLogs Demo' LIMIT 1;")
else
  log "Creating VictoriaLogs source..."
VL_RESP=$(curl -s -X POST "$API_URL/admin/sources" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "VictoriaLogs Demo",
    "description": "Demo VictoriaLogs data (vector -c victorialogs.toml)",
    "backend_type": "victorialogs",
    "meta_ts_field": "_time",
    "victorialogs_connection": {
      "url": "http://localhost:9428"
    }
  }')
VL_ID=$(echo "$VL_RESP" | grep -o '"id":[0-9]*' | head -1 | cut -d':' -f2)

  if [ -n "$VL_ID" ]; then
    log "Created VictoriaLogs source with ID: $VL_ID"
    curl -s -X POST "$API_URL/teams/$TEAM_ID/sources" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d "{\"source_id\": $VL_ID}" > /dev/null
    log "Linked VictoriaLogs source to team"
  else
    log "Failed to create VictoriaLogs source: $VL_RESP"
  fi
fi

log "Adding dev user to team..."
curl -s -X POST "$API_URL/teams/$TEAM_ID/members" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"user_id": 1, "role": "admin"}' > /dev/null

log "=== Seed Complete ==="
log "Dev Team created with HTTP, Syslog, and VictoriaLogs sources."
log "Login with dev@localhost (password: password) to explore!"
