#!/usr/bin/env bun
// Builds the dashboard gallery: synthetic data in ClickHouse and VictoriaLogs,
// a "Storefront" team with three sources, and three dashboards. Safe to rerun:
// the data is regenerated relative to now and each dashboard is replaced by name.

import type {
  Dashboard,
  DashboardPanelQuery,
  DashboardPanels,
  DashboardQueryPanel,
  DashboardTab,
  DashboardViz,
  NumberFormat,
} from "../../frontend/src/api/dashboards";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const env = (name: string, fallback: string) => process.env[name] || fallback;
const API = env("LOGCHEF_URL", "http://localhost:8125") + "/api/v1";
const TOKEN = env("LOGCHEF_TOKEN", "logchef_1_devsetuptoken00000000000000");
const CLICKHOUSE = env("CLICKHOUSE_URL", "http://localhost:8123");
const VICTORIALOGS = env("VICTORIALOGS_URL", "http://localhost:9428");
const VIEWER = env("GALLERY_VIEWER", "admin@logchef.internal");
// Addresses the Logchef server uses to reach the stores, as in dev/provisioning.toml.
const SOURCE_CLICKHOUSE_HOST = env("GALLERY_CLICKHOUSE_HOST", "127.0.0.1:9000");
const SOURCE_VICTORIALOGS_URL = env("GALLERY_VICTORIALOGS_URL", "http://127.0.0.1:9428");

const DASHBOARD_HEADERS = {
  "X-Logchef-Dashboard-Schema": "2",
  "X-Logchef-Dashboard-Features":
    "result_frames,formats,result_charts,stat_compare,thresholds,table_rules,column_formats,tabs,free_grid,panel_time",
};
const here = import.meta.dir;

async function api<T>(method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<T> {
  const res = await fetch(API + path, {
    method,
    headers: { Authorization: `Bearer ${TOKEN}`, "Content-Type": "application/json", ...headers },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path} returned ${res.status}: ${text}`);
  return (text ? JSON.parse(text).data : undefined) as T;
}

async function clickhouse(sql: string): Promise<string> {
  const res = await fetch(CLICKHOUSE + "/", { method: "POST", body: sql });
  const text = await res.text();
  if (!res.ok) throw new Error(`ClickHouse returned ${res.status} for:\n${sql.slice(0, 200)}\n${text}`);
  return text;
}

async function loadClickHouse() {
  const script = readFileSync(join(here, "data.sql"), "utf8").replace(/--.*$/gm, "");
  for (const statement of script.split(/;\s*$/m).map((s) => s.trim()).filter(Boolean)) {
    await clickhouse(statement);
  }
  const counts = await clickhouse(
    "SELECT (SELECT count() FROM gallery.http_requests), (SELECT count() FROM gallery.orders) FORMAT TSV",
  );
  const [requests, orders] = counts.trim().split("\t");
  console.error(`clickhouse: ${requests} requests, ${orders} orders`);
}

// VictoriaLogs here cannot delete, so each run writes to a new tenant and the
// source moves to it. Old tenants age out with the 7-day retention.
async function loadVictoriaLogs(projectID: string) {
  const lines = await clickhouse(readFileSync(join(here, "victorialogs.sql"), "utf8"));
  const params = "_stream_fields=service,env,namespace&_time_field=timestamp&_msg_field=message";
  const res = await fetch(`${VICTORIALOGS}/insert/jsonline?${params}`, {
    method: "POST",
    headers: { "Content-Type": "application/stream+json", AccountID: "0", ProjectID: projectID },
    body: lines,
  });
  if (!res.ok) throw new Error(`VictoriaLogs insert returned ${res.status}: ${await res.text()}`);
  const expected = lines.split("\n").filter(Boolean).length;
  for (let attempt = 0; attempt < 30; attempt++) {
    const count = await fetch(`${VICTORIALOGS}/select/logsql/query`, {
      method: "POST",
      headers: { AccountID: "0", ProjectID: projectID, "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ query: "* | stats count() as n" }),
    }).then((r) => r.text());
    const n = Number(JSON.parse(count.trim() || '{"n":"0"}').n);
    if (n === expected) {
      console.error(`victorialogs: ${n} log lines in project ${projectID}`);
      return;
    }
    await Bun.sleep(1000);
  }
  throw new Error(`VictoriaLogs project ${projectID} did not reach ${expected} lines`);
}

interface Team { id: number; name: string }
interface Source { id: number; name: string }
interface User { id: number; email: string }
interface Member { user_id: number }

async function ensureTeam(name: string, description: string): Promise<Team> {
  const teams = await api<Team[]>("GET", "/admin/teams");
  return teams.find((t) => t.name === name) ?? (await api<Team>("POST", "/admin/teams", { name, description }));
}

async function ensureMembers(team: Team, emails: string[]) {
  const users = await api<User[]>("GET", "/admin/users");
  const members = await api<Member[]>("GET", `/teams/${team.id}/members`);
  for (const email of emails) {
    const user = users.find((u) => u.email === email);
    if (!user) throw new Error(`user ${email} not found: sign in once through Dex, then rerun`);
    if (!members.some((m) => m.user_id === user.id)) {
      await api("POST", `/teams/${team.id}/members`, { user_id: user.id, role: "admin" });
    }
  }
}

async function ensureSource(team: Team, body: { name: string; source_type: string; meta_ts_field: string; meta_severity_field?: string; description: string; connection: object }): Promise<Source> {
  const sources = await api<Source[]>("GET", "/admin/sources");
  let source = sources.find((s) => s.name === body.name);
  if (!source) {
    source = await api<Source>("POST", "/admin/sources", { meta_is_auto_created: false, meta_severity_field: "", ...body });
  } else if (body.source_type === "victorialogs") {
    await api("PUT", `/admin/sources/${source.id}`, { connection: body.connection });
  }
  const linked = await api<Source[]>("GET", `/teams/${team.id}/sources`);
  if (!linked.some((s) => s.id === source.id)) {
    await api("POST", `/teams/${team.id}/sources`, { source_id: source.id });
  }
  return source;
}

// Panels are written as rows. Each row must fill all 12 columns, so the grid
// has no holes; the layout x and y come from the row order.
type Cell = [panel: DashboardQueryPanel, width: number];
function tab(id: string, title: string, rows: { h: number; cells: Cell[] }[]): { tab: DashboardTab; panels: DashboardQueryPanel[] } {
  const layout = [];
  const panels = [];
  let y = 0;
  for (const row of rows) {
    const width = row.cells.reduce((sum, [, w]) => sum + w, 0);
    if (width !== 12) throw new Error(`tab ${id}: row at y=${y} is ${width} columns wide, expected 12`);
    let x = 0;
    for (const [panel, w] of row.cells) {
      layout.push({ id: panel.id, x, y, w, h: row.h });
      panels.push(panel);
      x += w;
    }
    y += row.h;
  }
  return { tab: { id, title, layout }, panels };
}

function panel(source: Source & { team: Team }, id: string, title: string, query: DashboardPanelQuery, viz: DashboardViz): DashboardQueryPanel {
  return { id, kind: "query", title, team_id: source.team.id, source_id: source.id, query, viz };
}
const sql = (text: string, extra: Partial<DashboardPanelQuery> = {}): DashboardPanelQuery => ({
  mode: "result",
  language: "clickhouse-sql",
  text: text.trim(),
  ...extra,
});
const logsql = (text: string, extra: Partial<DashboardPanelQuery> = {}): DashboardPanelQuery => ({
  mode: "result",
  language: "logsql",
  text: text.trim(),
  ...extra,
});
const count = (text: string, extra: Partial<DashboardPanelQuery> = {}): DashboardPanelQuery => ({
  mode: "count",
  language: "logchefql",
  text,
  ...extra,
});
const crore: NumberFormat = { scale: 1e-7, decimals: 2, prefix: "₹", suffix: " Cr" };
const number = { kind: "number", confidence: "confirmed" } as const;

function apiHealth(http: Source & { team: Team }) {
  const lastHour = (select: string) =>
    sql(`
SELECT toStartOfHour(timestamp, {{__timezone}}) AS hour,
       ${select}
FROM gallery.http_requests
WHERE {{__time_filter}}
  AND timestamp < toStartOfHour(now(), {{__timezone}})
GROUP BY hour
HAVING hour > {{__from}}
ORDER BY hour`);
  const yesterday = (select: string) =>
    sql(
      `
SELECT toStartOfInterval(timestamp, {{__interval}}, {{__timezone}}) AS day,
       ${select}
FROM gallery.http_requests
WHERE {{__time_filter}}
  AND timestamp < toStartOfDay(now(), {{__timezone}})
GROUP BY day
ORDER BY day`,
      { range: "today-6d", interval: "24h" },
    );
  const stat = (value: string, order: string, good: "up" | "down" | "neutral", format: NumberFormat, extra: object = {}): DashboardViz => ({
    type: "stat",
    value,
    order_by: order,
    reduce: "last",
    compare: { mode: "previous_row", good },
    sparkline: true,
    format,
    ...extra,
  });
  const errorThresholds = { thresholds: [{ value: null, color: "green" }, { value: 1, color: "amber" }, { value: 5, color: "red" }], color_mode: "value" } as const;
  const latencyThresholds = { thresholds: [{ value: null, color: "green" }, { value: 600, color: "amber" }, { value: 1000, color: "red" }], color_mode: "value" } as const;

  const day = tab("day", "Last 24 hours", [
    {
      h: 3,
      cells: [
        [panel(http, "requests-hour", "Requests, last hour", lastHour("count() AS requests"), stat("requests", "hour", "up", { notation: "standard", grouping: true })), 3],
        [panel(http, "errors-hour", "5xx rate, last hour", lastHour("round(100 * countIf(status >= 500) / count(), 2) AS error_rate"), stat("error_rate", "hour", "down", { decimals: 2, unit: "percent" }, errorThresholds)), 3],
        [panel(http, "p95-hour", "p95 latency, last hour", lastHour("round(quantile(0.95)(duration_ms)) AS p95_ms"), stat("p95_ms", "hour", "down", { unit: "duration_ms" }, latencyThresholds)), 3],
        [panel(http, "bytes-hour", "Data served, last hour", lastHour("sum(bytes) AS bytes"), stat("bytes", "hour", "neutral", { unit: "bytes", decimals: 1 })), 3],
      ],
    },
    {
      h: 5,
      cells: [
        [
          panel(
            http,
            "requests-by-service",
            "Requests by service",
            sql(
              `
SELECT toStartOfInterval(timestamp, {{__interval}}, {{__timezone}}) AS t,
       service,
       count() AS requests
FROM gallery.http_requests
WHERE {{__time_filter}}
GROUP BY t, service
ORDER BY t, service`,
              { interval: "30m" },
            ),
            { type: "chart", x: "t", y: ["requests"], series: "service", mark: "bar", stack: "stacked", legend: true },
          ),
          8,
        ],
        [
          panel(
            http,
            "slow-routes",
            "Slowest routes, p95",
            sql(`
SELECT route, round(quantile(0.95)(duration_ms)) AS p95_ms
FROM gallery.http_requests
WHERE {{__time_filter}}
GROUP BY route
ORDER BY p95_ms DESC
LIMIT 8`),
            { type: "toplist", label: "route", value: "p95_ms", format: { unit: "duration_ms" } },
          ),
          4,
        ],
      ],
    },
    {
      h: 5,
      cells: [
        [
          panel(
            http,
            "latency",
            "Latency",
            sql(
              `
SELECT toStartOfInterval(timestamp, {{__interval}}, {{__timezone}}) AS t,
       round(quantile(0.5)(duration_ms)) AS p50,
       round(quantile(0.95)(duration_ms)) AS p95
FROM gallery.http_requests
WHERE {{__time_filter}}
GROUP BY t
ORDER BY t`,
              { interval: "15m" },
            ),
            {
              type: "chart",
              x: "t",
              y: ["p50", "p95"],
              mark: "line",
              legend: true,
              format: { unit: "duration_ms" },
              thresholds: [{ value: null, color: "green" }, { value: 800, color: "red" }],
            },
          ),
          7,
        ],
        [
          panel(
            http,
            "errors-by-service",
            "5xx by service",
            sql(
              `
SELECT toStartOfInterval(timestamp, {{__interval}}, {{__timezone}}) AS t,
       service,
       count() AS errors
FROM gallery.http_requests
WHERE {{__time_filter}} AND status >= 500
GROUP BY t, service
ORDER BY t, service`,
              { interval: "30m" },
            ),
            { type: "chart", x: "t", y: ["errors"], series: "service", mark: "bar", stack: "stacked", legend: true },
          ),
          5,
        ],
      ],
    },
    {
      h: 6,
      cells: [
        [
          panel(
            http,
            "recent-5xx",
            "Recent 5xx responses",
            sql(
              `
SELECT timestamp, service, method, route, status, duration_ms, region, msg, trace_id
FROM gallery.http_requests
WHERE {{__time_filter}} AND status >= 500
ORDER BY timestamp DESC
LIMIT 50`,
              { limit: 50 },
            ),
            {
              type: "table",
              visible_columns: ["timestamp", "service", "route", "status", "duration_ms", "region", "msg"],
              column_formats: {
                timestamp: { kind: "time", date: "medium", time: "seconds" },
                duration_ms: { unit: "duration_ms" },
              },
              rules: [
                { columns: ["duration_ms"], op: ">=", value: 1000, color: "red", scope: "cell" },
                { columns: ["status"], op: "=", value: 504, color: "amber", scope: "cell" },
                { columns: ["msg"], op: "contains", value: "timed out", color: "purple", scope: "row" },
              ],
            },
          ),
          12,
        ],
      ],
    },
  ]);

  const week = tab("week", "Last 7 days", [
    {
      h: 3,
      cells: [
        [panel(http, "requests-day", "Requests, yesterday", yesterday("count() AS requests"), stat("requests", "day", "up", { notation: "compact", decimals: 1 })), 4],
        [panel(http, "errors-day", "5xx rate, yesterday", yesterday("round(100 * countIf(status >= 500) / count(), 2) AS error_rate"), stat("error_rate", "day", "down", { decimals: 2, unit: "percent" }, errorThresholds)), 4],
        [panel(http, "p95-day", "p95 latency, yesterday", yesterday("round(quantile(0.95)(duration_ms)) AS p95_ms"), stat("p95_ms", "day", "down", { unit: "duration_ms" }, latencyThresholds)), 4],
      ],
    },
    {
      h: 5,
      cells: [
        [
          panel(
            http,
            "daily-requests",
            "Requests per day",
            sql(
              `
SELECT toStartOfInterval(timestamp, {{__interval}}, {{__timezone}}) AS day,
       count() AS requests
FROM gallery.http_requests
WHERE {{__time_filter}}
GROUP BY day
ORDER BY day`,
              { range: "today-6d", interval: "24h" },
            ),
            { type: "chart", x: "day", y: ["requests"], mark: "bar", show_values: true, format: { notation: "compact", decimals: 0 } },
          ),
          8,
        ],
        [
          panel(
            http,
            "regions",
            "Requests by region",
            sql(
              `
SELECT region, count() AS requests
FROM gallery.http_requests
WHERE {{__time_filter}}
GROUP BY region
ORDER BY requests DESC`,
              { range: "today-6d" },
            ),
            { type: "pie", label: "region", value: "requests", donut: true, format: { notation: "compact", decimals: 1 } },
          ),
          4,
        ],
      ],
    },
    {
      h: 5,
      cells: [
        [
          panel(
            http,
            "daily-p95",
            "Daily p95 latency by service",
            sql(
              `
SELECT toStartOfInterval(timestamp, {{__interval}}, {{__timezone}}) AS day,
       service,
       round(quantile(0.95)(duration_ms)) AS p95_ms
FROM gallery.http_requests
WHERE {{__time_filter}} AND service IN ('checkout', 'payments', 'search', 'recs')
GROUP BY day, service
ORDER BY day, service`,
              { range: "today-6d", interval: "24h" },
            ),
            { type: "chart", x: "day", y: ["p95_ms"], series: "service", mark: "line", legend: true, format: { unit: "duration_ms" } },
          ),
          7,
        ],
        [
          panel(
            http,
            "error-routes",
            "Routes with the most 5xx",
            sql(
              `
SELECT route, countIf(status >= 500) AS errors
FROM gallery.http_requests
WHERE {{__time_filter}}
GROUP BY route
ORDER BY errors DESC
LIMIT 8`,
              { range: "today-6d" },
            ),
            { type: "toplist", label: "route", value: "errors", format: { notation: "standard", grouping: true } },
          ),
          5,
        ],
      ],
    },
  ]);

  return blob([day, week], { default_time_range: "24h" });
}

function business(orders: Source & { team: Team }) {
  // Each day up to the current time of day, so today compares with yesterday at this hour.
  const sameTime = (select: string) =>
    sql(`
SELECT toStartOfDay(created_at, {{__timezone}}) AS day,
       ${select}
FROM gallery.orders
WHERE {{__time_filter}}
  AND dateDiff('second', toStartOfDay(created_at, {{__timezone}}), created_at)
      <= dateDiff('second', toStartOfDay(now(), {{__timezone}}), now())
GROUP BY day
ORDER BY day`);
  const stat = (value: string, good: "up" | "down", format: NumberFormat, extra: object = {}): DashboardViz => ({
    type: "stat",
    value,
    order_by: "day",
    reduce: "last",
    compare: { mode: "previous_row", good },
    sparkline: true,
    format,
    ...extra,
  });

  const main = tab("main", "Main", [
    {
      h: 3,
      cells: [
        [panel(orders, "revenue-today", "Revenue today", sameTime("sumIf(amount, status = 'paid') AS revenue"), stat("revenue", "up", crore)), 3],
        [panel(orders, "orders-today", "Orders today", sameTime("countIf(status = 'paid') AS orders"), stat("orders", "up", { notation: "standard", grouping: true })), 3],
        [panel(orders, "aov-today", "Average order value", sameTime("avgIf(amount, status = 'paid') AS aov"), stat("aov", "up", { decimals: 0, grouping: true, prefix: "₹" })), 3],
        [
          panel(
            orders,
            "refunds-today",
            "Refund rate",
            sameTime("round(100 * countIf(status = 'refunded') / count(), 2) AS refund_rate"),
            stat("refund_rate", "down", { decimals: 2, unit: "percent" }, {
              thresholds: [{ value: null, color: "green" }, { value: 4, color: "amber" }, { value: 6, color: "red" }],
              color_mode: "background",
            }),
          ),
          3,
        ],
      ],
    },
    {
      h: 5,
      cells: [
        [
          panel(
            orders,
            "daily-revenue",
            "Daily revenue",
            sql(
              `
SELECT toStartOfInterval(created_at, {{__interval}}, {{__timezone}}) AS day,
       sumIf(amount, status = 'paid') AS revenue
FROM gallery.orders
WHERE {{__time_filter}}
GROUP BY day
ORDER BY day`,
              { interval: "24h" },
            ),
            { type: "chart", x: "day", y: ["revenue"], mark: "bar", stack: "none", show_values: true, format: { ...crore, decimals: 1 } },
          ),
          8,
        ],
        [
          panel(
            orders,
            "payment-methods",
            "Orders by payment method",
            sql(`
SELECT payment_method, count() AS orders
FROM gallery.orders
WHERE {{__time_filter}}
GROUP BY payment_method
ORDER BY orders DESC`),
            { type: "pie", label: "payment_method", value: "orders", donut: true, format: { notation: "compact", decimals: 1 } },
          ),
          4,
        ],
      ],
    },
    {
      h: 5,
      cells: [
        [
          panel(
            orders,
            "hourly-orders",
            "Orders by hour, today and yesterday",
            sql(`
SELECT toHour(created_at, {{__timezone}}) AS hour,
       if(toDate(created_at, {{__timezone}}) = toDate(now(), {{__timezone}}), 'Today', 'Yesterday') AS day,
       count() AS orders
FROM gallery.orders
WHERE created_at >= toStartOfDay(now(), {{__timezone}}) - INTERVAL 1 DAY
  AND created_at < toStartOfHour(now(), {{__timezone}})
GROUP BY hour, day
ORDER BY hour, day`),
            { type: "chart", x: "hour", y: ["orders"], series: "day", mark: "line", x_scale: "category", legend: true },
          ),
          8,
        ],
        [
          panel(
            orders,
            "cities",
            "Revenue by city",
            sql(`
SELECT city, sumIf(amount, status = 'paid') AS revenue
FROM gallery.orders
WHERE {{__time_filter}}
GROUP BY city
ORDER BY revenue DESC
LIMIT 8`),
            { type: "toplist", label: "city", value: "revenue", format: crore },
          ),
          4,
        ],
      ],
    },
    {
      h: 4,
      cells: [
        [
          panel(
            orders,
            "categories",
            "Categories",
            sql(`
SELECT category,
       countIf(status = 'paid') AS orders,
       sumIf(amount, status = 'paid') AS revenue,
       avgIf(amount, status = 'paid') AS avg_order,
       round(100 * countIf(status = 'refunded') / count(), 2) AS refund_pct
FROM gallery.orders
WHERE {{__time_filter}}
GROUP BY category
ORDER BY revenue DESC`),
            {
              type: "table",
              column_formats: {
                orders: { grouping: true },
                revenue: crore,
                avg_order: { decimals: 0, grouping: true, prefix: "₹" },
                refund_pct: { decimals: 2, unit: "percent" },
              },
              rules: [
                { columns: ["refund_pct"], op: ">=", value: 3.6, color: "red", scope: "cell" },
                { columns: ["revenue"], op: ">=", value: 400000000, color: "green", scope: "cell" },
              ],
            },
          ),
          12,
        ],
      ],
    },
  ]);
  return blob([main], { default_time_range: "today-6d" });
}

function workerLogs(logs: Source & { team: Team }) {
  const main = tab("main", "Main", [
    {
      h: 3,
      cells: [
        [panel(logs, "lines", "Log lines", count(`env="prod"`), { type: "stat", format: { notation: "compact", decimals: 1 } }), 3],
        [
          panel(logs, "errors", "Errors", count(`level="error"`), {
            type: "stat",
            format: { notation: "standard", grouping: true },
            thresholds: [{ value: null, color: "green" }, { value: 500, color: "amber" }, { value: 2000, color: "red" }],
            color_mode: "background",
          }),
          3,
        ],
        [panel(logs, "warnings", "Warnings", count(`level="warn"`), { type: "stat", format: { notation: "standard", grouping: true }, thresholds: [{ value: null, color: "amber" }], color_mode: "value" }), 3],
        [
          panel(logs, "p95-duration", "p95 job duration", logsql(`* | stats quantile(0.95, duration_ms) as p95_ms`), {
            type: "stat",
            value: "p95_ms",
            columns: { p95_ms: number },
            format: { unit: "duration_ms" },
          }),
          3,
        ],
      ],
    },
    {
      h: 5,
      cells: [
        [panel(logs, "volume", "Log volume by service", count(`env="prod"`, { group_by: "service", interval: "15m" }), { type: "chart", mark: "bar", stack: "stacked" }), 8],
        [panel(logs, "levels", "Levels", count(`env="prod"`, { group_by: "level" }), { type: "pie", donut: true }), 4],
      ],
    },
    {
      h: 5,
      cells: [
        [
          panel(
            logs,
            "error-services",
            "Errors by service",
            logsql(`level:error | stats by (service) count() as errors | sort by (errors desc)`),
            { type: "toplist", label: "service", value: "errors", columns: { errors: number }, format: { notation: "standard", grouping: true } },
          ),
          4,
        ],
        [
          panel(
            logs,
            "recent-errors",
            "Recent errors",
            logsql(`level:error | sort by (_time desc) | limit 30 | fields _time, service, pod, duration_ms, _msg`, { limit: 30 }),
            {
              type: "table",
              columns: { _time: { kind: "time", confidence: "confirmed" }, duration_ms: number },
              visible_columns: ["_time", "service", "_msg", "duration_ms"],
              column_formats: {
                _time: { kind: "time", date: "none", time: "seconds" },
                duration_ms: { unit: "duration_ms" },
              },
              rules: [{ columns: ["service"], op: "=", value: "payment-webhooks", color: "amber", scope: "row" }],
            },
          ),
          8,
        ],
      ],
    },
  ]);
  return blob([main], { default_time_range: "24h" });
}

function blob(tabs: { tab: DashboardTab; panels: DashboardQueryPanel[] }[], extra: Partial<DashboardPanels>): DashboardPanels {
  // A short TTL keeps reruns fresh. TTL 0 is avoided on purpose: uncached
  // panels count against the per-user preview limit of 3 and one of four
  // concurrent panels fails with 429.
  return { version: 2, features: [], cache_ttl_seconds: 0, ...extra, tabs: tabs.map((t) => t.tab), panels: tabs.flatMap((t) => t.panels) };
}

interface Summary { id: number; name: string }

async function upsertDashboard(name: string, description: string, panels: DashboardPanels): Promise<number> {
  const all = await api<Summary[]>("GET", "/dashboards", undefined, DASHBOARD_HEADERS);
  const [existing, ...duplicates] = all.filter((d) => d.name === name);
  for (const d of duplicates) await api("DELETE", `/dashboards/${d.id}`, undefined, DASHBOARD_HEADERS);
  if (!existing) {
    const created = await api<Dashboard>("POST", "/dashboards", { name, description, panels }, DASHBOARD_HEADERS);
    return created.id;
  }
  const current = await api<Dashboard>("GET", `/dashboards/${existing.id}`, undefined, DASHBOARD_HEADERS);
  await api("PUT", `/dashboards/${existing.id}`, { name, description, panels, revision: current.revision }, DASHBOARD_HEADERS);
  return existing.id;
}

const projectID = String(Math.floor(Date.now() / 1000));
await loadClickHouse();
await loadVictoriaLogs(projectID);

const team = await ensureTeam("Storefront", "Dashboard gallery: synthetic storefront traffic, orders and worker logs");
const me = await api<{ user: User }>("GET", "/me");
await ensureMembers(team, [...new Set([me.user.email, VIEWER])]);
const withTeam = (s: Source) => ({ ...s, team });
const http = withTeam(
  await ensureSource(team, {
    name: "Storefront API",
    source_type: "clickhouse",
    meta_ts_field: "timestamp",
    meta_severity_field: "level",
    description: "Gallery: synthetic API access logs (dev/gallery)",
    connection: { host: SOURCE_CLICKHOUSE_HOST, username: "default", password: "", database: "gallery", table_name: "http_requests" },
  }),
);
const orders = withTeam(
  await ensureSource(team, {
    name: "Orders",
    source_type: "clickhouse",
    meta_ts_field: "created_at",
    description: "Gallery: synthetic orders in INR (dev/gallery)",
    connection: { host: SOURCE_CLICKHOUSE_HOST, username: "default", password: "", database: "gallery", table_name: "orders" },
  }),
);
const logs = withTeam(
  await ensureSource(team, {
    name: "Worker logs",
    source_type: "victorialogs",
    meta_ts_field: "_time",
    meta_severity_field: "level",
    description: "Gallery: synthetic background worker logs (dev/gallery)",
    connection: { base_url: SOURCE_VICTORIALOGS_URL, tenant: { account_id: "0", project_id: projectID } },
  }),
);

const dashboards = {
  "api-health": await upsertDashboard("API health", "Storefront API traffic, errors and latency", apiHealth(http)),
  business: await upsertDashboard("Business KPIs", "Orders and revenue in INR", business(orders)),
  "worker-logs": await upsertDashboard("Worker logs", "Background workers on VictoriaLogs", workerLogs(logs)),
};
console.log(JSON.stringify(dashboards));
