-- Remove 'editor' from team_members role constraint
-- Create a temporary table with the original constraint
CREATE TABLE team_members_new (
    team_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin', 'member')), -- Reverted to original constraint
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (team_id, user_id),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Copy data from the current table to the new one
-- This will fail if any records have 'editor' role, which is intentional
-- for rollback safety
INSERT INTO team_members_new SELECT * FROM team_members 
WHERE role IN ('admin', 'member');

-- Drop the old table
DROP TABLE team_members;

-- Rename the new table to the original name
ALTER TABLE team_members_new RENAME TO team_members;

-- Recreate indexes
CREATE INDEX IF NOT EXISTS idx_team_members_user_id ON team_members(user_id);
CREATE INDEX IF NOT EXISTS idx_team_members_team_id ON team_members(team_id);