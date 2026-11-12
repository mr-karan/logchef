-- Every save is a compare-and-swap on this counter (RFC 0003 "Revisions").
ALTER TABLE dashboards ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;
