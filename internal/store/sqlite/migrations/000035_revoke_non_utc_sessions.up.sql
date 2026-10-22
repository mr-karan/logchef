-- Legacy local offsets can miscount active sessions and revoke unrelated logins.
DELETE FROM sessions WHERE expires_at NOT LIKE '%+0000 UTC%';
