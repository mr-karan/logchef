-- Every save is a compare-and-swap on this counter (RFC 0003 "Revisions").
ALTER TABLE dashboards ADD COLUMN revision BIGINT NOT NULL DEFAULT 1;
