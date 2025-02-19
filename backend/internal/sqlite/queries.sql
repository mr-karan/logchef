-- Sources

-- name: CreateSource
-- Create a new source entry
-- $1: id
-- $2: _meta_is_auto_created
-- $3: _meta_ts_field
-- $4: host
-- $5: username
-- $6: password
-- $7: database
-- $8: table_name
-- $9: description
-- $10: ttl_days
INSERT INTO sources (
    id, _meta_is_auto_created, _meta_ts_field, host, username, password, database, table_name, description, ttl_days, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'));

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
-- $1: _meta_is_auto_created
-- $2: _meta_ts_field
-- $3: host
-- $4: username
-- $5: password
-- $6: database
-- $7: table_name
-- $8: description
-- $9: ttl_days
-- $10: id
UPDATE sources
SET _meta_is_auto_created = ?,
    _meta_ts_field = ?,
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
-- $1: id
-- $2: email
-- $3: full_name
-- $4: role
-- $5: status
-- $6: last_login_at
-- $7: created_at
-- $8: updated_at
INSERT INTO users (id, email, full_name, role, status, last_login_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

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
SELECT * FROM users ORDER BY created_at DESC;

-- name: CountAdminUsers
-- Count active admin users
-- $1: role
-- $2: status
SELECT COUNT(*) FROM users WHERE role = ? AND status = ?;

-- Sessions

-- name: CreateSession
-- Create a new session
-- $1: id
-- $2: user_id
-- $3: expires_at
-- $4: created_at
INSERT INTO sessions (id, user_id, expires_at, created_at)
VALUES (?, ?, ?, ?);

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
-- $1: id
-- $2: name
-- $3: description
-- $4: created_at
-- $5: updated_at
INSERT INTO teams (id, name, description, created_at, updated_at)
VALUES (?, ?, ?, datetime('now'), datetime('now'));

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
-- $4: created_at
INSERT INTO team_members (team_id, user_id, role, created_at)
VALUES (?, ?, ?, ?);

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
-- $3: created_at
INSERT INTO team_sources (team_id, source_id, created_at)
VALUES (?, ?, ?);

-- name: RemoveTeamSource
-- Remove a data source from a team
-- $1: team_id
-- $2: source_id
DELETE FROM team_sources WHERE team_id = ? AND source_id = ?;

-- name: ListTeamSources
-- List all data sources in a team
-- $1: team_id
SELECT s.id, s.database, s.table_name, s.description, s.created_at
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
-- $1: id
-- $2: team_id
-- $3: name
-- $4: description
-- $5: query_content
-- $6: created_at
-- $7: updated_at
INSERT INTO team_queries (id, team_id, name, description, query_content, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetTeamQuery
-- Get a query by ID
-- $1: id
SELECT * FROM team_queries WHERE id = ?;

-- name: UpdateTeamQuery
-- Update a query for a team
-- $1: name
-- $2: description
-- $3: query_content
-- $4: updated_at
-- $5: id
UPDATE team_queries
SET name = ?,
    description = ?,
    query_content = ?,
    updated_at = ?
WHERE id = ?;

-- name: DeleteTeamQuery
-- Delete a query by ID
-- $1: id
DELETE FROM team_queries WHERE id = ?;

-- name: ListTeamQueries
-- List all queries in a team
-- $1: team_id
SELECT * FROM team_queries WHERE team_id = ? ORDER BY created_at DESC;

-- name: DeleteUser
-- Delete a user by ID
-- $1: id
DELETE FROM users WHERE id = ?;