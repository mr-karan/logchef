-- RFC 0008 unit R8: a report sends to its recipient rows. A report saved
-- before recipient rows existed was sent to its owner, so it gets the owner
-- as its one user row. A report with any row, or without an owner, is left
-- as it is, so running this twice adds nothing.
INSERT INTO report_recipients (report_id, kind, user_id, email, added_by)
SELECT r.id, 'user', r.owner_id, '', r.owner_id
FROM reports r
WHERE r.owner_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM report_recipients rr WHERE rr.report_id = r.id);
