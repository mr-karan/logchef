-- Restore the two-value source_type CHECK. Adding it fails while a Loki
-- source exists: delete Loki sources first.
ALTER TABLE sources DROP CONSTRAINT sources_source_type_check;
ALTER TABLE sources ADD CONSTRAINT sources_source_type_check
    CHECK (source_type IN ('clickhouse', 'victorialogs'));
