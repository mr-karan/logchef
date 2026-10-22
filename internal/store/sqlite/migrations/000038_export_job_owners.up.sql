ALTER TABLE export_jobs ADD COLUMN owner_instance_id TEXT NOT NULL DEFAULT '';
ALTER TABLE export_jobs ADD COLUMN owner_process_id TEXT NOT NULL DEFAULT '';

UPDATE export_jobs
SET
    expires_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', CASE
        WHEN instr(expires_at, ' +') > 0 THEN
            substr(expires_at, 1, instr(expires_at, ' +') - 1) ||
            substr(expires_at, instr(expires_at, ' +') + 1, 3) || ':' ||
            substr(expires_at, instr(expires_at, ' +') + 4, 2)
        WHEN instr(expires_at, ' -') > 0 THEN
            substr(expires_at, 1, instr(expires_at, ' -') - 1) ||
            substr(expires_at, instr(expires_at, ' -') + 1, 3) || ':' ||
            substr(expires_at, instr(expires_at, ' -') + 4, 2)
        ELSE expires_at
    END), expires_at),
    completed_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', CASE
        WHEN instr(completed_at, ' +') > 0 THEN
            substr(completed_at, 1, instr(completed_at, ' +') - 1) ||
            substr(completed_at, instr(completed_at, ' +') + 1, 3) || ':' ||
            substr(completed_at, instr(completed_at, ' +') + 4, 2)
        WHEN instr(completed_at, ' -') > 0 THEN
            substr(completed_at, 1, instr(completed_at, ' -') - 1) ||
            substr(completed_at, instr(completed_at, ' -') + 1, 3) || ':' ||
            substr(completed_at, instr(completed_at, ' -') + 4, 2)
        ELSE completed_at
    END), completed_at),
    created_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', CASE
        WHEN instr(created_at, ' +') > 0 THEN
            substr(created_at, 1, instr(created_at, ' +') - 1) ||
            substr(created_at, instr(created_at, ' +') + 1, 3) || ':' ||
            substr(created_at, instr(created_at, ' +') + 4, 2)
        WHEN instr(created_at, ' -') > 0 THEN
            substr(created_at, 1, instr(created_at, ' -') - 1) ||
            substr(created_at, instr(created_at, ' -') + 1, 3) || ':' ||
            substr(created_at, instr(created_at, ' -') + 4, 2)
        ELSE created_at
    END), created_at),
    updated_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', CASE
        WHEN instr(updated_at, ' +') > 0 THEN
            substr(updated_at, 1, instr(updated_at, ' +') - 1) ||
            substr(updated_at, instr(updated_at, ' +') + 1, 3) || ':' ||
            substr(updated_at, instr(updated_at, ' +') + 4, 2)
        WHEN instr(updated_at, ' -') > 0 THEN
            substr(updated_at, 1, instr(updated_at, ' -') - 1) ||
            substr(updated_at, instr(updated_at, ' -') + 1, 3) || ':' ||
            substr(updated_at, instr(updated_at, ' -') + 4, 2)
        ELSE updated_at
    END), updated_at);
