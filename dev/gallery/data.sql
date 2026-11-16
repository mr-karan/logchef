-- Synthetic data for the dashboard gallery, in its own database.
-- Every timestamp is relative to now(), so a rerun moves the data forward.
-- provision.ts runs each statement in order, split on ";" at line end.

CREATE DATABASE IF NOT EXISTS gallery;

DROP TABLE IF EXISTS gallery.http_requests;

CREATE TABLE gallery.http_requests
(
    timestamp DateTime64(3),
    service LowCardinality(String),
    method LowCardinality(String),
    route LowCardinality(String),
    status UInt16,
    duration_ms UInt32,
    bytes UInt32,
    region LowCardinality(String),
    level LowCardinality(String),
    trace_id String,
    msg String
)
ENGINE = MergeTree
PARTITION BY toDate(timestamp)
ORDER BY (service, timestamp);

-- One row per request. Each minute of the last 7 days gets a request count
-- from an IST day curve (low at 04:00, high at 16:00, weekends at 80%) and a
-- per-day factor, since per-minute noise averages out over a day.
-- Two incidents: checkout and payments fail 3.5 hours ago, search 3 days ago.
-- The first stays out of the last two whole hours that the hourly stats compare.
INSERT INTO gallery.http_requests
SELECT
    minute + toIntervalMillisecond(rand(1) % 60000) AS timestamp,
    routes[r].2 AS service,
    routes[r].3 AS method,
    routes[r].1 AS route,
    multiIf(
        roll < p5xx, [500, 502, 503, 503, 504, 504][1 + rand(4) % 6],
        roll < p5xx + 0.028, if(service = 'auth', [401, 401, 403, 429][1 + rand(5) % 4], [400, 404, 404, 409, 429][1 + rand(5) % 5]),
        method = 'GET' AND roll > 0.95, 304,
        method = 'POST' AND service IN ('cart', 'checkout'), 201,
        200
    ) AS status,
    toUInt32(routes[r].4 * exp(randNormal(0, 0.45, 6)) * regions[g].2
        * if(rand(7) % 100 = 0, 6, 1)
        * multiIf(incident = 1 AND service IN ('checkout', 'payments'), 4.5, incident = 2 AND service = 'search', 3, 1)) AS duration_ms,
    toUInt32(if(method = 'GET', 1200 + randExponential(1 / 9000, 8), 300 + randExponential(1 / 900, 8))) AS bytes,
    regions[g].1 AS region,
    multiIf(status >= 500, 'error', status >= 400, 'warn', 'info') AS level,
    lower(hex(rand64(9))) AS trace_id,
    multiIf(
        status >= 500 AND service = 'payments', ['payment gateway timeout after 3000 ms', 'card network returned 502 for authorize', 'idempotency store unavailable'][1 + rand(10) % 3],
        status >= 500 AND service = 'checkout', ['inventory reservation failed: deadline exceeded', 'pricing service returned 503', 'order write conflict, retry budget spent'][1 + rand(10) % 3],
        status >= 500 AND service = 'search', ['search cluster returned 503: no shard available', 'query timed out after 2000 ms'][1 + rand(10) % 2],
        status >= 500, ['upstream connect error: connection reset by peer', 'circuit breaker open for upstream', 'worker pool exhausted'][1 + rand(10) % 3],
        status >= 400, 'client error',
        'request completed'
    ) AS msg
FROM
(
    SELECT
        minute,
        incident,
        multiIf(
            incident = 1 AND r IN (4, 5), 0.32,
            incident = 1, 0.012,
            incident = 2 AND r = 2, 0.14,
            0.0035
        ) AS p5xx,
        randCanonical(3) AS roll,
        rand(2) % 100 AS pr,
        rand(12) % 100 AS pg,
        arrayFirstIndex(c -> c > pr, [28, 46, 60, 66, 71, 78, 90, 100]) AS r,
        arrayFirstIndex(c -> c > pg, [56, 81, 100]) AS g,
        [('/api/v1/products', 'catalog', 'GET', 45), ('/api/v1/search', 'search', 'GET', 120),
         ('/api/v1/cart', 'cart', 'POST', 70), ('/api/v1/checkout', 'checkout', 'POST', 260),
         ('/api/v1/payments/authorize', 'payments', 'POST', 380), ('/api/v1/login', 'auth', 'POST', 55),
         ('/api/v1/orders/:id', 'orders', 'GET', 90), ('/api/v1/recommendations', 'recs', 'GET', 170)] AS routes,
        [('ap-south-1', 1.0), ('eu-west-1', 1.3), ('us-east-1', 1.45)] AS regions
    FROM
    (
        SELECT
            minute,
            ago,
            multiIf(ago BETWEEN 200 AND 228, 1, ago BETWEEN 4300 AND 4330, 2, 0) AS incident,
            toUInt32(150 * (0.55 - 0.45 * cos(2 * pi() * (hour - 4) / 24))
                * if(toDayOfWeek(minute, 0, 'Asia/Kolkata') >= 6, 0.8, 1)
                * (1 + 0.06 * sin(2.7 * toRelativeDayNum(minute, 'Asia/Kolkata')))
                * (0.92 + 0.16 * randCanonical(13))) AS n
        FROM
        (
            SELECT
                toStartOfMinute(now()) - toIntervalMinute(number + 1) AS minute,
                number + 1 AS ago,
                toHour(minute, 'Asia/Kolkata') + toMinute(minute, 'Asia/Kolkata') / 60 AS hour
            FROM numbers(10079)
        )
    )
    ARRAY JOIN range(n) AS i
);

DROP TABLE IF EXISTS gallery.orders;

CREATE TABLE gallery.orders
(
    created_at DateTime64(3),
    order_id String,
    city LowCardinality(String),
    payment_method LowCardinality(String),
    category LowCardinality(String),
    items UInt8,
    amount Decimal(18, 2),
    status LowCardinality(String)
)
ENGINE = MergeTree
PARTITION BY toDate(created_at)
ORDER BY created_at;

-- Orders peak at 21:00 IST, rise about 2% a day, and carry INR amounts.
INSERT INTO gallery.orders
SELECT
    minute + toIntervalMillisecond(rand(1) % 60000) AS created_at,
    concat('ORD-', toString(100000000 + rand(2) % 900000000)) AS order_id,
    ['Mumbai', 'Bengaluru', 'Delhi', 'Hyderabad', 'Pune', 'Chennai', 'Kolkata', 'Ahmedabad'][arrayFirstIndex(c -> c > pc, [22, 42, 59, 70, 79, 87, 94, 100])] AS city,
    ['UPI', 'Card', 'Cash on delivery', 'Net banking', 'Wallet'][arrayFirstIndex(c -> c > pp, [52, 73, 84, 93, 100])] AS payment_method,
    categories[k].1 AS category,
    toUInt8(1 + rand(5) % 4) AS items,
    toDecimal64(round(categories[k].2 * exp(randNormal(0, 0.5, 6)), 2), 2) AS amount,
    ['paid', 'refunded', 'failed'][arrayFirstIndex(c -> c > ps, [940, 975, 1000])] AS status
FROM
(
    SELECT
        minute,
        rand(3) % 100 AS pc,
        rand(4) % 100 AS pp,
        rand(7) % 1000 AS ps,
        rand(8) % 100 AS pk,
        arrayFirstIndex(c -> c > pk, [18, 44, 66, 84, 100]) AS k,
        [('Electronics', 8200), ('Fashion', 1700), ('Grocery', 780), ('Home', 2900), ('Beauty', 950)] AS categories
    FROM
    (
        SELECT
            minute,
            toUInt32(68 * (0.12 + 0.88 * pow(0.5 + 0.5 * cos(2 * pi() * (hour - 21) / 24), 1.4))
                * if(toDayOfWeek(minute, 0, 'Asia/Kolkata') >= 6, 1.15, 1)
                * (1 - 0.02 * ago / 1440)
                * (0.9 + 0.2 * randCanonical(9))) AS n
        FROM
        (
            SELECT
                toStartOfMinute(now()) - toIntervalMinute(number + 1) AS minute,
                number + 1 AS ago,
                toHour(minute, 'Asia/Kolkata') + toMinute(minute, 'Asia/Kolkata') / 60 AS hour
            FROM numbers(10079)
        )
    )
    ARRAY JOIN range(n) AS i
);
