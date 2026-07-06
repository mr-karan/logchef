-- Drop all tables in reverse order of creation to handle foreign key constraints

DROP TABLE IF EXISTS queries;
DROP TABLE IF EXISTS space_team_access;
DROP TABLE IF EXISTS space_data_sources;
DROP TABLE IF EXISTS spaces;
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS teams;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS sources;