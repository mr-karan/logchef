-- Derived list columns, written with the blob in the same statement so the
-- list never reads panels_json (RFC 0003 "The dashboard list"). Defaults make
-- every existing row stale; the startup backfill fills them.
ALTER TABLE dashboards
    ADD COLUMN panel_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN tab_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN panel_types TEXT NOT NULL DEFAULT '[]',
    ADD COLUMN panel_team_ids TEXT NOT NULL DEFAULT '[]',
    ADD COLUMN panels_corrupt BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN summary_revision INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN summary_version INTEGER NOT NULL DEFAULT 0;
