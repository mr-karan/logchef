#!/usr/bin/env bash
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

log "Setting up dev user and API token..."

TOKEN="logchef_1_devsetuptoken00000000000000"
TOKEN_HASH="6d86767ef4a9f4fc202e0ae56d2102c3be9b1353c95519c5ed4622c4cf66dc9b"

sqlite3 "$DB_PATH" "INSERT OR REPLACE INTO users (id, email, full_name, role, status) VALUES (1, 'dev@localhost', 'Dev Admin', 'admin', 'active');"
log "User: dev@localhost (admin)"

sqlite3 "$DB_PATH" "INSERT OR IGNORE INTO api_tokens (user_id, name, token_hash, prefix) VALUES (1, 'Dev Setup Token', '$TOKEN_HASH', 'logchef_1_de...');"
log "API token ready"

log "Checking backend status..."
if curl -s "$API_URL/health" > /dev/null 2>&1; then
  log "Backend is running"
else
  log "Backend not running (seeding directly via SQLite)"
fi

log "Creating Dev Team..."
sqlite3 "$DB_PATH" "INSERT OR IGNORE INTO teams (id, name, description) VALUES (1, 'Dev Team', 'Local development team');"
TEAM_ID=1
log "Team: Dev Team (ID: $TEAM_ID)"

log "Setting up sources..."

sqlite3 "$DB_PATH" "DELETE FROM sources WHERE database='default' AND table_name IN ('http', 'syslogs');"

sqlite3 "$DB_PATH" "INSERT INTO sources (name, description, _meta_is_auto_created, _meta_ts_field, host, username, password, database, table_name)
VALUES ('HTTP Access Logs', 'Demo HTTP access logs (vector -c http.toml)', 0, 'timestamp', '127.0.0.1:9000', 'default', '', 'default', 'http');"
HTTP_ID=$(sqlite3 "$DB_PATH" "SELECT id FROM sources WHERE database='default' AND table_name='http';")
log "Source: HTTP Access Logs (ID: $HTTP_ID) -> default.http"

sqlite3 "$DB_PATH" "INSERT INTO sources (name, description, _meta_is_auto_created, _meta_ts_field, _meta_severity_field, host, username, password, database, table_name)
VALUES ('Syslog Logs', 'Demo syslog data (vector -c syslog.toml)', 0, 'timestamp', 'lvl', '127.0.0.1:9000', 'default', '', 'default', 'syslogs');"
SYSLOG_ID=$(sqlite3 "$DB_PATH" "SELECT id FROM sources WHERE database='default' AND table_name='syslogs';")
log "Source: Syslog Logs (ID: $SYSLOG_ID) -> default.syslogs"

log "Linking sources to Dev Team..."
sqlite3 "$DB_PATH" "INSERT OR IGNORE INTO team_sources (team_id, source_id) VALUES ($TEAM_ID, $HTTP_ID);"
sqlite3 "$DB_PATH" "INSERT OR IGNORE INTO team_sources (team_id, source_id) VALUES ($TEAM_ID, $SYSLOG_ID);"

log "Adding dev user to team..."
sqlite3 "$DB_PATH" "INSERT OR IGNORE INTO team_members (team_id, user_id, role) VALUES ($TEAM_ID, 1, 'admin');"

log "Adding admin@logchef.internal to team (if exists)..."
ADMIN_ID=$(sqlite3 "$DB_PATH" "SELECT id FROM users WHERE email='admin@logchef.internal';")
if [ -n "$ADMIN_ID" ]; then
  sqlite3 "$DB_PATH" "INSERT OR IGNORE INTO team_members (team_id, user_id, role) VALUES ($TEAM_ID, $ADMIN_ID, 'admin');"
  log "Admin user added to Dev Team"
fi

log ""
log "=== Seed Complete ==="
log ""
log "Dev environment ready:"
log "  User:    dev@localhost (password: password)"
log "  Team:    Dev Team"
log "  Sources: HTTP Access Logs (default.http)"
log "           Syslog Logs (default.syslogs)"
log ""
log "Next steps:"
log "  1. Start backend: just run-backend"
log "  2. Start frontend: just run-frontend"
log "  3. Ingest logs: just dev-ingest-logs"
log "  4. Open http://localhost:5173"
