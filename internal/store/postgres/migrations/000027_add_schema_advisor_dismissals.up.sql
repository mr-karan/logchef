-- Schema advisor findings that an admin dismissed (RFC 0010). A finding id is
-- stable across refreshes, so a dismissal outlives the report it came from.
-- dismissed_by is NULL once the user is gone.
CREATE TABLE schema_advisor_dismissals (
    source_id    BIGINT      NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    finding_id   TEXT        NOT NULL,
    reason       TEXT        NOT NULL DEFAULT '',
    dismissed_by BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    dismissed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (source_id, finding_id)
);

-- dismissed_by is nulled on a user delete.
CREATE INDEX idx_schema_advisor_dismissals_dismissed_by ON schema_advisor_dismissals(dismissed_by);
