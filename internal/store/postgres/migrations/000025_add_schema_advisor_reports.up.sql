-- The last schema advisor report of each source (RFC 0010). A refresh
-- replaces the row. report is the JSON of models.SchemaAdvisorReport, at most
-- 256 KiB. generated_by is NULL for a scheduled run, or once the user is gone.
CREATE TABLE schema_advisor_reports (
    source_id    BIGINT PRIMARY KEY REFERENCES sources(id) ON DELETE CASCADE,
    generated_at TIMESTAMPTZ NOT NULL,
    generated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    window_days  BIGINT  NOT NULL,
    report       TEXT    NOT NULL
);

-- generated_by is nulled on a user delete.
CREATE INDEX idx_schema_advisor_reports_generated_by ON schema_advisor_reports(generated_by);
