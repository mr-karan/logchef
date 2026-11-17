-- header_secret_refs: the environment variable that provided each custom
-- header value of a provisioned source, as a JSON object of header name to
-- variable name, so an export keeps the declared names.
ALTER TABLE sources ADD COLUMN header_secret_refs TEXT
    CHECK (header_secret_refs IS NULL OR json_type(header_secret_refs) = 'object');
