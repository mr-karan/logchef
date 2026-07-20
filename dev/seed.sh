#!/usr/bin/env bash
set -e

DB_PATH="${LOGCHEF_DB_PATH:-../local.db}"

log() {
  echo "$(date '+%Y-%m-%d %H:%M:%S') | $1"
}

log "=== LogChef Dev Environment Seed ==="

if ! command -v sqlite3 &> /dev/null; then
  log "ERROR: sqlite3 is required. Install it first."
  exit 1
fi

if [ ! -f "$DB_PATH" ]; then
  log "ERROR: Database not found at $DB_PATH"
  log "Make sure LogChef backend has started at least once to create the database."
  exit 1
fi

log "Setting up dev API user and token..."

TOKEN="logchef_1_devsetuptoken00000000000000"
TOKEN_HASH="6d86767ef4a9f4fc202e0ae56d2102c3be9b1353c95519c5ed4622c4cf66dc9b"

sqlite3 "$DB_PATH" "INSERT OR IGNORE INTO users (email, full_name, role, status) VALUES ('dev@localhost', 'Dev Admin', 'admin', 'active');"
sqlite3 "$DB_PATH" "UPDATE users SET full_name='Dev Admin', role='admin', status='active', updated_at=strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE email='dev@localhost';"
DEV_USER_ID=$(sqlite3 "$DB_PATH" "SELECT id FROM users WHERE email='dev@localhost';")

if [ -z "$DEV_USER_ID" ]; then
  log "ERROR: Failed to resolve dev user ID after insert/update."
  exit 1
fi

log "User: dev@localhost (admin)"

sqlite3 "$DB_PATH" "INSERT OR IGNORE INTO api_tokens (user_id, name, token_hash, prefix) VALUES ($DEV_USER_ID, 'Dev Setup Token', '$TOKEN_HASH', 'logchef_1_de...');"
log "API token ready"

TEAM_ID=$(sqlite3 "$DB_PATH" "SELECT id FROM teams WHERE name='Dev Team';")
if [ -n "$TEAM_ID" ]; then
  log "Linking dev user to provisioned Dev Team..."
  sqlite3 "$DB_PATH" "INSERT OR IGNORE INTO team_members (team_id, user_id, role) VALUES ($TEAM_ID, $DEV_USER_ID, 'admin');"
  log "Team: Dev Team (ID: $TEAM_ID)"
else
  log "Dev Team not found yet. Start the backend once so provisioning can create it, then re-run just dev-seed if you need the local API user linked."
fi

log ""
log "=== Seed Complete ==="
log ""
log "Dev environment ready:"
log "  Login:   admin@logchef.internal / password (via Dex OIDC)"
log "  API User: dev@localhost"
log "  Token:    $TOKEN"
log ""
log "Next steps:"
log "  1. Start backend: just run-backend"
log "  2. Provisioning will create Dev Team and datasources from dev/provisioning.toml"
log "  3. Start frontend: just run-frontend"
log "  4. Ingest logs: just dev-ingest-logs"
log "  5. Open http://localhost:5173"
