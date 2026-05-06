-- WARNING: irreversible. Removes the team-scoped Query Folders feature shipped
-- briefly in v1.6.0-dev. Folder rows and membership rows are dropped without
-- backup; the down migration only re-creates the empty schema. If you need
-- folder data preserved, snapshot query_folders + query_folder_items before
-- running this migration.

DROP INDEX IF EXISTS idx_query_folder_items_folder_order;
DROP INDEX IF EXISTS idx_query_folder_items_query_id;
DROP INDEX IF EXISTS idx_query_folders_team_sort;
DROP INDEX IF EXISTS idx_query_folders_team_id;
DROP TABLE IF EXISTS query_folder_items;
DROP TABLE IF EXISTS query_folders;
