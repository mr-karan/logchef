-- Sources

-- name: CreateSource :one
-- Create a new source entry
INSERT INTO sources (
    name, _meta_is_auto_created, source_type, _meta_ts_field, _meta_severity_field, connection_config, identity_key, description, ttl_days, managed, secret_ref, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now(), now())
RETURNING id;

-- name: GetSource :one
-- Get a single source by ID
SELECT * FROM sources WHERE id = $1;

-- name: GetSourceByIdentityKey :one
-- Get a single source by provider-computed identity key
SELECT * FROM sources WHERE identity_key = $1;

-- name: ListSources :many
-- Get all sources ordered by creation date
SELECT * FROM sources ORDER BY created_at DESC;

-- name: UpdateSource :exec
-- Update an existing source
UPDATE sources
SET name = $1,
    _meta_is_auto_created = $2,
    source_type = $3,
    _meta_ts_field = $4,
    _meta_severity_field = $5,
    connection_config = $6,
    identity_key = $7,
    description = $8,
    ttl_days = $9,
    managed = $10,
    secret_ref = $11,
    updated_at = now()
WHERE id = $12;

-- name: DeleteSource :exec
-- Delete a source by ID
DELETE FROM sources WHERE id = $1;

-- Users

-- name: CreateUser :one
-- Create a new user
INSERT INTO users (email, full_name, role, status, last_login_at, account_type)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: GetUser :one
-- Get a user by ID
SELECT * FROM users WHERE id = $1;

-- name: GetUserForUpdate :one
-- Get a user by ID and lock the row until the transaction ends
SELECT * FROM users WHERE id = $1 FOR NO KEY UPDATE;

-- name: GetUserByEmail :one
-- Get a user by email
SELECT * FROM users WHERE email = $1;

-- name: SetUserPasswordHash :exec
-- Set (or clear) a user's local-auth bcrypt hash
UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2;

-- name: UpdateUser :exec
-- Update a user
UPDATE users
SET email = $1,
    full_name = $2,
    role = $3,
    status = $4,
    last_login_at = $5,
    last_active_at = $6,
    updated_at = $7
WHERE id = $8;

-- name: UpdateUserLastLogin :exec
-- Record a login without rewriting the rest of the user row
UPDATE users SET last_login_at = $2, updated_at = now() WHERE id = $1;

-- name: ListUsers :many
-- List all users
SELECT * FROM users ORDER BY created_at ASC;

-- name: ListServiceAccounts :many
-- List service principals
SELECT * FROM users WHERE account_type = 'service' ORDER BY created_at ASC;

-- name: CountAdminUsers :one
-- Count active admin users
SELECT COUNT(*) FROM users WHERE role = $1 AND status = $2;

-- name: LockMembershipWrites :exec
-- Hold a transaction-scoped advisory lock
SELECT pg_advisory_xact_lock($1);

-- name: DeleteUser :exec
-- Delete a user by ID
DELETE FROM users WHERE id = $1;

-- User Preferences

-- name: GetUserPreferences :one
-- Get user preferences by user ID
SELECT * FROM user_preferences WHERE user_id = $1;

-- name: UpsertUserPreferences :exec
-- Insert or update user preferences
INSERT INTO user_preferences (user_id, preferences_json, created_at, updated_at)
VALUES ($1, $2, now(), now())
ON CONFLICT(user_id) DO UPDATE SET
    preferences_json = excluded.preferences_json,
    updated_at = now();

-- Sessions

-- name: CreateSession :exec
-- Create a new session
INSERT INTO sessions (id, user_id, expires_at, created_at)
VALUES ($1, $2, $3, $4);

-- name: GetSession :one
-- Get a session by ID
SELECT * FROM sessions WHERE id = $1;

-- name: DeleteSession :exec
-- Delete a session by ID
DELETE FROM sessions WHERE id = $1;

-- name: DeleteUserSessions :exec
-- Delete all sessions for a user
DELETE FROM sessions WHERE user_id = $1;

-- name: CountUserSessions :one
-- Count active sessions for a user
SELECT COUNT(*) FROM sessions WHERE user_id = $1 AND expires_at > $2;

-- name: DeleteExpiredSessions :exec
-- Delete all sessions whose expiry is at or before the given time
DELETE FROM sessions WHERE expires_at <= $1;

-- Teams

-- name: CreateTeam :one
-- Create a new team
INSERT INTO teams (name, description)
VALUES ($1, $2)
RETURNING id;

-- name: GetTeam :one
-- Get a team by ID
SELECT * FROM teams WHERE id = $1;

-- name: UpdateTeam :exec
-- Update a team
UPDATE teams
SET name = $1,
    description = $2,
    updated_at = $3
WHERE id = $4;

-- name: DeleteTeam :exec
-- Delete a team by ID
DELETE FROM teams WHERE id = $1;

-- name: ListTeams :many
-- List all teams
SELECT t.*, COUNT(tm.user_id) as member_count
FROM teams t
LEFT JOIN team_members tm ON t.id = tm.team_id
GROUP BY t.id
ORDER BY t.created_at DESC;

-- Team Members

-- name: AddTeamMember :exec
-- Add a member to a team; adding an existing member is a no-op and keeps their role.
INSERT INTO team_members (team_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (team_id, user_id) DO NOTHING;

-- name: GetTeamMember :one
-- Get a team member
SELECT * FROM team_members WHERE team_id = $1 AND user_id = $2;

-- name: UpdateTeamMemberRole :exec
-- Update a team member's role
UPDATE team_members
SET role = $1
WHERE team_id = $2 AND user_id = $3;

-- name: RemoveTeamMember :exec
-- Remove a member from a team
DELETE FROM team_members
WHERE team_id = $1 AND user_id = $2;

-- name: ListTeamMembersWithDetails :many
-- List all members of a team with user details
SELECT tm.team_id, tm.user_id, tm.role, tm.created_at, u.email, u.full_name, u.account_type
FROM team_members tm
JOIN users u ON tm.user_id = u.id
WHERE tm.team_id = $1
ORDER BY tm.created_at ASC;

-- name: ListUserTeams :many
-- List all teams a user is a member of
SELECT t.*
FROM teams t
JOIN team_members tm ON t.id = tm.team_id
WHERE tm.user_id = $1
ORDER BY t.name;

-- Team Sources

-- name: AddTeamSource :exec
-- Add a data source to a team; linking an already linked source is a no-op.
INSERT INTO team_sources (team_id, source_id)
VALUES ($1, $2)
ON CONFLICT (team_id, source_id) DO NOTHING;

-- name: RemoveTeamSource :exec
-- Remove a data source from a team
DELETE FROM team_sources WHERE team_id = $1 AND source_id = $2;

-- name: ListTeamSources :many
-- List all data sources in a team
SELECT s.*
FROM sources s
JOIN team_sources ts ON s.id = ts.source_id
WHERE ts.team_id = $1
ORDER BY s.created_at DESC;

-- name: ListSourceTeams :many
-- List all teams a data source is a member of
SELECT t.*
FROM teams t
JOIN team_sources ts ON t.id = ts.team_id
WHERE ts.source_id = $1
ORDER BY t.name;

-- Saved Queries (cross-team, source-scoped)

-- name: CreateSavedQuery :one
-- Insert a new saved query and return its id
INSERT INTO saved_queries (source_id, created_from_team_id, name, description, query_language, editor_mode, query_content, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id;

-- name: GetSavedQuery :one
-- Look up one saved query by id
SELECT * FROM saved_queries WHERE id = $1;

-- name: UpdateSavedQuery :exec
-- Update a saved query's mutable fields
UPDATE saved_queries
SET name = $1,
    description = $2,
    query_language = $3,
    editor_mode = $4,
    query_content = $5,
    updated_at = now()
WHERE id = $6;

-- name: DeleteSavedQuery :exec
-- Delete a saved query
DELETE FROM saved_queries WHERE id = $1;

-- name: ListSavedQueriesForUser :many
-- List every saved query the user can see (any source attached to any of their teams)
SELECT
    sq.id,
    sq.source_id,
    sq.created_from_team_id,
    sq.name,
    sq.description,
    sq.query_language,
    sq.editor_mode,
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
    WHERE tm.user_id = $1
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
    sq.query_language,
    sq.editor_mode,
    sq.query_content,
    sq.created_at,
    sq.updated_at,
    sq.created_by,
    s.name AS source_name
FROM saved_queries sq
JOIN sources s ON s.id = sq.source_id
WHERE sq.source_id = $1
  AND EXISTS (
    SELECT 1 FROM team_sources ts
    JOIN team_members tm ON tm.team_id = ts.team_id
    WHERE ts.source_id = sq.source_id AND tm.user_id = $2
  )
ORDER BY sq.updated_at DESC;

-- name: ListAllSavedQueries :many
-- List every saved query without a source-access gate. This is only for the
-- global-admin browse surface; callers must authorize before invoking it.
SELECT
    sq.id,
    sq.source_id,
    sq.created_from_team_id,
    sq.name,
    sq.description,
    sq.query_language,
    sq.editor_mode,
    sq.query_content,
    sq.created_at,
    sq.updated_at,
    sq.created_by,
    s.name AS source_name
FROM saved_queries sq
JOIN sources s ON s.id = sq.source_id
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
) VALUES ($1, $2, $3, $4, $5, $6);

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
WHERE qs.token = $1;

-- name: TouchQueryShare :exec
-- Update a query share's last access time
UPDATE query_shares
SET last_accessed_at = $1
WHERE token = $2;

-- name: DeleteQueryShare :one
-- Delete a query share and return its token
DELETE FROM query_shares
WHERE token = $1
RETURNING token;

-- name: PruneExpiredQueryShares :exec
-- Delete expired query shares
DELETE FROM query_shares
WHERE expires_at < $1;

-- Export Jobs

-- name: CreateExportJob :exec
-- Persist an async export job
INSERT INTO export_jobs (
    id,
    owner_instance_id,
    owner_process_id,
    source_id,
    created_by,
    status,
    format,
    request_json,
    expires_at,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: GetExportJob :one
-- Retrieve an export job by ID
SELECT id, source_id, created_by, status, format, request_json, file_name, file_path, error_message, rows_exported, bytes_written, expires_at, completed_at, created_at, updated_at, owner_instance_id, owner_process_id
FROM export_jobs
WHERE id = $1;

-- name: UpdateExportJobRunning :one
-- Mark an export job as running and return its ID
UPDATE export_jobs
SET
    status = $1,
    error_message = NULL,
    updated_at = now()
WHERE id = $2
  AND status IN ('pending', 'running')
RETURNING id;

-- name: CompleteExportJob :one
-- Mark an export job as complete and return its ID
UPDATE export_jobs
SET
    status = $1,
    file_name = $2,
    file_path = $3,
    error_message = NULL,
    rows_exported = $4,
    bytes_written = $5,
    completed_at = $6,
    updated_at = $7
WHERE id = $8
  AND status IN ('pending', 'running')
RETURNING id;

-- name: FailExportJob :one
-- Mark an export job as failed and return its ID
UPDATE export_jobs
SET
    status = $1,
    file_name = NULL,
    file_path = NULL,
    error_message = $2,
    completed_at = NULL,
    updated_at = $3
WHERE id = $4
RETURNING id;

-- name: ListExpiredExportJobPaths :many
-- List artifact paths for expired export jobs
SELECT file_path
FROM export_jobs
WHERE expires_at < $1
  AND file_path IS NOT NULL;

-- name: DeleteExpiredExportJobs :exec
-- Delete expired export jobs
DELETE FROM export_jobs
WHERE expires_at < $1;

-- Additional queries for user-source and team-source access

-- name: TeamHasSource :one
-- Check if a team has access to a source
SELECT EXISTS(
    SELECT 1 FROM team_sources
    WHERE team_id = $1 AND source_id = $2
);

-- name: UserHasSourceAccess :one
-- Check if a user has access to a source through any team
SELECT EXISTS(
    SELECT 1 FROM team_members tm
    JOIN team_sources ts ON tm.team_id = ts.team_id
    WHERE tm.user_id = $1 AND ts.source_id = $2
);

-- name: GetUserTeamForSource :one
-- Get a team ID that the user belongs to and that has access to the source
SELECT tm.team_id FROM team_members tm
JOIN team_sources ts ON tm.team_id = ts.team_id
WHERE tm.user_id = $1 AND ts.source_id = $2
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
    tm.user_id = $1  -- The current user ID
ORDER BY
    t.created_at DESC;

-- name: GetTeamByName :one
-- Get a team by its name
SELECT * FROM teams WHERE name = $1;

-- name: ListSourcesForUser :many
-- List all sources a user has access to
SELECT DISTINCT s.* FROM sources s
JOIN team_sources ts ON s.id = ts.source_id
JOIN team_members tm ON ts.team_id = tm.team_id
WHERE tm.user_id = $1
ORDER BY s.created_at DESC;

-- API Tokens

-- name: CreateAPIToken :one
-- Create a new API token
INSERT INTO api_tokens (user_id, name, token_hash, prefix, expires_at, scopes)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: GetAPIToken :one
-- Get an API token by ID
SELECT * FROM api_tokens WHERE id = $1;

-- name: GetAPITokenByHash :one
-- Get an API token by its hash (for authentication)
SELECT * FROM api_tokens WHERE token_hash = $1;

-- name: ListAPITokensForUser :many
-- List all API tokens for a user
SELECT * FROM api_tokens WHERE user_id = $1 ORDER BY created_at DESC;

-- name: UpdateAPITokenLastUsed :exec
-- Update the last used timestamp for an API token
UPDATE api_tokens
SET last_used_at = now(),
    updated_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '60 seconds');

-- name: DeleteAPIToken :exec
-- Delete an API token by ID and user ID (ensure user owns the token)
DELETE FROM api_tokens WHERE id = $1 AND user_id = $2;

-- Alerts

-- name: CreateAlert :one
INSERT INTO alerts (
    source_id,
    name,
    description,
    query_language,
    editor_mode,
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
    group_by_json,
    is_active,
    created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
RETURNING *;

-- name: GetAlert :one
SELECT * FROM alerts WHERE id = $1;

-- name: ListAlertsBySource :many
-- List alerts for one source
SELECT * FROM alerts
WHERE source_id = $1
ORDER BY updated_at DESC, created_at DESC;

-- name: ListAlertsForUser :many
-- List every alert the user can see (any source attached to any of their teams)
SELECT a.* FROM alerts a
WHERE a.source_id IN (
    SELECT DISTINCT ts.source_id
    FROM team_sources ts
    JOIN team_members tm ON tm.team_id = ts.team_id
    WHERE tm.user_id = $1
)
ORDER BY a.updated_at DESC, a.created_at DESC;

-- name: UpdateAlert :one
UPDATE alerts
SET name = $1,
    description = $2,
    query_language = $3,
    editor_mode = $4,
    query = $5,
    condition_json = $6,
    lookback_seconds = $7,
    threshold_operator = $8,
    threshold_value = $9,
    frequency_seconds = $10,
    severity = $11,
    labels_json = $12,
    annotations_json = $13,
    recipient_user_ids_json = $14,
    webhook_urls_json = $15,
    generator_url = $16,
    group_by_json = $17,
    is_active = $18,
    eval_seq = eval_seq + 1,
    updated_at = now()
WHERE id = $19
RETURNING id;

-- name: DeleteAlert :one
DELETE FROM alerts WHERE id = $1
RETURNING id;

-- name: MarkAlertEvaluated :exec
UPDATE alerts
SET last_state = 'resolved',
    last_evaluated_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: TouchAlertEvaluated :exec
UPDATE alerts
SET last_evaluated_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: MarkAlertTriggered :exec
UPDATE alerts
SET last_state = 'firing',
    last_triggered_at = CASE WHEN last_state = 'firing' THEN last_triggered_at ELSE now() END,
    last_evaluated_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: AdvanceAlertEvalSeq :one
UPDATE alerts
SET eval_seq = eval_seq + 1
WHERE id = sqlc.arg(id) AND eval_seq = sqlc.arg(eval_seq)
RETURNING id;

-- Alert instance queries

-- name: ListAlertInstances :many
SELECT * FROM alert_instances
WHERE alert_id = $1
ORDER BY group_key;

-- name: UpsertAlertInstance :exec
INSERT INTO alert_instances (alert_id, group_key, group_json, state, value, state_since, last_seen_at, history_id, notified)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (alert_id, group_key) DO UPDATE SET
    group_json = excluded.group_json,
    state = excluded.state,
    value = excluded.value,
    state_since = excluded.state_since,
    last_seen_at = excluded.last_seen_at,
    history_id = excluded.history_id,
    notified = excluded.notified;

-- name: MarkAlertInstanceNotified :one
UPDATE alert_instances
SET notified = TRUE
WHERE alert_id = $1 AND group_key = $2 AND history_id = $3
RETURNING alert_id;

-- name: DeleteAlertInstances :exec
DELETE FROM alert_instances WHERE alert_id = $1;

-- name: DeleteAlertInstance :exec
DELETE FROM alert_instances WHERE alert_id = sqlc.arg(alert_id) AND group_key = sqlc.arg(group_key);

-- name: CountAlertInstancesByState :many
-- Instance counts of the given alerts, one row per (alert, state).
SELECT alert_id, state, COUNT(*) AS instances
FROM alert_instances
WHERE alert_id = ANY(sqlc.arg(alert_ids)::bigint[])
GROUP BY alert_id, state;

-- name: RefreshAlertState :exec
-- After a manual resolve of one group: the rule is firing while any group fires.
UPDATE alerts
SET last_state = CASE WHEN EXISTS (
        SELECT 1 FROM alert_instances WHERE alert_instances.alert_id = alerts.id AND alert_instances.state = 'firing'
    ) THEN 'firing' ELSE 'resolved' END,
    eval_seq = eval_seq + 1,
    updated_at = now()
WHERE id = sqlc.arg(id);

-- Alert history queries

-- name: InsertAlertHistory :one
INSERT INTO alert_history (
    alert_id,
    group_key,
    group_json,
    status,
    value,
    message,
    payload_json
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: InterruptAlertDeliveries :exec
UPDATE alert_history
SET payload_json = ((payload_json::jsonb - 'delivery_deadline_at') || jsonb_build_object(
    'delivery_in_progress', false,
    'delivery_failed', true,
    'delivery_error', 'delivery interrupted',
    'delivery_retries_exhausted', COALESCE((payload_json::jsonb ->> 'delivery_attempts')::integer, 1) >= 3))::text
WHERE status = 'triggered'
  AND payload_json::jsonb ->> 'delivery_in_progress' = 'true'
  AND (sqlc.arg(history_id)::bigint = 0 OR id = sqlc.arg(history_id))
  AND (sqlc.arg(alert_id)::bigint = 0 OR alert_id = sqlc.arg(alert_id));

-- name: GetTriggeredAlertHistoryAlertID :one
SELECT alert_id FROM alert_history
WHERE id = $1 AND status = 'triggered'
FOR UPDATE;

-- name: ResolveAlertHistoryForAlert :exec
UPDATE alert_history
SET status = 'resolved',
    resolved_at = now(),
    message = sqlc.arg(message)
WHERE alert_id = sqlc.arg(alert_id) AND status = 'triggered';

-- name: UpdateAlertHistoryPayload :one
UPDATE alert_history
SET payload_json = $1
WHERE id = $2 AND status = 'triggered'
RETURNING id;

-- name: GetLatestUnresolvedAlertHistory :one
SELECT * FROM alert_history
WHERE alert_id = $1 AND status = 'triggered'
ORDER BY triggered_at DESC, id DESC
LIMIT 1;

-- name: ListOpenAlertHistory :many
-- Every open episode of the alert, newest first.
SELECT * FROM alert_history
WHERE alert_id = sqlc.arg(alert_id) AND status = 'triggered'
ORDER BY triggered_at DESC, id DESC;

-- name: ListOpenAlertGroupHistoryIDs :many
SELECT id FROM alert_history
WHERE alert_id = sqlc.arg(alert_id) AND group_key = sqlc.arg(group_key) AND status = 'triggered'
FOR UPDATE;

-- name: ResolveAlertGroupHistory :exec
UPDATE alert_history
SET status = 'resolved',
    resolved_at = now(),
    message = sqlc.arg(message)
WHERE alert_id = sqlc.arg(alert_id) AND group_key = sqlc.arg(group_key) AND status = 'triggered';

-- name: GetAlertHistoryEntry :one
SELECT * FROM alert_history
WHERE id = $1 AND alert_id = $2;

-- name: ListAlertHistory :many
SELECT * FROM alert_history
WHERE alert_id = $1
ORDER BY triggered_at DESC, id DESC
LIMIT $2;

-- name: PruneAlertHistory :exec
-- Keeps every open episode and the newest N closed rows.
DELETE FROM alert_history AS target
WHERE target.alert_id = $1
  AND target.status <> 'triggered'
  AND target.id NOT IN (
    SELECT keep.id
    FROM alert_history AS keep
    WHERE keep.alert_id = $2 AND keep.status <> 'triggered'
    ORDER BY keep.triggered_at DESC, keep.id DESC
    LIMIT $3
 );

-- System Settings Queries

-- name: GetSystemSetting :one
SELECT * FROM system_settings
WHERE key = $1;

-- name: ListSystemSettings :many
SELECT * FROM system_settings
ORDER BY category, key;

-- name: ListSystemSettingsByCategory :many
SELECT * FROM system_settings
WHERE category = $1
ORDER BY key;

-- name: UpsertSystemSetting :exec
INSERT INTO system_settings (key, value, value_type, category, description, is_sensitive, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT(key) DO UPDATE SET
    value = excluded.value,
    value_type = excluded.value_type,
    description = excluded.description,
    is_sensitive = system_settings.is_sensitive OR excluded.is_sensitive,
    updated_at = now();

-- name: DeleteSystemSetting :exec
DELETE FROM system_settings
WHERE key = $1;

-- name: ListUsersForProvisioning :many
SELECT * FROM users WHERE email = ANY(sqlc.arg(emails)::text[]) ORDER BY id;

-- name: ListManagedSources :many
-- Get all sources managed by provisioning config
SELECT * FROM sources WHERE managed = true ORDER BY id;

-- name: ListManagedTeams :many
-- Get all teams managed by provisioning config
SELECT * FROM teams WHERE managed = true ORDER BY id;

-- name: SetSourceManaged :exec
-- Mark a source as managed/unmanaged and set secret_ref
UPDATE sources SET managed = $1, secret_ref = $2, updated_at = now() WHERE id = $3;

-- name: SetTeamManaged :exec
-- Mark a team as managed/unmanaged
UPDATE teams SET managed = $1, updated_at = now() WHERE id = $2;

-- name: SetUserManaged :exec
-- Mark a user as managed/unmanaged
UPDATE users SET managed = $1, updated_at = now() WHERE id = $2;

-- name: IsSourceManaged :one
-- Check if a source is managed
SELECT managed FROM sources WHERE id = $1;

-- name: IsTeamManaged :one
-- Check if a team is managed
SELECT managed FROM teams WHERE id = $1;

-- name: GetSourceByNameForProvisioning :one
-- Get source by name for provisioning lookup
SELECT * FROM sources WHERE name = $1;


-- Collections (cross-team curation lists for saved queries)

-- name: CreateCollection :one
-- Insert a new collection (personal or shared)
INSERT INTO collections (name, description, is_personal, created_by)
VALUES ($1, $2, $3, $4)
RETURNING id, created_at, updated_at;

-- name: GetCollection :one
-- Look up a collection by id with direct member, shared team, and item counts
SELECT
    c.id,
    c.name,
    c.description,
    c.is_personal,
    c.created_by,
    c.created_at,
    c.updated_at,
    (SELECT COUNT(*) FROM collection_members WHERE collection_id = c.id) AS member_count,
    (SELECT COUNT(*) FROM collection_teams WHERE collection_id = c.id) AS team_count,
    (SELECT COUNT(*) FROM collection_items WHERE collection_id = c.id) AS item_count
FROM collections c
WHERE c.id = $1;

-- name: GetPersonalCollection :one
-- Find the caller's personal collection if it exists
SELECT * FROM collections WHERE created_by = $1 AND is_personal = true;

-- name: UpdateCollection :exec
-- Update name/description (owner only - enforced in app code)
UPDATE collections
SET name = $1,
    description = $2,
    updated_at = now()
WHERE id = $3;

-- name: DeleteCollection :exec
-- Delete a collection. Personal collections cannot be deleted (enforced in app code).
DELETE FROM collections WHERE id = $1;

-- name: ListCollectionsForUser :many
-- List collections the user can see: direct membership (any role) or membership
-- in a team the collection is shared with. Each collection appears once. A
-- direct role wins; team-only access is always 'member'. member_count counts
-- direct membership rows only (owners included); team_count counts team shares.
SELECT
    c.id,
    c.name,
    c.description,
    c.is_personal,
    c.created_by,
    c.created_at,
    c.updated_at,
    CAST(COALESCE(cm.role, 'member') AS TEXT) AS caller_role,
    (SELECT COUNT(*) FROM collection_members WHERE collection_id = c.id) AS member_count,
    (SELECT COUNT(*) FROM collection_teams WHERE collection_id = c.id) AS team_count,
    (SELECT COUNT(*) FROM collection_items WHERE collection_id = c.id) AS item_count
FROM collections c
LEFT JOIN collection_members cm ON cm.collection_id = c.id AND cm.user_id = sqlc.arg(user_id)
WHERE cm.user_id IS NOT NULL
   OR EXISTS (
        SELECT 1
        FROM collection_teams ct
        JOIN team_members tm ON tm.team_id = ct.team_id
        WHERE ct.collection_id = c.id AND tm.user_id = sqlc.arg(user_id)
   )
ORDER BY c.is_personal DESC, c.updated_at DESC;

-- name: AddCollectionMember :exec
-- Add a member; idempotent on (collection_id, user_id).
INSERT INTO collection_members (collection_id, user_id, role, added_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT(collection_id, user_id) DO NOTHING;

-- name: GetCollectionMember :one
-- Look up a single membership row
SELECT collection_id, user_id, role, added_by, created_at
FROM collection_members
WHERE collection_id = $1 AND user_id = $2;

-- name: ListCollectionMembers :many
-- List members of a collection with user details
SELECT cm.collection_id, cm.user_id, cm.role, cm.added_by, cm.created_at,
       u.email, u.full_name
FROM collection_members cm
JOIN users u ON u.id = cm.user_id
WHERE cm.collection_id = $1
ORDER BY cm.role DESC, u.email ASC;

-- name: ListCollectionEditors :many
SELECT cm.collection_id, cm.user_id, cm.role, cm.added_by, cm.created_at,
       u.email, u.full_name
FROM collection_members cm
JOIN users u ON u.id = cm.user_id
WHERE cm.collection_id = ANY(sqlc.arg(collection_ids)::bigint[]) AND cm.role IN ('owner', 'editor')
ORDER BY cm.role DESC, u.email ASC;

-- name: RemoveCollectionMember :exec
-- Remove a member from a collection
DELETE FROM collection_members WHERE collection_id = $1 AND user_id = $2;

-- name: LockCollection :one
-- Lock the collection row until the transaction ends, so a link, member or
-- share write authorizes, mutates and selects its response against one state.
SELECT id FROM collections WHERE id = $1 FOR UPDATE;

-- name: AddCollectionItem :execrows
-- Link a saved query to a collection. An existing link of any kind is left as
-- it is; the row count tells the caller whether the insert happened.
INSERT INTO collection_items (collection_id, saved_query_id, sort_order, kind, added_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT(collection_id, saved_query_id) DO NOTHING;

-- name: GetCollectionItemLink :one
-- Read one link row
SELECT collection_id, saved_query_id, kind, added_by, sort_order, created_at
FROM collection_items
WHERE collection_id = $1 AND saved_query_id = $2;

-- name: SetCollectionItemKind :execrows
-- Convert a link to the given kind. One changed row means the link had the
-- other kind.
UPDATE collection_items
SET kind = $1
WHERE collection_id = $2 AND saved_query_id = $3 AND kind <> $1;

-- name: RemoveCollectionItem :execrows
-- Remove a link of either kind
DELETE FROM collection_items WHERE collection_id = $1 AND saved_query_id = $2;

-- name: RemoveCollectionPin :execrows
-- Remove a link only when it is a pin. A managed link is left as it is.
DELETE FROM collection_items
WHERE collection_id = $1 AND saved_query_id = $2 AND kind = 'pin';

-- name: ListCollectionItems :many
-- List items in a collection with saved-query details
SELECT
    ci.collection_id,
    ci.saved_query_id,
    ci.kind,
    ci.sort_order,
    ci.added_by,
    ci.created_at AS item_added_at,
    sq.id AS query_id,
    sq.source_id,
    sq.name AS query_name,
    sq.description AS query_description,
    sq.query_language,
    sq.editor_mode,
    sq.query_content,
    sq.created_by AS query_created_by,
    sq.created_at AS query_created_at,
    sq.updated_at AS query_updated_at,
    s.name AS source_name
FROM collection_items ci
JOIN saved_queries sq ON sq.id = ci.saved_query_id
JOIN sources s ON s.id = sq.source_id
WHERE ci.collection_id = $1
ORDER BY ci.sort_order ASC, ci.created_at ASC;

-- name: AddCollectionTeam :exec
-- Share a collection with a team; idempotent on (collection_id, team_id).
INSERT INTO collection_teams (collection_id, team_id, added_by)
VALUES ($1, $2, $3)
ON CONFLICT(collection_id, team_id) DO NOTHING;

-- name: RemoveCollectionTeam :exec
-- Remove a team share. Direct memberships and other team shares are untouched.
DELETE FROM collection_teams WHERE collection_id = $1 AND team_id = $2;

-- name: ListCollectionTeams :many
-- List teams a collection is shared with, with team names
SELECT ct.collection_id, ct.team_id, ct.added_by, ct.created_at, t.name AS team_name
FROM collection_teams ct
JOIN teams t ON t.id = ct.team_id
WHERE ct.collection_id = $1
ORDER BY t.name ASC;

-- name: CountCollectionTeamAccess :one
-- Count the teams shared with a collection that the user currently belongs to.
-- A non-zero count grants the user the collection Member role.
SELECT COUNT(*)
FROM collection_teams ct
JOIN team_members tm ON tm.team_id = ct.team_id
WHERE ct.collection_id = $1 AND tm.user_id = $2;

-- name: CountSharedCollectionEditAccess :one
-- Count shared (non-personal) collections that manage the given saved query
-- and in which the user is an owner or editor. A non-zero count means the
-- user has delegated edit rights on that query. A pin grants nothing.
SELECT COUNT(*)
FROM collection_items ci
JOIN collections c ON c.id = ci.collection_id
JOIN collection_members cm ON cm.collection_id = ci.collection_id
WHERE ci.saved_query_id = $1
  AND ci.kind = 'managed'
  AND cm.user_id = $2
  AND c.is_personal = false
  AND cm.role IN ('owner', 'editor');

-- name: ListSharedCollectionPins :many
-- Every pin in a shared collection, oldest first, with the query's creator and
-- the user who added the link. Backs the admin report of demoted links.
SELECT
    ci.collection_id,
    c.name AS collection_name,
    ci.saved_query_id,
    sq.name AS saved_query_name,
    sq.created_by AS query_created_by,
    cu.email AS query_creator_email,
    ci.added_by,
    au.email AS added_by_email,
    ci.created_at
FROM collection_items ci
JOIN collections c ON c.id = ci.collection_id
JOIN saved_queries sq ON sq.id = ci.saved_query_id
LEFT JOIN users cu ON cu.id = sq.created_by
LEFT JOIN users au ON au.id = ci.added_by
WHERE ci.kind = 'pin' AND c.is_personal = false
ORDER BY ci.created_at ASC, ci.collection_id ASC, ci.saved_query_id ASC;

-- Dashboards -----------------------------------------------------------------

-- name: CreateDashboard :one
-- Insert a new dashboard with the derived list columns of its blob.
INSERT INTO dashboards (name, description, panels_json, created_by, panel_count, tab_count, panel_types, panel_team_ids, panels_corrupt, summary_revision, summary_version)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, $10)
RETURNING id;

-- name: GetDashboard :one
-- Look up one dashboard by id, with creator identity like ListDashboardSummaries.
SELECT
    d.id,
    d.name,
    d.description,
    d.panels_json,
    d.created_by,
    d.created_at,
    d.updated_at,
    d.revision,
    u.email AS created_by_email,
    u.full_name AS created_by_name
FROM dashboards d
LEFT JOIN users u ON u.id = d.created_by
WHERE d.id = $1;

-- name: ListDashboardSummaries :many
-- List every dashboard's summary, newest-updated first, from the derived
-- columns and never from panels_json. A name or description from before the
-- bound is cut to it.
SELECT
    d.id,
    left(d.name, 200)::text AS name,
    coalesce(left(d.description, 2000), '')::text AS description,
    d.created_by,
    d.created_at,
    d.updated_at,
    d.revision,
    d.panel_count,
    d.tab_count,
    d.panel_types,
    d.panel_team_ids,
    d.panels_corrupt,
    d.summary_revision,
    d.summary_version,
    u.email AS created_by_email,
    u.full_name AS created_by_name
FROM dashboards d
LEFT JOIN users u ON u.id = d.created_by
ORDER BY d.updated_at DESC, d.id DESC;

-- name: ListStaleDashboardSummaryInputs :many
-- Rows whose derived columns do not match their blob, in id order.
SELECT id, revision, panels_json
FROM dashboards
WHERE id > $1 AND (summary_revision != revision OR summary_version < $2)
ORDER BY id ASC
LIMIT $3;

-- name: RepairDashboardSummary :execrows
-- Write derived columns for one revision, only while the row still holds that
-- revision and is still stale. Changes neither revision nor updated_at.
UPDATE dashboards
SET panel_count = $1,
    tab_count = $2,
    panel_types = $3,
    panel_team_ids = $4,
    panels_corrupt = $5,
    summary_revision = $6,
    summary_version = $7
WHERE id = $8 AND revision = $9 AND (summary_revision != revision OR summary_version < $10);

-- name: UpdateDashboard :one
-- Compare-and-swap on the client revision; no row means missing or stale. The
-- derived columns move with the blob in this one statement.
UPDATE dashboards
SET name = $1,
    description = $2,
    panels_json = $3,
    panel_count = $4,
    tab_count = $5,
    panel_types = $6,
    panel_team_ids = $7,
    panels_corrupt = $8,
    summary_revision = revision + 1,
    summary_version = $9,
    revision = revision + 1,
    updated_at = now()
WHERE id = $10 AND revision = $11
RETURNING revision;

-- name: DeleteDashboard :one
-- Delete a dashboard; RETURNING lets callers detect not-found.
DELETE FROM dashboards WHERE id = $1
RETURNING id;

-- Query history ---------------------------------------------------------------

-- name: InsertQueryHistory :one
-- Record one executed query and return its id.
INSERT INTO query_history (user_id, team_id, source_id, query_text, query_language, duration_ms, row_count)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: PruneQueryHistoryForUser :exec
-- Delete a user's history rows beyond the newest `offset` (the per-user cap),
-- keeping history bounded on every insert.
DELETE FROM query_history
WHERE id IN (
    SELECT qh.id FROM query_history qh
    WHERE qh.user_id = $1
    ORDER BY qh.created_at DESC, qh.id DESC
    OFFSET $2
);

-- name: ListQueryHistory :many
-- List one user's recent history, newest first.
SELECT
    id,
    user_id,
    team_id,
    source_id,
    query_text,
    query_language,
    duration_ms,
    row_count,
    created_at
FROM query_history
WHERE user_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: ListQueryActivity :many
-- Most recent query_history rows across all users, newest first, enriched with
-- the executing user's email and the source's display name. LEFT JOIN on
-- sources so history survives a deleted source (source_name is NULL then).
-- Backs the admin recent-activity view; the table is capped per user, so this
-- is a recent window, not all-time analytics.
SELECT
    qh.*,
    u.email AS user_email,
    s.name AS source_name
FROM query_history qh
JOIN users u ON u.id = qh.user_id
LEFT JOIN sources s ON s.id = qh.source_id
ORDER BY qh.created_at DESC, qh.id DESC
LIMIT $1;

-- Query stats daily rollup -----------------------------------------------------

-- name: IncrementQueryStats :exec
-- Upsert one executed query into the non-pruned daily rollup: add 1 to
-- query_count and the given duration to total_duration_ms for the composite key.
INSERT INTO query_stats_daily (bucket_date, user_id, team_id, source_id, query_language, query_count, total_duration_ms)
VALUES ($1, $2, $3, $4, $5, 1, $6)
ON CONFLICT (bucket_date, user_id, team_id, source_id, query_language)
DO UPDATE SET
    query_count = query_stats_daily.query_count + 1,
    total_duration_ms = query_stats_daily.total_duration_ms + EXCLUDED.total_duration_ms;

-- name: TopSourcesByQueries :many
-- Top sources by total query count over rollup rows on/after `since`, with the
-- source display name (LEFT JOIN so a deleted source yields ''), and integer
-- average duration (0 when count is 0).
SELECT
    qsd.source_id AS source_id,
    COALESCE(s.name, '') AS source_name,
    SUM(qsd.query_count)::bigint AS query_count,
    (CASE WHEN SUM(qsd.query_count) > 0
        THEN TRUNC(SUM(qsd.total_duration_ms)::numeric / SUM(qsd.query_count))
        ELSE 0 END)::bigint AS avg_duration_ms
FROM query_stats_daily qsd
LEFT JOIN sources s ON s.id = qsd.source_id
WHERE qsd.bucket_date >= $1
GROUP BY qsd.source_id, s.name
ORDER BY query_count DESC, qsd.source_id ASC
LIMIT $2;

-- name: TopUsersByQueries :many
-- Top users by total query count over rollup rows on/after `since`, joined to
-- users for the email.
SELECT
    qsd.user_id AS user_id,
    u.email AS user_email,
    SUM(qsd.query_count)::bigint AS query_count
FROM query_stats_daily qsd
JOIN users u ON u.id = qsd.user_id
WHERE qsd.bucket_date >= $1
GROUP BY qsd.user_id, u.email
ORDER BY query_count DESC, qsd.user_id ASC
LIMIT $2;

-- name: QueryVolumeByDay :many
-- Per-day total query count over rollup rows on/after `since`, ascending by day.
SELECT
    qsd.bucket_date AS bucket_date,
    SUM(qsd.query_count)::bigint AS query_count
FROM query_stats_daily qsd
WHERE qsd.bucket_date >= $1
GROUP BY qsd.bucket_date
ORDER BY qsd.bucket_date ASC;

-- OAuth authorization server ------------------------------------------------
-- Every *_hash argument is an HMAC hex digest, never a plaintext code or
-- token. Postgres runs at READ COMMITTED: a conditional UPDATE waits on the
-- row lock and re-checks its predicate, so exactly one writer wins.

-- name: CreateOAuthAuthRequest :exec
INSERT INTO oauth_auth_requests (
    id, client_id, redirect_uri, resource, scopes, offline_access,
    code_challenge, state, expires_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: GetOAuthAuthRequest :one
SELECT * FROM oauth_auth_requests WHERE id = $1;

-- name: ClaimOAuthAuthRequestDecision :one
-- Record the first decision on a pending, unexpired request. Zero rows means
-- the request is unknown, expired or already decided.
UPDATE oauth_auth_requests
SET decided_at = sqlc.arg(now)::timestamptz, user_id = sqlc.arg(user_id), denied = sqlc.arg(denied)
WHERE id = sqlc.arg(id) AND decided_at IS NULL AND expires_at > sqlc.arg(now)::timestamptz
RETURNING *;

-- name: SetOAuthAuthRequestGrant :exec
UPDATE oauth_auth_requests SET grant_id = $1 WHERE id = $2;

-- name: SaveOAuthAuthCode :execrows
-- Attach the code to an approved request. A request carries at most one code.
UPDATE oauth_auth_requests
SET code_hash = sqlc.arg(code_hash), code_expires_at = sqlc.arg(code_expires_at)
WHERE id = sqlc.arg(id) AND grant_id IS NOT NULL AND NOT denied AND code_hash IS NULL;

-- name: ConsumeOAuthAuthCode :one
-- Single-use consumption. Exactly one concurrent caller gets the row.
UPDATE oauth_auth_requests
SET code_consumed_at = sqlc.arg(now)::timestamptz
WHERE code_hash = sqlc.arg(code_hash)
  AND code_consumed_at IS NULL
  AND code_expires_at > sqlc.arg(now)::timestamptz
  AND grant_id IN (SELECT g.id FROM oauth_grants g WHERE g.revoked_at IS NULL)
RETURNING *;

-- name: GetOAuthAuthCodeState :one
SELECT grant_id, code_consumed_at FROM oauth_auth_requests WHERE code_hash = $1;

-- name: CreateOAuthGrant :one
INSERT INTO oauth_grants (user_id, client_id, resource, scopes, offline_access, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetOAuthGrant :one
SELECT * FROM oauth_grants WHERE id = $1;

-- name: RevokeOAuthGrant :execrows
UPDATE oauth_grants
SET revoked_at = sqlc.arg(now), revoke_reason = sqlc.arg(reason)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeOAuthGrantForUser :execrows
UPDATE oauth_grants
SET revoked_at = sqlc.arg(now), revoke_reason = sqlc.arg(reason)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND revoked_at IS NULL;

-- name: RevokeOAuthGrantByToken :exec
-- Revoke the grant that owns an access-token ID hash or refresh-token hash,
-- only when the grant belongs to the given client.
UPDATE oauth_grants
SET revoked_at = sqlc.arg(now), revoke_reason = sqlc.arg(reason)
WHERE oauth_grants.revoked_at IS NULL
  AND oauth_grants.client_id = sqlc.arg(client_id)
  AND oauth_grants.id IN (
      SELECT a.grant_id FROM oauth_access_tokens a WHERE a.id_hash = sqlc.arg(token_hash)
      UNION
      SELECT r.grant_id FROM oauth_refresh_tokens r WHERE r.token_hash = sqlc.arg(token_hash)
  );

-- name: TouchOAuthGrant :exec
UPDATE oauth_grants SET last_used_at = $1 WHERE id = $2;

-- name: ListOAuthGrantsForUser :many
SELECT * FROM oauth_grants
WHERE user_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: InsertOAuthAccessToken :execrows
-- Insert only while the grant is active, so a token is never issued under a
-- grant revoked a moment earlier.
INSERT INTO oauth_access_tokens (id_hash, grant_id, expires_at, created_at)
SELECT sqlc.arg(id_hash), g.id, sqlc.arg(expires_at)::timestamptz, sqlc.arg(created_at)::timestamptz
FROM oauth_grants g
WHERE g.id = sqlc.arg(grant_id) AND g.revoked_at IS NULL;

-- name: InsertOAuthRefreshToken :execrows
INSERT INTO oauth_refresh_tokens (token_hash, grant_id, expires_at, created_at)
SELECT sqlc.arg(token_hash), g.id, sqlc.arg(expires_at)::timestamptz, sqlc.arg(created_at)::timestamptz
FROM oauth_grants g
WHERE g.id = sqlc.arg(grant_id) AND g.revoked_at IS NULL;

-- name: GetOAuthRefreshTokenState :one
SELECT r.consumed_at, r.expires_at, sqlc.embed(g)
FROM oauth_refresh_tokens r
JOIN oauth_grants g ON g.id = r.grant_id
WHERE r.token_hash = $1;

-- name: ConsumeOAuthRefreshToken :one
-- Rotation claim. Exactly one concurrent caller gets the row.
UPDATE oauth_refresh_tokens
SET consumed_at = sqlc.arg(now)::timestamptz, replaced_by_hash = sqlc.arg(replaced_by_hash)
WHERE token_hash = sqlc.arg(token_hash)
  AND consumed_at IS NULL
  AND expires_at > sqlc.arg(now)::timestamptz
  AND grant_id IN (SELECT g.id FROM oauth_grants g WHERE g.revoked_at IS NULL)
RETURNING grant_id;

-- name: AuthenticateOAuthAccessToken :one
SELECT sqlc.embed(g), sqlc.embed(u)
FROM oauth_access_tokens a
JOIN oauth_grants g ON g.id = a.grant_id
JOIN users u ON u.id = g.user_id
WHERE a.id_hash = sqlc.arg(id_hash)
  AND a.expires_at > sqlc.arg(now)::timestamptz
  AND g.revoked_at IS NULL
  AND g.resource = sqlc.arg(resource)
  AND u.status = 'active'
  AND u.account_type = 'human';

-- name: DeleteExpiredOAuthAuthRequests :exec
DELETE FROM oauth_auth_requests
WHERE expires_at <= sqlc.arg(now)::timestamptz
  AND (code_expires_at IS NULL OR code_expires_at <= sqlc.arg(now)::timestamptz);

-- name: DeleteExpiredOAuthAccessTokens :exec
DELETE FROM oauth_access_tokens WHERE expires_at <= $1;

-- name: DeleteExpiredOAuthRefreshFamilies :exec
-- Delete refresh rows only when no row of the same grant is still live. A
-- consumed row therefore stays while its family can still be refreshed, so a
-- replay of it is still detected.
DELETE FROM oauth_refresh_tokens
WHERE NOT EXISTS (
    SELECT 1 FROM oauth_refresh_tokens live
    WHERE live.grant_id = oauth_refresh_tokens.grant_id
      AND live.expires_at > sqlc.arg(now)::timestamptz
);

-- name: ReapInterruptedExportJobs :many
UPDATE export_jobs AS e
SET status = 'failed', error_message = 'interrupted by restart', updated_at = now()
FROM (
    SELECT j.id, j.updated_at FROM export_jobs j
    WHERE j.status IN ('pending', 'running')
      AND (j.updated_at < now() - sqlc.arg(max_age_seconds)::bigint * interval '1 second'
        OR (sqlc.arg(instance_id)::text <> '' AND j.owner_instance_id = sqlc.arg(instance_id) AND j.owner_process_id <> sqlc.arg(process_id)))
    ORDER BY j.id
    LIMIT 100
    FOR UPDATE
) AS interrupted
WHERE e.id = interrupted.id
RETURNING e.id, e.file_path, interrupted.updated_at AS previous_updated_at;

-- name: MarkAlertResolved :exec
UPDATE alerts SET last_state = 'resolved', eval_seq = eval_seq + 1, updated_at = CURRENT_TIMESTAMP WHERE id = $1;

-- Jobs (internal/jobs). Every time comparison uses the database clock.

-- name: UpsertJob :exec
-- A new job is due now. A changed schedule makes an existing job due now;
-- otherwise its next run and lease are kept.
INSERT INTO jobs (
    kind, key, schedule_kind, schedule_expr, timezone, misfire, grace_seconds, enabled,
    next_run_at, created_at, updated_at
) VALUES (
    sqlc.arg(kind), sqlc.arg(key), sqlc.arg(schedule_kind), sqlc.arg(schedule_expr),
    sqlc.arg(timezone), sqlc.arg(misfire), sqlc.arg(grace_seconds), sqlc.arg(enabled),
    now(), now(), now()
)
ON CONFLICT (kind, key) DO UPDATE SET
    next_run_at = CASE
        WHEN jobs.schedule_kind = excluded.schedule_kind
         AND jobs.schedule_expr = excluded.schedule_expr
         AND jobs.timezone = excluded.timezone
        THEN jobs.next_run_at
        ELSE excluded.next_run_at
    END,
    schedule_kind = excluded.schedule_kind,
    schedule_expr = excluded.schedule_expr,
    timezone = excluded.timezone,
    misfire = excluded.misfire,
    grace_seconds = excluded.grace_seconds,
    enabled = excluded.enabled,
    updated_at = excluded.updated_at;

-- name: DeleteJob :exec
DELETE FROM jobs WHERE kind = sqlc.arg(kind) AND key = sqlc.arg(key);

-- name: GetJob :one
SELECT * FROM jobs WHERE kind = sqlc.arg(kind) AND key = sqlc.arg(key);

-- name: KickJob :execrows
UPDATE jobs SET next_run_at = now(), updated_at = now()
WHERE kind = sqlc.arg(kind) AND key = sqlc.arg(key);

-- name: ClaimJobs :many
-- Concurrent claims from several replicas receive disjoint rows. SKIP LOCKED
-- also passes over a job whose handler holds its row through jobs.Hold, so
-- the token cannot rise before that handler commits.
-- last_started_at is the database clock at the claim.
UPDATE jobs
SET lease_owner = sqlc.arg(owner),
    lease_token = lease_token + 1,
    lease_expires_at = now() + make_interval(secs => sqlc.arg(lease_seconds)::double precision),
    last_started_at = now(),
    updated_at = now()
WHERE id IN (
    SELECT j.id FROM jobs j
    WHERE j.enabled
      AND j.kind = sqlc.arg(kind)
      AND j.next_run_at <= now()
      AND (j.lease_expires_at IS NULL OR j.lease_expires_at < now())
    ORDER BY j.next_run_at
    LIMIT sqlc.arg(max_jobs)
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: HoldJob :execrows
UPDATE jobs SET updated_at = now()
WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(lease_token);

-- name: FinishJob :execrows
-- The next run applies only while the job keeps the schedule the run used,
-- so a schedule change made during the run is not overwritten.
UPDATE jobs
SET next_run_at = CASE
        WHEN schedule_kind = sqlc.arg(schedule_kind)
         AND schedule_expr = sqlc.arg(schedule_expr)
         AND timezone = sqlc.arg(timezone)
        THEN sqlc.arg(next_run_at)
        ELSE next_run_at
    END,
    last_status = sqlc.arg(last_status),
    last_error = sqlc.narg(last_error),
    last_finished_at = now(),
    lease_owner = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(lease_token);

-- name: ReleaseJob :execrows
UPDATE jobs SET lease_owner = NULL, lease_expires_at = NULL, updated_at = now()
WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(lease_token);

-- name: DeleteClaimedJob :execrows
DELETE FROM jobs WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(lease_token);

-- Saved views -----------------------------------------------------------------

-- name: CreateSavedView :one
-- Insert a view; a name or default taken in its scope fails a unique index.
INSERT INTO saved_views (source_id, scope, user_id, team_id, name, description, definition, is_default, managed, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetSavedView :one
SELECT * FROM saved_views WHERE id = $1;

-- name: ListSavedViews :many
-- The caller's personal views, the team's views and the source views of one
-- source, personal first, then team, then source, each by name. COLLATE "C"
-- orders names by bytes, as SQLite does.
SELECT * FROM saved_views
WHERE source_id = sqlc.arg(source_id)
  AND (
    (scope = 'personal' AND user_id = sqlc.arg(user_id))
    OR (scope = 'team' AND team_id = sqlc.arg(team_id))
    OR scope = 'source'
  )
ORDER BY CASE scope WHEN 'personal' THEN 0 WHEN 'team' THEN 1 ELSE 2 END, name COLLATE "C", id;

-- name: UpdateSavedView :one
-- Compare-and-swap on the caller's revision; no row means missing or stale.
UPDATE saved_views
SET name = $1,
    description = $2,
    definition = $3,
    revision = revision + 1,
    updated_at = now()
WHERE id = $4 AND revision = $5
RETURNING *;

-- name: DeleteSavedView :one
DELETE FROM saved_views WHERE id = $1
RETURNING id;

-- name: ClearSavedViewScopeDefault :many
-- Clear the default of one scope and source, except on keep_id. The partial
-- unique index leaves at most one row. A NULL owner column compares as 0.
UPDATE saved_views
SET is_default = FALSE
WHERE source_id = sqlc.arg(source_id)
  AND scope = sqlc.arg(scope)
  AND COALESCE(user_id, 0) = sqlc.arg(user_id)::BIGINT
  AND COALESCE(team_id, 0) = sqlc.arg(team_id)::BIGINT
  AND is_default
  AND id <> sqlc.arg(keep_id)
RETURNING id, managed;

-- name: SetSavedViewDefaultFlag :one
UPDATE saved_views SET is_default = $1 WHERE id = $2
RETURNING id;

-- name: CountSavedViewsInScope :one
-- A NULL owner column compares as 0.
SELECT COUNT(*) FROM saved_views
WHERE source_id = sqlc.arg(source_id)
  AND scope = sqlc.arg(scope)
  AND COALESCE(user_id, 0) = sqlc.arg(user_id)::BIGINT
  AND COALESCE(team_id, 0) = sqlc.arg(team_id)::BIGINT;

-- name: ListManagedSavedViews :many
SELECT * FROM saved_views WHERE managed
ORDER BY source_id, name COLLATE "C", id;

-- Audit events

-- name: InsertAuditEvent :exec
INSERT INTO audit_events (
    id, occurred_at, action, outcome, reason, actor_type, actor_id, actor_label,
    auth_method, credential_id, client_id, target_type, target_id, target_label,
    team_id, source_id, ip, user_agent, http_method, route, status, changes, detail
) VALUES (
    sqlc.arg(id), sqlc.arg(occurred_at), sqlc.arg(action), sqlc.arg(outcome),
    sqlc.arg(reason), sqlc.arg(actor_type), sqlc.narg(actor_id), sqlc.arg(actor_label),
    sqlc.arg(auth_method), sqlc.arg(credential_id), sqlc.arg(client_id), sqlc.arg(target_type),
    sqlc.arg(target_id), sqlc.arg(target_label), sqlc.narg(team_id), sqlc.narg(source_id),
    sqlc.arg(ip), sqlc.arg(user_agent), sqlc.arg(http_method), sqlc.arg(route),
    sqlc.arg(status), sqlc.arg(changes), sqlc.arg(detail)
);

-- name: ListAuditEventsDesc :many
-- Newest first. An empty text argument or a NULL argument disables its
-- filter; a NULL cursor_at starts at the newest event.
SELECT * FROM audit_events
WHERE (sqlc.narg(from_at)::timestamptz IS NULL OR occurred_at >= sqlc.narg(from_at))
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR occurred_at < sqlc.narg(to_at))
  AND (sqlc.arg(action)::text = '' OR action = sqlc.arg(action))
  AND (sqlc.arg(action_prefix)::text = '' OR left(action, length(sqlc.arg(action_prefix))) = sqlc.arg(action_prefix))
  AND (sqlc.narg(actor_id)::bigint IS NULL OR actor_id = sqlc.narg(actor_id))
  AND (sqlc.arg(auth_method)::text = '' OR auth_method = sqlc.arg(auth_method))
  AND (sqlc.arg(credential_id)::text = '' OR credential_id = sqlc.arg(credential_id))
  AND (sqlc.arg(target_type)::text = '' OR target_type = sqlc.arg(target_type))
  AND (sqlc.arg(target_id)::text = '' OR target_id = sqlc.arg(target_id))
  AND (sqlc.narg(team_id)::bigint IS NULL OR team_id = sqlc.narg(team_id))
  AND (sqlc.narg(source_id)::bigint IS NULL OR source_id = sqlc.narg(source_id))
  AND (sqlc.arg(outcome)::text = '' OR outcome = sqlc.arg(outcome))
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
    OR occurred_at < sqlc.narg(cursor_at)
    OR (occurred_at = sqlc.narg(cursor_at) AND id < sqlc.arg(cursor_id)::text))
ORDER BY occurred_at DESC, id DESC
LIMIT sqlc.arg(row_limit)::bigint;

-- name: ListAuditEventsAsc :many
-- Oldest first, with the same filters as ListAuditEventsDesc.
SELECT * FROM audit_events
WHERE (sqlc.narg(from_at)::timestamptz IS NULL OR occurred_at >= sqlc.narg(from_at))
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR occurred_at < sqlc.narg(to_at))
  AND (sqlc.arg(action)::text = '' OR action = sqlc.arg(action))
  AND (sqlc.arg(action_prefix)::text = '' OR left(action, length(sqlc.arg(action_prefix))) = sqlc.arg(action_prefix))
  AND (sqlc.narg(actor_id)::bigint IS NULL OR actor_id = sqlc.narg(actor_id))
  AND (sqlc.arg(auth_method)::text = '' OR auth_method = sqlc.arg(auth_method))
  AND (sqlc.arg(credential_id)::text = '' OR credential_id = sqlc.arg(credential_id))
  AND (sqlc.arg(target_type)::text = '' OR target_type = sqlc.arg(target_type))
  AND (sqlc.arg(target_id)::text = '' OR target_id = sqlc.arg(target_id))
  AND (sqlc.narg(team_id)::bigint IS NULL OR team_id = sqlc.narg(team_id))
  AND (sqlc.narg(source_id)::bigint IS NULL OR source_id = sqlc.narg(source_id))
  AND (sqlc.arg(outcome)::text = '' OR outcome = sqlc.arg(outcome))
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
    OR occurred_at > sqlc.narg(cursor_at)
    OR (occurred_at = sqlc.narg(cursor_at) AND id > sqlc.arg(cursor_id)::text))
ORDER BY occurred_at ASC, id ASC
LIMIT sqlc.arg(row_limit)::bigint;

-- name: CountAuditEvents :one
-- Counts the events that match the filters, stopping at max_rows.
SELECT COUNT(*) FROM (
    SELECT 1 FROM audit_events
    WHERE (sqlc.narg(from_at)::timestamptz IS NULL OR occurred_at >= sqlc.narg(from_at))
      AND (sqlc.narg(to_at)::timestamptz IS NULL OR occurred_at < sqlc.narg(to_at))
      AND (sqlc.arg(action)::text = '' OR action = sqlc.arg(action))
      AND (sqlc.arg(action_prefix)::text = '' OR left(action, length(sqlc.arg(action_prefix))) = sqlc.arg(action_prefix))
      AND (sqlc.narg(actor_id)::bigint IS NULL OR actor_id = sqlc.narg(actor_id))
      AND (sqlc.arg(auth_method)::text = '' OR auth_method = sqlc.arg(auth_method))
      AND (sqlc.arg(credential_id)::text = '' OR credential_id = sqlc.arg(credential_id))
      AND (sqlc.arg(target_type)::text = '' OR target_type = sqlc.arg(target_type))
      AND (sqlc.arg(target_id)::text = '' OR target_id = sqlc.arg(target_id))
      AND (sqlc.narg(team_id)::bigint IS NULL OR team_id = sqlc.narg(team_id))
      AND (sqlc.narg(source_id)::bigint IS NULL OR source_id = sqlc.narg(source_id))
      AND (sqlc.arg(outcome)::text = '' OR outcome = sqlc.arg(outcome))
    LIMIT sqlc.arg(max_rows)::bigint
) AS matched;

-- name: GetAuditPruneBoundary :one
-- The newest event beyond the keep_rows newest ones.
SELECT occurred_at, id
FROM audit_events
ORDER BY occurred_at DESC, id DESC
LIMIT 1 OFFSET sqlc.arg(keep_rows)::bigint;

-- name: PruneAuditEvents :execrows
-- Deletes up to batch_size of the oldest events that are older than before or
-- at or below the row-count boundary. A NULL argument disables its rule.
DELETE FROM audit_events
WHERE id IN (
    SELECT id FROM audit_events
    WHERE (sqlc.narg(before)::timestamptz IS NOT NULL AND occurred_at < sqlc.narg(before))
       OR (sqlc.narg(bound_at)::timestamptz IS NOT NULL
           AND (occurred_at < sqlc.narg(bound_at)
             OR (occurred_at = sqlc.narg(bound_at) AND id <= sqlc.arg(bound_id)::text)))
    ORDER BY occurred_at ASC, id ASC
    LIMIT sqlc.arg(batch_size)::bigint
);

-- Scheduled reports (RFC 0008).

-- name: CreateReport :one
INSERT INTO reports (
    name, owner_id, source_kind, dashboard_id, tab_id, saved_query_id,
    schedule_json, timezone, time_range, range_anchor, variables_json,
    max_rows, skip_if_empty, attach_csv
) VALUES (
    sqlc.arg(name), sqlc.narg(owner_id), sqlc.arg(source_kind), sqlc.narg(dashboard_id),
    sqlc.narg(tab_id), sqlc.narg(saved_query_id), sqlc.arg(schedule_json), sqlc.arg(timezone),
    sqlc.narg(time_range), sqlc.arg(range_anchor), sqlc.arg(variables_json),
    sqlc.arg(max_rows), sqlc.arg(skip_if_empty), sqlc.arg(attach_csv)
)
RETURNING id;

-- name: GetReport :one
SELECT * FROM reports WHERE id = sqlc.arg(id);

-- name: ListReports :many
SELECT * FROM reports ORDER BY name, id;

-- name: ListReportsByOwner :many
SELECT * FROM reports WHERE owner_id = sqlc.arg(owner_id) ORDER BY name, id;

-- name: ListReportsForRecipient :many
-- A user receives a report through a user row, a team row of a team the
-- user belongs to, or an email row that matches the user's address.
SELECT r.* FROM reports r
WHERE EXISTS (
    SELECT 1 FROM report_recipients rr
    WHERE rr.report_id = r.id
      AND (
        (rr.kind = 'user' AND rr.user_id = sqlc.arg(user_id))
        OR (rr.kind = 'team' AND rr.team_id IN (
            SELECT tm.team_id FROM team_members tm WHERE tm.user_id = sqlc.arg(user_id)))
        OR (rr.kind = 'email' AND lower(rr.email) = (
            SELECT lower(u.email) FROM users u WHERE u.id = sqlc.arg(user_id)))
      )
)
ORDER BY r.name, r.id;

-- name: UpdateReport :execrows
UPDATE reports
SET name = sqlc.arg(name),
    source_kind = sqlc.arg(source_kind),
    dashboard_id = sqlc.narg(dashboard_id),
    tab_id = sqlc.narg(tab_id),
    saved_query_id = sqlc.narg(saved_query_id),
    schedule_json = sqlc.arg(schedule_json),
    timezone = sqlc.arg(timezone),
    time_range = sqlc.narg(time_range),
    range_anchor = sqlc.arg(range_anchor),
    variables_json = sqlc.arg(variables_json),
    max_rows = sqlc.arg(max_rows),
    skip_if_empty = sqlc.arg(skip_if_empty),
    attach_csv = sqlc.arg(attach_csv),
    updated_at = now()
WHERE id = sqlc.arg(id);

-- name: DeleteReport :execrows
DELETE FROM reports WHERE id = sqlc.arg(id);

-- name: PauseReport :execrows
UPDATE reports
SET state = 'paused',
    pause_reason = sqlc.arg(pause_reason),
    updated_at = now()
WHERE id = sqlc.arg(id);

-- name: ResumeReport :execrows
UPDATE reports
SET state = 'active',
    pause_reason = '',
    consecutive_failures = 0,
    updated_at = now()
WHERE id = sqlc.arg(id);

-- name: ClaimReport :execrows
-- A claim lends the new owner's access, so a pause for an inactive owner
-- no longer applies.
UPDATE reports
SET owner_id = sqlc.arg(owner_id),
    state = CASE WHEN pause_reason = 'owner_inactive' THEN 'active' ELSE state END,
    pause_reason = CASE WHEN pause_reason = 'owner_inactive' THEN '' ELSE pause_reason END,
    updated_at = now()
WHERE id = sqlc.arg(id);

-- name: TakeReportManualRunCooldown :execrows
-- clock_timestamp(), not now(): inside a transaction now() is the start of
-- the transaction, and the cooldown must use the time of this statement.
UPDATE reports
SET last_manual_run_at = clock_timestamp()
WHERE id = sqlc.arg(id)
  AND (last_manual_run_at IS NULL
       OR last_manual_run_at <= clock_timestamp() - make_interval(secs => sqlc.arg(window_seconds)::double precision));

-- name: TakeReportPreviewCooldown :execrows
UPDATE reports
SET last_preview_at = clock_timestamp()
WHERE id = sqlc.arg(id)
  AND (last_preview_at IS NULL
       OR last_preview_at <= clock_timestamp() - make_interval(secs => sqlc.arg(window_seconds)::double precision));

-- name: ListReportRecipients :many
SELECT * FROM report_recipients WHERE report_id = sqlc.arg(report_id) ORDER BY id;

-- name: LockReport :one
-- Holds the report row until the transaction ends, so two replacements of
-- its recipients run one after the other instead of merging.
SELECT id FROM reports WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: DeleteReportRecipients :exec
DELETE FROM report_recipients WHERE report_id = sqlc.arg(report_id);

-- name: InsertReportRecipient :exec
INSERT INTO report_recipients (report_id, kind, user_id, team_id, email, added_by)
VALUES (sqlc.arg(report_id), sqlc.arg(kind), sqlc.narg(user_id), sqlc.narg(team_id), sqlc.arg(email), sqlc.narg(added_by));

-- name: DeleteReportUserRecipient :execrows
DELETE FROM report_recipients
WHERE report_id = sqlc.arg(report_id) AND kind = 'user' AND user_id = sqlc.arg(user_id);

-- name: InsertRunningReportRun :one
-- A conflict on either partial unique index inserts nothing and returns no
-- row; the caller then tells the tick conflict from a run in flight.
INSERT INTO report_runs (report_id, trigger, scheduled_for, actor_id, status, started_at, deadline_at)
VALUES (
    sqlc.arg(report_id), sqlc.arg(trigger), sqlc.arg(scheduled_for), sqlc.narg(actor_id), 'running',
    clock_timestamp(),
    clock_timestamp() + make_interval(secs => sqlc.arg(deadline_seconds)::double precision)
)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: InsertSkippedReportTick :one
INSERT INTO report_runs (report_id, trigger, scheduled_for, status, reason, started_at, finished_at)
VALUES (
    sqlc.arg(report_id), 'schedule', sqlc.arg(scheduled_for), 'skipped', sqlc.arg(reason),
    clock_timestamp(), clock_timestamp()
)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: ReportTickExists :one
SELECT EXISTS(
    SELECT 1 FROM report_runs
    WHERE report_id = sqlc.arg(report_id) AND trigger = 'schedule'
      AND scheduled_for = sqlc.arg(scheduled_for)
);

-- name: FinishReportRun :one
UPDATE report_runs
SET status = sqlc.arg(status),
    reason = sqlc.arg(reason),
    window_start = sqlc.narg(window_start),
    window_end = sqlc.narg(window_end),
    panels_json = sqlc.arg(panels_json),
    deliveries_json = sqlc.arg(deliveries_json),
    deadline_at = NULL,
    finished_at = clock_timestamp()
WHERE id = sqlc.arg(id) AND status = 'running'
RETURNING report_id;

-- name: CountReportFailure :one
-- A failed run adds one to the streak; a sent or partial run ends it.
UPDATE reports
SET consecutive_failures = CASE sqlc.arg(status)::text
        WHEN 'failed' THEN consecutive_failures + 1
        WHEN 'sent' THEN 0
        WHEN 'partial' THEN 0
        ELSE consecutive_failures
    END
WHERE id = sqlc.arg(id)
RETURNING consecutive_failures;

-- name: PruneReportRuns :exec
DELETE FROM report_runs
WHERE report_runs.report_id = sqlc.arg(report_id)
  AND report_runs.status <> 'running'
  AND report_runs.id NOT IN (
    SELECT keep.id FROM report_runs keep
    WHERE keep.report_id = sqlc.arg(report_id)
    ORDER BY keep.id DESC
    LIMIT sqlc.arg(keep)
  );

-- name: ListReportRuns :many
-- before = 0 starts at the newest run.
SELECT * FROM report_runs
WHERE report_id = sqlc.arg(report_id)
  AND (sqlc.arg(before)::bigint = 0 OR id < sqlc.arg(before)::bigint)
ORDER BY id DESC
LIMIT sqlc.arg(max_runs)::bigint;

-- name: InterruptStaleReportRuns :execrows
UPDATE report_runs
SET status = 'interrupted',
    reason = 'process_stopped',
    deadline_at = NULL,
    finished_at = clock_timestamp()
WHERE status = 'running'
  AND deadline_at < clock_timestamp() - make_interval(secs => sqlc.arg(grace_seconds)::double precision);
