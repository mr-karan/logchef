-- Notification destinations (RFC 0006 unit D1). A rule's email recipients and
-- webhook URLs become reusable destinations linked to the rule, and the two
-- JSON columns on alerts are dropped.

CREATE TABLE notification_destinations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('email', 'webhook', 'slack', 'pagerduty')),
    scope TEXT NOT NULL CHECK (scope IN ('private', 'team', 'global')),
    team_id INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    config_json TEXT NOT NULL DEFAULT '{}',
    secret TEXT NOT NULL DEFAULT '',
    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    last_delivery_at DATETIME,
    last_delivery_status TEXT,
    last_delivery_error TEXT,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    CHECK ((scope = 'team') = (team_id IS NOT NULL))
);

CREATE INDEX idx_notification_destinations_team ON notification_destinations(team_id);
CREATE INDEX idx_notification_destinations_created_by ON notification_destinations(created_by);

CREATE TABLE alert_destinations (
    alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    destination_id INTEGER NOT NULL REFERENCES notification_destinations(id) ON DELETE RESTRICT,
    PRIMARY KEY (alert_id, destination_id)
);

CREATE INDEX idx_alert_destinations_destination ON alert_destinations(destination_id);

-- Each distinct webhook URL of one creator becomes one private version 1
-- webhook destination named after the URL's host (credentials, port, path
-- and query left out of the name).
CREATE TEMP TABLE migrated_webhooks AS
SELECT DISTINCT a.created_by AS created_by, trim(j.value) AS url
FROM alerts a, json_each(CASE WHEN json_valid(a.webhook_urls_json) THEN CASE WHEN json_type(a.webhook_urls_json) = 'array' THEN a.webhook_urls_json END END) j
WHERE j.type = 'text' AND trim(j.value) <> '';

-- The name carries the host and never a byte of the userinfo: the authority
-- ends at the first '/', '?' or '#', and the userinfo ends at its last '@'.
-- An '@' after the authority means the userinfo held one of those
-- delimiters, so the authority is unclear and the name has no host. An
-- unparsable host also leaves the plain name.
INSERT INTO notification_destinations (name, type, scope, config_json, secret, created_by)
SELECT
    CASE WHEN host.name = '' THEN 'Webhook' ELSE 'Webhook ' || host.name END,
    'webhook', 'private', '{"payload_version":1}', w.url, w.created_by
FROM migrated_webhooks w
JOIN (
    SELECT url,
        CASE
            WHEN unclear THEN ''
            WHEN substr(hostport, 1, 1) = '[' THEN substr(hostport, 1, instr(hostport, ']'))
            WHEN instr(hostport, ':') > 0 THEN substr(hostport, 1, instr(hostport, ':') - 1)
            ELSE hostport
        END AS name
    FROM (
        SELECT url,
            instr(substr(rest, length(authority) + 1), '@') > 0 AS unclear,
            -- rtrim drops the trailing characters other than '@', so its
            -- length is the position of the last '@' (0 when there is none).
            substr(authority, length(rtrim(authority, replace(authority, '@', ''))) + 1) AS hostport
        FROM (
            SELECT url, rest,
                substr(rest, 1, MIN(
                    CASE WHEN instr(rest, '/') > 0 THEN instr(rest, '/') ELSE length(rest) + 1 END,
                    CASE WHEN instr(rest, '?') > 0 THEN instr(rest, '?') ELSE length(rest) + 1 END,
                    CASE WHEN instr(rest, '#') > 0 THEN instr(rest, '#') ELSE length(rest) + 1 END
                ) - 1) AS authority
            FROM (SELECT DISTINCT url, CASE WHEN instr(url, '://') > 0 THEN substr(url, instr(url, '://') + 3) ELSE url END AS rest FROM migrated_webhooks)
        )
    )
) host ON host.url = w.url
ORDER BY w.created_by, w.url;

INSERT INTO alert_destinations (alert_id, destination_id)
SELECT DISTINCT a.id, d.id
FROM alerts a, json_each(CASE WHEN json_valid(a.webhook_urls_json) THEN CASE WHEN json_type(a.webhook_urls_json) = 'array' THEN a.webhook_urls_json END END) j
JOIN notification_destinations d
    ON d.type = 'webhook' AND d.secret = trim(j.value) AND d.created_by IS a.created_by
WHERE j.type = 'text';

DROP TABLE migrated_webhooks;

-- Each rule with recipients gets one private email destination with the
-- same user ids. Its id is the highest destination id so far plus the rule
-- id, which links it to the rule without a lookup by name.
CREATE TEMP TABLE migrated_recipients AS
SELECT DISTINCT a.id AS alert_id, CAST(j.value AS INTEGER) AS user_id
FROM alerts a, json_each(CASE WHEN json_valid(a.recipient_user_ids_json) THEN CASE WHEN json_type(a.recipient_user_ids_json) = 'array' THEN a.recipient_user_ids_json END END) j
WHERE j.type = 'integer';

CREATE TEMP TABLE migrated_email_base AS
SELECT COALESCE(MAX(id), 0) AS base FROM notification_destinations;

INSERT INTO notification_destinations (id, name, type, scope, config_json, created_by)
SELECT
    (SELECT base FROM migrated_email_base) + a.id,
    'Email for ' || a.name,
    'email', 'private',
    json_object('user_ids', (
        SELECT json_group_array(user_id) FROM (
            SELECT r.user_id FROM migrated_recipients r WHERE r.alert_id = a.id ORDER BY r.user_id
        )
    )),
    a.created_by
FROM alerts a
WHERE a.id IN (SELECT alert_id FROM migrated_recipients)
ORDER BY a.id;

INSERT INTO alert_destinations (alert_id, destination_id)
SELECT DISTINCT r.alert_id, (SELECT base FROM migrated_email_base) + r.alert_id
FROM migrated_recipients r;

DROP TABLE migrated_recipients;
DROP TABLE migrated_email_base;

ALTER TABLE alerts DROP COLUMN recipient_user_ids_json;
ALTER TABLE alerts DROP COLUMN webhook_urls_json;
