-- Allow Loki sources (RFC 0011).
ALTER TABLE sources DROP CONSTRAINT sources_source_type_check;
ALTER TABLE sources ADD CONSTRAINT sources_source_type_check
    CHECK (source_type IN ('clickhouse', 'victorialogs', 'loki'));
