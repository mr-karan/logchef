-- Dashboards: a saved grid of visualization panels. The panel layout, per-panel
-- source/query/type and options live in a single versioned JSON blob
-- (panels_json), validated in the application layer (models.ValidateDashboardPanels).
-- created_by is nulled (not cascaded) when the author is deleted so dashboards
-- survive user removal, mirroring saved_queries/alerts.
CREATE TABLE dashboards (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT,
    panels_json TEXT NOT NULL,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_dashboards_created_by ON dashboards(created_by);
CREATE INDEX IF NOT EXISTS idx_dashboards_updated_at ON dashboards(updated_at);
