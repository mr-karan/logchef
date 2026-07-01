CREATE TABLE IF NOT EXISTS query_folders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    color TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_by INTEGER,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL,
    UNIQUE(team_id, name)
);

CREATE TABLE IF NOT EXISTS query_folder_items (
    folder_id INTEGER NOT NULL,
    query_id INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    added_by INTEGER,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (folder_id, query_id),
    FOREIGN KEY (folder_id) REFERENCES query_folders(id) ON DELETE CASCADE,
    FOREIGN KEY (query_id) REFERENCES team_queries(id) ON DELETE CASCADE,
    FOREIGN KEY (added_by) REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_query_folders_team_id ON query_folders(team_id);
CREATE INDEX IF NOT EXISTS idx_query_folders_team_sort ON query_folders(team_id, sort_order, name);
CREATE INDEX IF NOT EXISTS idx_query_folder_items_query_id ON query_folder_items(query_id);
CREATE INDEX IF NOT EXISTS idx_query_folder_items_folder_order ON query_folder_items(folder_id, sort_order);
