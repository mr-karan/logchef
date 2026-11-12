-- A collection link has a kind. A pin is a bookmark and grants nothing. A
-- managed link delegates edit on the saved query to the collection's owners
-- and editors. Existing links keep delegation only when a shared collection
-- holds them and the query's creator or a global admin added them. Every
-- other link, including one whose adder was deleted, becomes a pin.
ALTER TABLE collection_items ADD COLUMN kind TEXT NOT NULL DEFAULT 'pin' CHECK (kind IN ('pin', 'managed'));

UPDATE collection_items
SET kind = 'managed'
WHERE collection_id IN (SELECT id FROM collections WHERE is_personal = false)
  AND added_by IS NOT NULL
  AND (added_by = (SELECT q.created_by FROM saved_queries q
                   WHERE q.id = collection_items.saved_query_id)
       OR added_by IN (SELECT id FROM users WHERE role = 'admin'));
