-- Saved views: a named Explorer setup (columns, sort, display mode, default
-- filter and time range) for one source. RFC 0007.
-- The CHECK ties the scope to its owner columns: a personal view has a user
-- and no team, a team view a team and no user, a source view neither.
-- A team view has no foreign key to team_sources on purpose: an unlink that
-- provisioning later undoes must not delete the team's views.
CREATE TABLE saved_views (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    scope TEXT NOT NULL,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    team_id INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    definition TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
    managed INTEGER NOT NULL DEFAULT 0 CHECK (managed IN (0, 1)),
    revision INTEGER NOT NULL DEFAULT 1,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    CHECK (
        (scope = 'personal' AND user_id IS NOT NULL AND team_id IS NULL)
        OR (scope = 'team' AND team_id IS NOT NULL AND user_id IS NULL)
        OR (scope = 'source' AND user_id IS NULL AND team_id IS NULL)
    )
);

-- A name is unique inside its scope and source; a scope has at most one
-- default per source.
CREATE UNIQUE INDEX idx_saved_views_personal_name ON saved_views(source_id, user_id, name) WHERE scope = 'personal';
CREATE UNIQUE INDEX idx_saved_views_team_name ON saved_views(source_id, team_id, name) WHERE scope = 'team';
CREATE UNIQUE INDEX idx_saved_views_source_name ON saved_views(source_id, name) WHERE scope = 'source';
CREATE UNIQUE INDEX idx_saved_views_personal_default ON saved_views(source_id, user_id) WHERE scope = 'personal' AND is_default = 1;
CREATE UNIQUE INDEX idx_saved_views_team_default ON saved_views(source_id, team_id) WHERE scope = 'team' AND is_default = 1;
CREATE UNIQUE INDEX idx_saved_views_source_default ON saved_views(source_id) WHERE scope = 'source' AND is_default = 1;
CREATE INDEX idx_saved_views_source ON saved_views(source_id);
-- The owner columns cascade or null on a user or team delete.
CREATE INDEX idx_saved_views_user ON saved_views(user_id);
CREATE INDEX idx_saved_views_team ON saved_views(team_id);
CREATE INDEX idx_saved_views_created_by ON saved_views(created_by);
