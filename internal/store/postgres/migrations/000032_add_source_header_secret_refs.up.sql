-- header_secret_refs: the environment variable that provided each custom
-- header value of a provisioned source, as a JSON object of header name to
-- variable name, so an export keeps the declared names.
ALTER TABLE sources ADD COLUMN header_secret_refs JSONB
    CHECK (header_secret_refs IS NULL OR jsonb_typeof(header_secret_refs) = 'object');
