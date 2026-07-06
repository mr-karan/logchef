DROP INDEX IF EXISTS idx_collection_items_collection_order;
DROP INDEX IF EXISTS idx_collection_items_query;
DROP TABLE IF EXISTS collection_items;

DROP INDEX IF EXISTS idx_collection_members_user;
DROP TABLE IF EXISTS collection_members;

DROP INDEX IF EXISTS idx_collections_created_by;
DROP INDEX IF EXISTS idx_collections_one_personal_per_user;
DROP TABLE IF EXISTS collections;
