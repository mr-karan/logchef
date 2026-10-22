CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_teams_name ON teams(name);
CREATE INDEX IF NOT EXISTS idx_team_members_team_id ON team_members(team_id);
CREATE INDEX IF NOT EXISTS idx_team_sources_team_id ON team_sources(team_id);
CREATE INDEX IF NOT EXISTS idx_query_stats_daily_date ON query_stats_daily(bucket_date);
