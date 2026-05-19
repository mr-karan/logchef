-- Sources

-- name: CreateSource :one
-- Create a new source entry
INSERT INTO sources (
    name, _meta_is_auto_created, _meta_ts_field, _meta_severity_field, host, username, password, database, table_name, description, ttl_days, tls_enable, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
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
    tls_enable = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?;

-- name: DeleteSource :exec
-- Delete a source by ID
DELETE FROM sources WHERE id = ?;

-- Users

-- name: CreateUser :one
-- Create a new user
INSERT INTO users (email, full_name, role, status, last_login_at, account_type)
VALUES (?, ?, ?, ?, ?, ?)
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

-- name: ListServiceAccounts :many
-- List service principals
SELECT * FROM users WHERE account_type = 'service' ORDER BY created_at ASC;

-- name: CountAdminUsers :one
-- Count active admin users
SELECT COUNT(*) FROM users WHERE role = ? AND status = ?;

-- name: DeleteUser :exec
-- Delete a user by ID
DELETE FROM users WHERE id = ?;

-- User Preferences

-- name: GetUserPreferences :one
-- Get user preferences by user ID
SELECT * FROM user_preferences WHERE user_id = ?;

-- name: UpsertUserPreferences :exec
-- Insert or update user preferences
INSERT INTO user_preferences (user_id, preferences_json, created_at, updated_at)
VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
ON CONFLICT(user_id) DO UPDATE SET
    preferences_json = excluded.preferences_json,
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now');

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
SELECT tm.team_id, tm.user_id, tm.role, tm.created_at, u.email, u.full_name, u.account_type
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

-- Saved Queries (cross-team, source-scoped)

-- name: CreateSavedQuery :one
-- Insert a new saved query and return its id
INSERT INTO saved_queries (source_id, created_from_team_id, name, description, query_type, query_content, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetSavedQuery :one
-- Look up one saved query by id
SELECT * FROM saved_queries WHERE id = ?;

-- name: UpdateSavedQuery :exec
-- Update a saved query's mutable fields
UPDATE saved_queries
SET name = ?,
    description = ?,
    query_type = ?,
    query_content = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?;

-- name: DeleteSavedQuery :exec
-- Delete a saved query
DELETE FROM saved_queries WHERE id = ?;

-- name: ListSavedQueriesForUser :many
-- List every saved query the user can see (any source attached to any of their teams)
SELECT
    sq.id,
    sq.source_id,
    sq.created_from_team_id,
    sq.name,
    sq.description,
    sq.query_type,
    sq.query_content,
    sq.created_at,
    sq.updated_at,
    sq.created_by,
    s.name AS source_name
FROM saved_queries sq
JOIN sources s ON s.id = sq.source_id
WHERE sq.source_id IN (
    SELECT DISTINCT ts.source_id
    FROM team_sources ts
    JOIN team_members tm ON tm.team_id = ts.team_id
    WHERE tm.user_id = ?
)
ORDER BY sq.updated_at DESC;

-- name: ListSavedQueriesForUserBySource :many
-- List saved queries for a specific source, scoped to a user that has access to it
SELECT
    sq.id,
    sq.source_id,
    sq.created_from_team_id,
    sq.name,
    sq.description,
    sq.query_type,
    sq.query_content,
    sq.created_at,
    sq.updated_at,
    sq.created_by,
    s.name AS source_name
FROM saved_queries sq
JOIN sources s ON s.id = sq.source_id
WHERE sq.source_id = ?
  AND EXISTS (
    SELECT 1 FROM team_sources ts
    JOIN team_members tm ON tm.team_id = ts.team_id
    WHERE ts.source_id = sq.source_id AND tm.user_id = ?
  )
ORDER BY sq.updated_at DESC;

-- Query Shares

-- name: CreateQueryShare :exec
-- Persist an ad hoc query share token
INSERT INTO query_shares (
    token,
    source_id,
    team_id,
    created_by,
    payload_json,
    expires_at
) VALUES (?, ?, ?, ?, ?, ?);

-- name: GetQueryShare :one
-- Retrieve an ad hoc query share by token with creator details
SELECT
    qs.token,
    qs.source_id,
    qs.team_id,
    qs.created_by,
    qs.payload_json,
    qs.expires_at,
    qs.last_accessed_at,
    qs.created_at,
    u.email,
    u.full_name
FROM query_shares qs
JOIN users u ON u.id = qs.created_by
WHERE qs.token = ?;

-- name: TouchQueryShare :exec
-- Update a query share's last access time
UPDATE query_shares
SET last_accessed_at = ?
WHERE token = ?;

-- name: DeleteQueryShare :one
-- Delete a query share and return its token
DELETE FROM query_shares
WHERE token = ?
RETURNING token;

-- name: PruneExpiredQueryShares :exec
-- Delete expired query shares
DELETE FROM query_shares
WHERE expires_at < ?;

-- Export Jobs

-- name: CreateExportJob :exec
-- Persist an async export job
INSERT INTO export_jobs (
    id,
    source_id,
    created_by,
    status,
    format,
    request_json,
    expires_at,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetExportJob :one
-- Retrieve an export job by ID
SELECT
    id,
    source_id,
    created_by,
    status,
    format,
    request_json,
    file_name,
    file_path,
    error_message,
    rows_exported,
    bytes_written,
    expires_at,
    completed_at,
    created_at,
    updated_at
FROM export_jobs
WHERE id = ?;

-- name: UpdateExportJobRunning :one
-- Mark an export job as running and return its ID
UPDATE export_jobs
SET
    status = ?,
    error_message = NULL,
    updated_at = ?
WHERE id = ?
RETURNING id;

-- name: CompleteExportJob :one
-- Mark an export job as complete and return its ID
UPDATE export_jobs
SET
    status = ?,
    file_name = ?,
    file_path = ?,
    error_message = NULL,
    rows_exported = ?,
    bytes_written = ?,
    completed_at = ?,
    updated_at = ?
WHERE id = ?
RETURNING id;

-- name: FailExportJob :one
-- Mark an export job as failed and return its ID
UPDATE export_jobs
SET
    status = ?,
    file_name = NULL,
    file_path = NULL,
    error_message = ?,
    completed_at = NULL,
    updated_at = ?
WHERE id = ?
RETURNING id;

-- name: ListExpiredExportJobPaths :many
-- List artifact paths for expired export jobs
SELECT file_path
FROM export_jobs
WHERE expires_at < ?
  AND file_path IS NOT NULL;

-- name: DeleteExpiredExportJobs :exec
-- Delete expired export jobs
DELETE FROM export_jobs
WHERE expires_at < ?;

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

-- name: GetUserTeamForSource :one
-- Get a team ID that the user belongs to and that has access to the source
SELECT tm.team_id FROM team_members tm
JOIN team_sources ts ON tm.team_id = ts.team_id
WHERE tm.user_id = ? AND ts.source_id = ?
LIMIT 1;

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
INSERT INTO api_tokens (user_id, name, token_hash, prefix, expires_at, scopes)
VALUES (?, ?, ?, ?, ?, ?)
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
SET last_used_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?;

-- name: DeleteAPIToken :exec
-- Delete an API token by ID and user ID (ensure user owns the token)
DELETE FROM api_tokens WHERE id = ? AND user_id = ?;

-- name: DeleteExpiredAPITokens :exec
-- Delete all expired API tokens
DELETE FROM api_tokens WHERE expires_at IS NOT NULL AND expires_at < strftime('%Y-%m-%dT%H:%M:%SZ', 'now');

-- Alerts

-- name: CreateAlert :one
INSERT INTO alerts (
    source_id,
    name,
    description,
    query_type,
    query,
    condition_json,
    lookback_seconds,
    threshold_operator,
    threshold_value,
    frequency_seconds,
    severity,
    labels_json,
    annotations_json,
    recipient_user_ids_json,
    webhook_urls_json,
    generator_url,
    is_active,
    created_by
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAlert :one
SELECT * FROM alerts WHERE id = ?;

-- name: ListAlertsBySource :many
-- List alerts for one source
SELECT * FROM alerts
WHERE source_id = ?
ORDER BY updated_at DESC, created_at DESC;

-- name: ListAlertsForUser :many
-- List every alert the user can see (any source attached to any of their teams)
SELECT a.* FROM alerts a
WHERE a.source_id IN (
    SELECT DISTINCT ts.source_id
    FROM team_sources ts
    JOIN team_members tm ON tm.team_id = ts.team_id
    WHERE tm.user_id = ?
)
ORDER BY a.updated_at DESC, a.created_at DESC;

-- name: UpdateAlert :one
UPDATE alerts
SET name = ?,
    description = ?,
    query_type = ?,
    query = ?,
    condition_json = ?,
    lookback_seconds = ?,
    threshold_operator = ?,
    threshold_value = ?,
    frequency_seconds = ?,
    severity = ?,
    labels_json = ?,
    annotations_json = ?,
    recipient_user_ids_json = ?,
    webhook_urls_json = ?,
    generator_url = ?,
    is_active = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?
RETURNING id;

-- name: DeleteAlert :one
DELETE FROM alerts WHERE id = ?
RETURNING id;

-- name: MarkAlertEvaluated :exec
UPDATE alerts
SET last_state = 'resolved',
    last_evaluated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?;

-- name: MarkAlertTriggered :exec
UPDATE alerts
SET last_state = 'firing',
    last_triggered_at = CASE WHEN last_state = 'firing' THEN last_triggered_at ELSE strftime('%Y-%m-%dT%H:%M:%SZ', 'now') END,
    last_evaluated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
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
    value,
    message,
    payload_json
)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ResolveAlertHistory :one
UPDATE alert_history
SET status = 'resolved',
    resolved_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
    message = ?
WHERE id = ?
RETURNING id;

-- name: UpdateAlertHistoryPayload :one
UPDATE alert_history
SET payload_json = ?
WHERE id = ?
RETURNING id;

-- name: GetLatestUnresolvedAlertHistory :one
SELECT * FROM alert_history
WHERE alert_id = ? AND status = 'triggered'
ORDER BY triggered_at DESC, id DESC
LIMIT 1;

-- name: ListAlertHistory :many
SELECT * FROM alert_history
WHERE alert_id = ?
ORDER BY triggered_at DESC, id DESC
LIMIT ?;

-- name: PruneAlertHistory :exec
DELETE FROM alert_history AS target
WHERE target.alert_id = ?
  AND target.id NOT IN (
    SELECT keep.id
    FROM alert_history AS keep
    WHERE keep.alert_id = ?
    ORDER BY keep.triggered_at DESC, keep.id DESC
    LIMIT ?
 );

-- System Settings Queries

-- name: GetSystemSetting :one
SELECT * FROM system_settings
WHERE key = ?;

-- name: ListSystemSettings :many
SELECT * FROM system_settings
ORDER BY category, key;

-- name: ListSystemSettingsByCategory :many
SELECT * FROM system_settings
WHERE category = ?
ORDER BY key;

-- name: UpsertSystemSetting :exec
INSERT INTO system_settings (key, value, value_type, category, description, is_sensitive, updated_at)
VALUES (?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
ON CONFLICT(key) DO UPDATE SET
    value = excluded.value,
    value_type = excluded.value_type,
    description = excluded.description,
    is_sensitive = excluded.is_sensitive,
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now');

-- name: DeleteSystemSetting :exec
DELETE FROM system_settings
WHERE key = ?;

-- Provisioning Queries

-- name: ListManagedSources :many
-- Get all sources managed by provisioning config
SELECT * FROM sources WHERE managed = 1 ORDER BY id;

-- name: ListManagedTeams :many
-- Get all teams managed by provisioning config
SELECT * FROM teams WHERE managed = 1 ORDER BY id;

-- name: ListManagedUsers :many
-- Get all users managed by provisioning config
SELECT * FROM users WHERE managed = 1 ORDER BY id;

-- name: SetSourceManaged :exec
-- Mark a source as managed/unmanaged and set secret_ref
UPDATE sources SET managed = ?, secret_ref = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: SetTeamManaged :exec
-- Mark a team as managed/unmanaged
UPDATE teams SET managed = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: SetUserManaged :exec
-- Mark a user as managed/unmanaged
UPDATE users SET managed = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: IsSourceManaged :one
-- Check if a source is managed
SELECT managed FROM sources WHERE id = ?;

-- name: IsTeamManaged :one
-- Check if a team is managed
SELECT managed FROM teams WHERE id = ?;

-- name: IsUserManaged :one
-- Check if a user is managed
SELECT managed FROM users WHERE id = ?;

-- name: GetSourceByNameForProvisioning :one
-- Get source by name for provisioning lookup
SELECT * FROM sources WHERE name = ?;


-- Collections (cross-team curation lists for saved queries)

-- name: CreateCollection :one
-- Insert a new collection (personal or shared)
INSERT INTO collections (name, description, is_personal, created_by)
VALUES (?, ?, ?, ?)
RETURNING id, created_at, updated_at;

-- name: GetCollection :one
-- Look up a collection by id
SELECT * FROM collections WHERE id = ?;

-- name: GetPersonalCollection :one
-- Find the caller's personal collection if it exists
SELECT * FROM collections WHERE created_by = ? AND is_personal = 1;

-- name: UpdateCollection :exec
-- Update name/description (owner only - enforced in app code)
UPDATE collections
SET name = ?,
    description = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?;

-- name: DeleteCollection :exec
-- Delete a collection. Personal collections cannot be deleted (enforced in app code).
DELETE FROM collections WHERE id = ?;

-- name: ListCollectionsForUser :many
-- List collections the user owns or is a member of, with member count and item count
SELECT
    c.id,
    c.name,
    c.description,
    c.is_personal,
    c.created_by,
    c.created_at,
    c.updated_at,
    cm.role AS caller_role,
    (SELECT COUNT(*) FROM collection_members WHERE collection_id = c.id) AS member_count,
    (SELECT COUNT(*) FROM collection_items WHERE collection_id = c.id) AS item_count
FROM collections c
JOIN collection_members cm ON cm.collection_id = c.id
WHERE cm.user_id = ?
ORDER BY c.is_personal DESC, c.updated_at DESC;

-- name: AddCollectionMember :exec
-- Add a member; idempotent on (collection_id, user_id).
INSERT INTO collection_members (collection_id, user_id, role, added_by)
VALUES (?, ?, ?, ?)
ON CONFLICT(collection_id, user_id) DO NOTHING;

-- name: GetCollectionMember :one
-- Look up a single membership row
SELECT collection_id, user_id, role, added_by, created_at
FROM collection_members
WHERE collection_id = ? AND user_id = ?;

-- name: ListCollectionMembers :many
-- List members of a collection with user details
SELECT cm.collection_id, cm.user_id, cm.role, cm.added_by, cm.created_at,
       u.email, u.full_name
FROM collection_members cm
JOIN users u ON u.id = cm.user_id
WHERE cm.collection_id = ?
ORDER BY cm.role DESC, u.email ASC;

-- name: RemoveCollectionMember :exec
-- Remove a member from a collection
DELETE FROM collection_members WHERE collection_id = ? AND user_id = ?;

-- name: AddCollectionItem :exec
-- Add a saved query to a collection; idempotent on (collection_id, saved_query_id).
INSERT INTO collection_items (collection_id, saved_query_id, sort_order, added_by)
VALUES (?, ?, ?, ?)
ON CONFLICT(collection_id, saved_query_id) DO NOTHING;

-- name: RemoveCollectionItem :exec
-- Remove an item from a collection
DELETE FROM collection_items WHERE collection_id = ? AND saved_query_id = ?;

-- name: ListCollectionItems :many
-- List items in a collection with saved-query details
SELECT
    ci.collection_id,
    ci.saved_query_id,
    ci.sort_order,
    ci.added_by,
    ci.created_at AS item_added_at,
    sq.id AS query_id,
    sq.source_id,
    sq.name AS query_name,
    sq.description AS query_description,
    sq.query_type,
    sq.query_content,
    sq.created_by AS query_created_by,
    sq.created_at AS query_created_at,
    sq.updated_at AS query_updated_at,
    s.name AS source_name
FROM collection_items ci
JOIN saved_queries sq ON sq.id = ci.saved_query_id
JOIN sources s ON s.id = sq.source_id
WHERE ci.collection_id = ?
ORDER BY ci.sort_order ASC, ci.created_at ASC;
