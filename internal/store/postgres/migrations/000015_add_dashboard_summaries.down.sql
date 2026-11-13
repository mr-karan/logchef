ALTER TABLE dashboards
    DROP COLUMN summary_version,
    DROP COLUMN summary_revision,
    DROP COLUMN panels_corrupt,
    DROP COLUMN panel_team_ids,
    DROP COLUMN panel_types,
    DROP COLUMN tab_count,
    DROP COLUMN panel_count;
