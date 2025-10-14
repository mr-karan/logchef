-- Sources

-- name: CreateSource :one
-- Create a new source entry
INSERT INTO sources (
    name, _meta_is_auto_created, _meta_ts_field, _meta_severity_field, host, username, password, database, table_name, description, ttl_days, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
RETURNING id;

-- name: GetSource :one
-- Get a single source by ID
SELECT * FROM sources WHERE id = ?;

-- name: GetSourceByName :one
-- Get a single source by table name and database
SELECT * FROM sources WHERE database = ? AND table_name = ?;

-- name: ListSources :many
-- Get all sources ordered by creation date
SELECT * FROM sources ORDER BY created_at DESC;

-- name: UpdateSource :exec
-- Update an existing source
UPDATE sources
SET name = ?,
    _meta_is_auto_created = ?,
    _meta_ts_field = ?,
    _meta_severity_field = ?,
    host = ?,
    username = ?,
    password = ?,
    database = ?,
    table_name = ?,
    description = ?,
    ttl_days = ?,
    updated_at = datetime('now')
WHERE id = ?;

-- name: DeleteSource :exec
-- Delete a source by ID
DELETE FROM sources WHERE id = ?;

-- Users

-- name: CreateUser :one
-- Create a new user
INSERT INTO users (email, full_name, role, status, last_login_at)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: GetUser :one
-- Get a user by ID
SELECT * FROM users WHERE id = ?;

-- name: GetUserByEmail :one
-- Get a user by email
SELECT * FROM users WHERE email = ?;

-- name: UpdateUser :exec
-- Update a user
UPDATE users
SET email = ?,
    full_name = ?,
    role = ?,
    status = ?,
    last_login_at = ?,
    last_active_at = ?,
    updated_at = ?
WHERE id = ?;

-- name: ListUsers :many
-- List all users
SELECT * FROM users ORDER BY created_at ASC;

-- name: CountAdminUsers :one
-- Count active admin users
SELECT COUNT(*) FROM users WHERE role = ? AND status = ?;

-- name: DeleteUser :exec
-- Delete a user by ID
DELETE FROM users WHERE id = ?;

-- Sessions

-- name: CreateSession :exec
-- Create a new session
INSERT INTO sessions (id, user_id, expires_at, created_at)
VALUES (?, ?, ?, ?);

-- name: GetSession :one
-- Get a session by ID
SELECT * FROM sessions WHERE id = ?;

-- name: DeleteSession :exec
-- Delete a session by ID
DELETE FROM sessions WHERE id = ?;

-- name: DeleteUserSessions :exec
-- Delete all sessions for a user
DELETE FROM sessions WHERE user_id = ?;

-- name: CountUserSessions :one
-- Count active sessions for a user
SELECT COUNT(*) FROM sessions WHERE user_id = ? AND expires_at > ?;

-- Teams

-- name: CreateTeam :one
-- Create a new team
INSERT INTO teams (name, description)
VALUES (?, ?)
RETURNING id;

-- name: GetTeam :one
-- Get a team by ID
SELECT * FROM teams WHERE id = ?;

-- name: UpdateTeam :exec
-- Update a team
UPDATE teams
SET name = ?,
    description = ?,
    updated_at = ?
WHERE id = ?;

-- name: DeleteTeam :exec
-- Delete a team by ID
DELETE FROM teams WHERE id = ?;

-- name: ListTeams :many
-- List all teams
SELECT t.*, COUNT(tm.user_id) as member_count
FROM teams t
LEFT JOIN team_members tm ON t.id = tm.team_id
GROUP BY t.id
ORDER BY t.created_at DESC;

-- Team Members

-- name: AddTeamMember :exec
-- Add a member to a team
INSERT INTO team_members (team_id, user_id, role)
VALUES (?, ?, ?);

-- name: GetTeamMember :one
-- Get a team member
SELECT * FROM team_members WHERE team_id = ? AND user_id = ?;

-- name: UpdateTeamMemberRole :exec
-- Update a team member's role
UPDATE team_members
SET role = ?
WHERE team_id = ? AND user_id = ?;

-- name: RemoveTeamMember :exec
-- Remove a member from a team
DELETE FROM team_members
WHERE team_id = ? AND user_id = ?;

-- name: ListTeamMembers :many
-- List all members of a team
SELECT tm.team_id, tm.user_id, tm.role, tm.created_at
FROM team_members tm
WHERE tm.team_id = ?
ORDER BY tm.created_at;

-- name: ListTeamMembersWithDetails :many
-- List all members of a team with user details
SELECT tm.team_id, tm.user_id, tm.role, tm.created_at, u.email, u.full_name
FROM team_members tm
JOIN users u ON tm.user_id = u.id
WHERE tm.team_id = ?
ORDER BY tm.created_at ASC;

-- name: ListUserTeams :many
-- List all teams a user is a member of
SELECT t.*
FROM teams t
JOIN team_members tm ON t.id = tm.team_id
WHERE tm.user_id = ?
ORDER BY t.name;

-- Team Sources

-- name: AddTeamSource :exec
-- Add a data source to a team
INSERT INTO team_sources (team_id, source_id)
VALUES (?, ?);

-- name: RemoveTeamSource :exec
-- Remove a data source from a team
DELETE FROM team_sources WHERE team_id = ? AND source_id = ?;

-- name: ListTeamSources :many
-- List all data sources in a team
SELECT s.*
FROM sources s
JOIN team_sources ts ON s.id = ts.source_id
WHERE ts.team_id = ?
ORDER BY s.created_at DESC;

-- name: ListSourceTeams :many
-- List all teams a data source is a member of
SELECT t.*
FROM teams t
JOIN team_sources ts ON t.id = ts.team_id
WHERE ts.source_id = ?
ORDER BY t.name;

-- Team Queries

-- name: CreateTeamSourceQuery :one
-- Create a new query for a team and source
INSERT INTO team_queries (team_id, source_id, name, description, query_type, query_content)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetTeamSourceQuery :one
-- Get a query by ID for a specific team and source
SELECT * FROM team_queries
WHERE id = ? AND team_id = ? AND source_id = ?;

-- name: UpdateTeamSourceQuery :exec
-- Update a query for a team and source
UPDATE team_queries
SET name = ?,
    description = ?,
    query_type = ?,
    query_content = ?,
    updated_at = datetime('now')
WHERE id = ? AND team_id = ? AND source_id = ?;

-- name: DeleteTeamSourceQuery :exec
-- Delete a query by ID for a specific team and source
DELETE FROM team_queries
WHERE id = ? AND team_id = ? AND source_id = ?;

-- name: ListQueriesByTeamAndSource :many
-- List all queries for a specific team and source
SELECT * FROM team_queries WHERE team_id = ? AND source_id = ? ORDER BY created_at DESC;

-- Additional queries for user-source and team-source access

-- name: TeamHasSource :one
-- Check if a team has access to a source
SELECT COUNT(*) FROM team_sources
WHERE team_id = ? AND source_id = ?;

-- name: UserHasSourceAccess :one
-- Check if a user has access to a source through any team
SELECT COUNT(*) FROM team_members tm
JOIN team_sources ts ON tm.team_id = ts.team_id
WHERE tm.user_id = ? AND ts.source_id = ?;

-- name: ListTeamsForUser :many
-- List all teams a user is a member of
SELECT
    t.id,
    t.name,
    t.description,
    t.created_at,
    t.updated_at,
    tm.role,  -- The current user's role in this team
    (SELECT COUNT(*) FROM team_members sub_tm WHERE sub_tm.team_id = t.id) as member_count
FROM
    teams t
JOIN
    team_members tm ON t.id = tm.team_id
WHERE
    tm.user_id = ?  -- The current user ID
ORDER BY
    t.created_at DESC;

-- name: GetTeamByName :one
-- Get a team by its name
SELECT * FROM teams WHERE name = ?;

-- name: ListSourcesForUser :many
-- List all sources a user has access to
SELECT DISTINCT s.* FROM sources s
JOIN team_sources ts ON s.id = ts.source_id
JOIN team_members tm ON ts.team_id = tm.team_id
WHERE tm.user_id = ?
ORDER BY s.created_at DESC;

-- API Tokens

-- name: CreateAPIToken :one
-- Create a new API token
INSERT INTO api_tokens (user_id, name, token_hash, prefix, expires_at)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: GetAPIToken :one
-- Get an API token by ID
SELECT * FROM api_tokens WHERE id = ?;

-- name: GetAPITokenByHash :one
-- Get an API token by its hash (for authentication)
SELECT * FROM api_tokens WHERE token_hash = ?;

-- name: ListAPITokensForUser :many
-- List all API tokens for a user
SELECT * FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC;

-- name: UpdateAPITokenLastUsed :exec
-- Update the last used timestamp for an API token
UPDATE api_tokens
SET last_used_at = datetime('now'),
    updated_at = datetime('now')
WHERE id = ?;

-- name: DeleteAPIToken :exec
-- Delete an API token by ID and user ID (ensure user owns the token)
DELETE FROM api_tokens WHERE id = ? AND user_id = ?;

-- name: DeleteExpiredAPITokens :exec
-- Delete all expired API tokens
DELETE FROM api_tokens WHERE expires_at IS NOT NULL AND expires_at < datetime('now');

-- Alerts

-- name: CreateAlert :one
INSERT INTO alerts (
    team_id,
    source_id,
    name,
    description,
    query_type,
    query,
    lookback_seconds,
    threshold_operator,
    threshold_value,
    frequency_seconds,
    severity,
    channels,
    is_active
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetAlert :one
SELECT * FROM alerts WHERE id = ?;

-- name: GetAlertForTeamSource :one
SELECT * FROM alerts WHERE id = ? AND team_id = ? AND source_id = ?;

-- name: ListAlertsByTeamAndSource :many
SELECT * FROM alerts
WHERE team_id = ? AND source_id = ?
ORDER BY created_at DESC;

-- name: UpdateAlert :exec
UPDATE alerts
SET name = ?,
    description = ?,
    query_type = ?,
    query = ?,
    lookback_seconds = ?,
    threshold_operator = ?,
    threshold_value = ?,
    frequency_seconds = ?,
    severity = ?,
    channels = ?,
    is_active = ?,
    updated_at = datetime('now')
WHERE id = ?;

-- name: DeleteAlert :exec
DELETE FROM alerts WHERE id = ?;

-- name: MarkAlertEvaluated :exec
UPDATE alerts
SET last_evaluated_at = datetime('now'),
    updated_at = datetime('now')
WHERE id = ?;

-- name: MarkAlertTriggered :exec
UPDATE alerts
SET last_triggered_at = datetime('now'),
    last_evaluated_at = datetime('now'),
    updated_at = datetime('now')
WHERE id = ?;

-- name: ListActiveAlertsDue :many
SELECT * FROM alerts
WHERE is_active = 1
  AND (
        last_evaluated_at IS NULL
        OR last_evaluated_at <= datetime('now', '-' || frequency_seconds || ' seconds')
      );

-- Alert history queries

-- name: InsertAlertHistory :one
INSERT INTO alert_history (
    alert_id,
    status,
    value_text,
    channels,
    message
)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: ResolveAlertHistory :exec
UPDATE alert_history
SET status = 'resolved',
    resolved_at = datetime('now'),
    message = ?
WHERE id = ?;

-- name: GetLatestUnresolvedAlertHistory :one
SELECT * FROM alert_history
WHERE alert_id = ? AND status = 'triggered'
ORDER BY triggered_at DESC
LIMIT 1;

-- name: ListAlertHistory :many
SELECT * FROM alert_history
WHERE alert_id = ?
ORDER BY triggered_at DESC
LIMIT ?;
