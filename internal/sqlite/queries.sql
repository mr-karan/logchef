-- Sources

-- name: CreateSource
-- Create a new source entry
-- $1: name
-- $2: _meta_is_auto_created
-- $3: _meta_ts_field
-- $4: _meta_severity_field
-- $5: host
-- $6: username
-- $7: password
-- $8: database
-- $9: table_name
-- $10: description
-- $11: ttl_days
INSERT INTO sources (
    name, _meta_is_auto_created, _meta_ts_field, _meta_severity_field, host, username, password, database, table_name, description, ttl_days, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'));
SELECT last_insert_rowid();

-- name: GetSource
-- Get a single source by ID
-- $1: id
SELECT * FROM sources WHERE id = ?;

-- name: GetSourceByName
-- Get a single source by table name and database
-- $1: database
-- $2: table_name
SELECT * FROM sources WHERE database = ? AND table_name = ?;

-- name: ListSources
-- Get all sources ordered by creation date
SELECT * FROM sources ORDER BY created_at DESC;

-- name: UpdateSource
-- Update an existing source
-- $1: name
-- $2: _meta_is_auto_created
-- $3: _meta_ts_field
-- $4: _meta_severity_field
-- $5: host
-- $6: username
-- $7: password
-- $8: database
-- $9: table_name
-- $10: description
-- $11: ttl_days
-- $12: id
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

-- name: DeleteSource
-- Delete a source by ID
-- $1: id
DELETE FROM sources WHERE id = ?;

-- Users

-- name: CreateUser
-- Create a new user
-- $1: email
-- $2: full_name
-- $3: role
-- $4: status
-- $5: last_login_at
INSERT INTO users (email, full_name, role, status, last_login_at)
VALUES (?, ?, ?, ?, ?);
SELECT last_insert_rowid();

-- name: GetUser
-- Get a user by ID
-- $1: id
SELECT * FROM users WHERE id = ?;

-- name: GetUserByEmail
-- Get a user by email
-- $1: email
SELECT * FROM users WHERE email = ?;

-- name: UpdateUser
-- Update a user
-- $1: email
-- $2: full_name
-- $3: role
-- $4: status
-- $5: last_login_at
-- $6: last_active_at
-- $7: updated_at
-- $8: id
UPDATE users
SET email = ?,
    full_name = ?,
    role = ?,
    status = ?,
    last_login_at = ?,
    last_active_at = ?,
    updated_at = ?
WHERE id = ?;

-- name: ListUsers
-- List all users
SELECT * FROM users ORDER BY created_at ASC;

-- name: CountAdminUsers
-- Count active admin users
-- $1: role
-- $2: status
SELECT COUNT(*) FROM users WHERE role = ? AND status = ?;

-- Sessions

-- name: CreateSession
-- Create a new session
-- $1: user_id
-- $2: expires_at
-- $3: created_at
INSERT INTO sessions (id, user_id, expires_at, created_at)
VALUES (?, ?, ?, ?);
SELECT id FROM sessions WHERE rowid = last_insert_rowid();

-- name: GetSession
-- Get a session by ID
-- $1: id
SELECT * FROM sessions WHERE id = ?;

-- name: DeleteSession
-- Delete a session by ID
-- $1: id
DELETE FROM sessions WHERE id = ?;

-- name: DeleteUserSessions
-- Delete all sessions for a user
-- $1: user_id
DELETE FROM sessions WHERE user_id = ?;

-- name: CountUserSessions
-- Count active sessions for a user
-- $1: user_id
-- $2: current_time
SELECT COUNT(*) FROM sessions WHERE user_id = ? AND expires_at > ?;

-- Teams

-- name: CreateTeam
-- Create a new team
-- $1: name
-- $2: description
INSERT INTO teams (name, description)
VALUES (?, ?);
SELECT last_insert_rowid();

-- name: GetTeam
-- Get a team by ID
-- $1: id
SELECT * FROM teams WHERE id = ?;

-- name: UpdateTeam
-- Update a team
-- $1: name
-- $2: description
-- $3: updated_at
-- $4: id
UPDATE teams
SET name = ?,
    description = ?,
    updated_at = ?
WHERE id = ?;

-- name: DeleteTeam
-- Delete a team by ID
-- $1: id
DELETE FROM teams WHERE id = ?;

-- name: ListTeams
-- List all teams
SELECT * FROM teams ORDER BY created_at DESC;

-- Team Members

-- name: AddTeamMember
-- Add a member to a team
-- $1: team_id
-- $2: user_id
-- $3: role
INSERT INTO team_members (team_id, user_id, role)
VALUES (?, ?, ?);

-- name: GetTeamMember
-- Get a team member
-- $1: team_id
-- $2: user_id
SELECT * FROM team_members WHERE team_id = ? AND user_id = ?;

-- name: UpdateTeamMemberRole
-- Update a team member's role
-- $1: role
-- $2: team_id
-- $3: user_id
UPDATE team_members
SET role = ?
WHERE team_id = ? AND user_id = ?;

-- name: RemoveTeamMember
-- Remove a member from a team
-- $1: team_id
-- $2: user_id
DELETE FROM team_members
WHERE team_id = ? AND user_id = ?;

-- name: ListTeamMembers
-- List all members of a team
-- $1: team_id
SELECT tm.team_id, tm.user_id, tm.role, tm.created_at
FROM team_members tm
WHERE tm.team_id = ?
ORDER BY tm.created_at;

-- name: ListTeamMembersWithDetails
-- List all members of a team with user details
-- $1: team_id
SELECT tm.team_id, tm.user_id, tm.role, tm.created_at, u.email, u.full_name
FROM team_members tm
JOIN users u ON tm.user_id = u.id
WHERE tm.team_id = ?
ORDER BY tm.created_at ASC;

-- name: ListUserTeams
-- List all teams a user is a member of
-- $1: user_id
SELECT t.*
FROM teams t
JOIN team_members tm ON t.id = tm.team_id
WHERE tm.user_id = ?
ORDER BY t.name;

-- Team Sources

-- name: AddTeamSource
-- Add a data source to a team
-- $1: team_id
-- $2: source_id
INSERT INTO team_sources (team_id, source_id)
VALUES (?, ?);

-- name: RemoveTeamSource
-- Remove a data source from a team
-- $1: team_id
-- $2: source_id
DELETE FROM team_sources WHERE team_id = ? AND source_id = ?;

-- name: ListTeamSources
-- List all data sources in a team
-- $1: team_id
SELECT s.id, s.name, s.database, s.table_name, s.description, s.created_at
FROM sources s
JOIN team_sources ts ON s.id = ts.source_id
WHERE ts.team_id = ?
ORDER BY s.created_at DESC;

-- name: ListSourceTeams
-- List all teams a data source is a member of
-- $1: source_id
SELECT t.*
FROM teams t
JOIN team_sources ts ON t.id = ts.team_id
WHERE ts.source_id = ?
ORDER BY t.name;

-- Team Queries

-- name: CreateTeamQuery
-- Create a new query for a team
-- $1: team_id
-- $2: source_id
-- $3: name
-- $4: description
-- $5: query_type
-- $6: query_content
INSERT INTO team_queries (team_id, source_id, name, description, query_type, query_content)
VALUES (?, ?, ?, ?, ?, ?);
SELECT last_insert_rowid();

-- name: GetTeamQuery
-- Get a query by ID
-- $1: id
SELECT * FROM team_queries WHERE id = ?;

-- name: UpdateTeamQuery
-- Update a query for a team
-- $1: name
-- $2: description
-- $3: source_id
-- $4: query_type
-- $5: query_content
-- $6: id
UPDATE team_queries
SET name = ?,
    description = ?,
    source_id = ?,
    query_type = ?,
    query_content = ?,
    updated_at = datetime('now')
WHERE id = ?;

-- name: DeleteTeamQuery
-- Delete a query by ID
-- $1: id
DELETE FROM team_queries WHERE id = ?;

-- name: ListTeamQueries
-- List all queries in a team
-- $1: team_id
SELECT * FROM team_queries WHERE team_id = ? ORDER BY created_at DESC;

-- name: ListQueriesBySource
-- List all queries for a specific source
-- $1: source_id
SELECT * FROM team_queries WHERE source_id = ? ORDER BY created_at DESC;

-- name: ListQueriesByTeamAndSource
-- List all queries for a specific team and source
-- $1: team_id
-- $2: source_id
SELECT * FROM team_queries WHERE team_id = ? AND source_id = ? ORDER BY created_at DESC;

-- name: DeleteUser
-- Delete a user by ID
-- $1: id
DELETE FROM users WHERE id = ?;

-- name: GetTeamQueryWithAccess
-- Get a team query by ID and check if the user has access to it
-- $1: id
-- $2: user_id
SELECT tq.* FROM team_queries tq
JOIN team_members tm ON tq.team_id = tm.team_id
WHERE tq.id = ? AND tm.user_id = ?;

-- name: ListQueriesForUserAndTeam
-- List all queries for a specific team that a user has access to
-- $1: team_id
-- $2: user_id
SELECT tq.* FROM team_queries tq
JOIN team_members tm ON tq.team_id = tm.team_id
WHERE tq.team_id = ? AND tm.user_id = ?
ORDER BY tq.created_at DESC;

-- name: ListQueriesForUser
-- List all queries that a user has access to across all their teams
-- $1: user_id
SELECT tq.* FROM team_queries tq
JOIN team_members tm ON tq.team_id = tm.team_id
WHERE tm.user_id = ?
ORDER BY tq.created_at DESC;

-- name: ListQueriesForUserBySource
-- List all queries for a specific source that a user has access to
-- $1: user_id
-- $2: source_id
SELECT tq.* FROM team_queries tq
JOIN team_members tm ON tq.team_id = tm.team_id
WHERE tm.user_id = ? AND tq.source_id = ?
ORDER BY tq.created_at DESC;

-- Additional queries for user-source and team-source access

-- name: TeamHasSource
-- Check if a team has access to a source
-- $1: team_id
-- $2: source_id
SELECT COUNT(*) FROM team_sources
WHERE team_id = ? AND source_id = ?;

-- name: UserHasSourceAccess
-- Check if a user has access to a source through any team
-- $1: user_id
-- $2: source_id
SELECT COUNT(*) FROM team_members tm
JOIN team_sources ts ON tm.team_id = ts.team_id
WHERE tm.user_id = ? AND ts.source_id = ?;

-- name: ListTeamsForUser
-- List all teams a user is a member of
-- $1: user_id
SELECT t.* FROM teams t
JOIN team_members tm ON t.id = tm.team_id
WHERE tm.user_id = ?
ORDER BY t.created_at DESC;

-- name: GetTeamByName
-- Get a team by its name
-- $1: name
SELECT * FROM teams WHERE name = ?;
