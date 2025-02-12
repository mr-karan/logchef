-- Sources

-- name: CreateSource
-- Create a new source entry
-- $1: id
-- $2: schema_type
-- $3: host
-- $4: username
-- $5: password
-- $6: database
-- $7: table_name
-- $8: description
-- $9: ttl_days
INSERT INTO sources (
    id, schema_type, host, username, password, database, table_name, description, ttl_days, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'));

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
-- $1: schema_type
-- $2: host
-- $3: username
-- $4: password
-- $5: database
-- $6: table_name
-- $7: description
-- $8: ttl_days
-- $9: id
UPDATE sources
SET schema_type = ?,
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
-- $5: created_at
INSERT INTO users (id, email, full_name, role, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?);

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
-- $4: last_login_at
-- $5: last_active_at
-- $6: updated_at
-- $7: id
UPDATE users
SET email = ?,
    full_name = ?,
    role = ?,
    last_login_at = ?,
    last_active_at = ?,
    updated_at = ?
WHERE id = ?;

-- name: ListUsers
-- List all users
SELECT * FROM users ORDER BY created_at DESC;

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
-- $4: created_by
-- $5: created_at
INSERT INTO teams (id, name, description, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetTeam
-- Get a team by ID
-- $1: id
SELECT id, name, description, created_by, created_at, updated_at
FROM teams
WHERE id = ?;

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
DELETE FROM teams
WHERE id = ?;

-- name: ListTeams
-- List all teams
SELECT id, name, description, created_by, created_at, updated_at
FROM teams
ORDER BY created_at DESC;

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
SELECT team_id, user_id, role, created_at
FROM team_members
WHERE team_id = ? AND user_id = ?;

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
SELECT u.id, u.email, u.name, u.created_at, u.updated_at
FROM users u
JOIN team_members tm ON u.id = tm.user_id
WHERE tm.team_id = ?
ORDER BY u.name;

-- name: ListUserTeams
-- List all teams a user is a member of
-- $1: user_id
SELECT t.id, t.name, t.description, t.created_by, t.created_at, t.updated_at
FROM teams t
JOIN team_members tm ON t.id = tm.team_id
WHERE tm.user_id = ?
ORDER BY t.name;

-- Spaces

-- name: CreateSpace
-- Create a new space
-- $1: id
-- $2: name
-- $3: description
-- $4: created_by
-- $5: created_at
INSERT INTO spaces (id, name, description, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetSpace
-- Get a space by ID
-- $1: id
SELECT id, name, description, created_by, created_at, updated_at
FROM spaces
WHERE id = ?;

-- name: UpdateSpace
-- Update a space
-- $1: name
-- $2: description
-- $3: updated_at
-- $4: id
UPDATE spaces
SET name = ?,
    description = ?,
    updated_at = ?
WHERE id = ?;

-- name: DeleteSpace
-- Delete a space by ID
-- $1: id
DELETE FROM spaces
WHERE id = ?;

-- name: ListSpaces
-- List all spaces
SELECT id, name, description, created_by, created_at, updated_at
FROM spaces
ORDER BY name;

-- name: AddSpaceDataSource
-- Add a data source to a space
-- $1: space_id
-- $2: data_source_id
-- $3: created_at
INSERT INTO space_data_sources (space_id, data_source_id, created_at)
VALUES (?, ?, ?);

-- name: RemoveSpaceDataSource
-- Remove a data source from a space
-- $1: space_id
-- $2: data_source_id
DELETE FROM space_data_sources
WHERE space_id = ? AND data_source_id = ?;

-- name: ListSpaceDataSources
-- List all data sources in a space
-- $1: space_id
SELECT s.id, s.name, s.type, s.config, s.created_at, s.updated_at
FROM sources s
JOIN space_data_sources sds ON s.id = sds.data_source_id
WHERE sds.space_id = ?
ORDER BY s.name;

-- name: SetSpaceTeamAccess
-- Set a team's access to a space
-- $1: space_id
-- $2: team_id
-- $3: permission
-- $4: created_at
INSERT INTO space_team_access (space_id, team_id, permission, created_at, updated_at)
VALUES (?, ?, ?, ?, ?);

-- name: UpdateSpaceTeamAccess
-- Update a team's access to a space
-- $1: permission
-- $2: updated_at
-- $3: space_id
-- $4: team_id
UPDATE space_team_access
SET permission = ?,
    updated_at = ?
WHERE space_id = ? AND team_id = ?;

-- name: RemoveSpaceTeamAccess
-- Remove a team's access to a space
-- $1: space_id
-- $2: team_id
DELETE FROM space_team_access
WHERE space_id = ? AND team_id = ?;

-- name: GetSpaceTeamAccess
-- Get a team's access to a space
-- $1: space_id
-- $2: team_id
SELECT space_id, team_id, permission, created_at, updated_at
FROM space_team_access
WHERE space_id = ? AND team_id = ?;

-- name: ListSpaceTeamAccess
-- List all team access for a space
-- $1: space_id
SELECT t.id, t.name, t.description, t.created_by, t.created_at, t.updated_at, sta.permission
FROM teams t
JOIN space_team_access sta ON t.id = sta.team_id
WHERE sta.space_id = ?
ORDER BY t.name;

-- Queries

-- name: CreateQuery
-- Create a new query
-- $1: id
-- $2: space_id
-- $3: name
-- $4: description
-- $5: query_content
-- $6: created_by
-- $7: created_at
INSERT INTO queries (id, space_id, name, description, query_content, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetQuery
-- Get a query by ID
-- $1: id
SELECT id, space_id, name, description, query_content, created_by, created_at, updated_at
FROM queries
WHERE id = ?;

-- name: UpdateQuery
-- Update a query
-- $1: name
-- $2: description
-- $3: query_content
-- $4: updated_at
-- $5: id
UPDATE queries
SET name = ?,
    description = ?,
    query_content = ?,
    updated_at = ?
WHERE id = ?;

-- name: DeleteQuery
-- Delete a query by ID
-- $1: id
DELETE FROM queries
WHERE id = ?;

-- name: ListQueries
-- List all queries in a space
-- $1: space_id
SELECT id, space_id, name, description, query_content, created_by, created_at, updated_at
FROM queries
WHERE space_id = ?
ORDER BY name;