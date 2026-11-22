import { afterEach, describe, expect, it } from "vitest";
import { createApp, nextTick, type App } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import { i18n } from "@/i18n";
import { controlledApi } from "@/test/controlledApi";
import type { QueryStatsResponse, QueryTotals, RunHealth, SourceActivity, UserQueryStat } from "@/api/admin";
import QueryActivity from "../QueryActivity.vue";

const totals = (query_count: number, total_duration_ms: number): QueryTotals => ({ query_count, total_duration_ms });
// Before latency, scan and failure collection began.
const uncollected: RunHealth = { cost: null, failure_count: 0, failure_rate: null };
const noP95 = Array.from({ length: 30 }, () => null);
const user = (id: number, fields: Partial<UserQueryStat>): UserQueryStat => ({
  user_id: id, email: `user${id}@example.test`, full_name: "", account_type: "human", active_days: 3,
  current: totals(10, 1000), prior: totals(10, 1000), ...fields,
});
const stats: QueryStatsResponse = {
  window: { since: "2026-10-22", until: "2026-11-20", days: 30 },
  prior_window: { since: "2026-09-22", until: "2026-10-21", days: 30 },
  filters: { account: "human", source_id: null, source_name: "", user_id: null, user_name: "" },
  account_totals: { human: 14820, service: 253700, all: 268520 },
  total_users: 68,
  summary: {
    current: { query_count: 14820, total_duration_ms: 14820 * 1480, active_users: 42, active_sources: 10, avg_daily_active_users: 14.6, ...uncollected },
    prior: { query_count: 11950, total_duration_ms: 11950 * 1000, active_users: 36, active_sources: 10, avg_daily_active_users: 12, ...uncollected },
  },
  volume_by_day: Array.from({ length: 30 }, (_, i) => ({ date: `2026-${i < 10 ? "10" : "11"}-${String(i < 10 ? 22 + i : i - 9).padStart(2, "0")}`, query_count: i * 10, active_users: i % 5 })),
  top_users: [
    user(5, { full_name: "Alice Example", email: "alice@example.test", active_days: 24, current: totals(2140, 2140 * 2400), prior: totals(1633, 1633 * 2400) }),
    user(6, { current: totals(900, 900 * 500), prior: totals(0, 0) }),
  ],
  service_summary: {
    account_count: 3, query_count: 253700,
    top: [user(90, { account_type: "service", full_name: "Error Digest Worker", email: "svc-1@service.logchef.internal", current: totals(241000, 0) })],
  },
  sources: [
    { source_id: 1, source_name: "HTTP Access", source_type: "clickhouse", team_id: 4, service_query_count: 178600, current: totals(4920, 4920 * 1820), prior: totals(3814, 3814 * 1200), advisor: { analyzed: true, high: 1, medium: 0, low: 2 }, p95_by_day: noP95, ...uncollected },
    { source_id: 2, source_name: "Order Engine", source_type: "clickhouse", team_id: 4, service_query_count: 0, current: totals(3410, 3410 * 340), prior: totals(3045, 3045 * 374), advisor: { analyzed: false, high: 0, medium: 0, low: 0 }, p95_by_day: noP95, ...uncollected },
    { source_id: 3, source_name: "Edge Proxy", source_type: "loki", team_id: null, service_query_count: 0, current: totals(620, 620 * 410), prior: totals(660, 660 * 406), advisor: null, p95_by_day: noP95, ...uncollected },
  ],
  shapes: {
    since: "2026-10-22",
    unanalyzed_query_count: 148,
    rows: [
      { source_id: 1, source_name: "HTTP Access", source_type: "clickhouse", query_language: "clickhouse-sql", shape_hash: "a", shape: [{ column: "http_referer", kind: "like" }, { column: "attrs", path: ["merchant_id"], access: "map", kind: "eq" }], max_lookback_s: 30 * 86400, current: totals(842, 842 * 4380), prior: totals(600, 600 * 4000), finding: { id: "f1", title: "Add a bloom filter index on http_referer", impact: "high" }, avg_bytes_read: null },
      { source_id: 2, source_name: "Order Engine", source_type: "clickhouse", query_language: "logchefql", shape_hash: "b", shape: [{ column: "status", kind: "in" }], max_lookback_s: null, current: totals(100, 10000), prior: null, finding: null, avg_bytes_read: null },
    ],
  },
  language_mix: [{ language: "logchefql", query_count: 640 }, { language: "clickhouse-sql", query_count: 310 }, { language: "logql", query_count: 50 }],
  collected_since: null,
  failures: { by_reason: [], top: [] },
};

const GiB = 1024 ** 3;
const pct = (ms: number) => ({ ms, open: false });
// The same window once latency, scan and failures are recorded.
const sourceHealth: Record<number, Partial<SourceActivity>> = {
  1: {
    cost: { runs: 4900, p50: pct(1820), p95: { ms: 60_000, open: true }, avg_rows_read: 24_800_000, avg_bytes_read: 2.14 * GiB },
    failure_count: 232, failure_rate: 0.0451, p95_by_day: [...noP95.slice(0, 27), 9000, null, 11400],
  },
  // 26 times the prior window's queries.
  2: { current: totals(3410, 3410 * 340), prior: totals(131, 131 * 374), cost: { runs: 3400, p50: pct(340), p95: pct(1400), avg_rows_read: 1_900_000, avg_bytes_read: 184 * 1024 ** 2 }, failure_count: 0, failure_rate: 0 },
  3: { cost: { runs: 620, p50: pct(410), p95: pct(1900), avg_rows_read: 0, avg_bytes_read: 0 }, failure_count: 11, failure_rate: 0.0174 },
};
const healthy: QueryStatsResponse = {
  ...stats,
  collected_since: "2026-11-10",
  summary: {
    current: { ...stats.summary.current, cost: { runs: 14400, p50: pct(640), p95: pct(6800), avg_rows_read: 0, avg_bytes_read: 0 }, failure_count: 415, failure_rate: 0.028 },
    prior: { ...stats.summary.prior, cost: { runs: 11700, p50: pct(600), p95: pct(5600), avg_rows_read: 0, avg_bytes_read: 0 }, failure_count: 228, failure_rate: 0.019 },
  },
  sources: stats.sources.map(source => ({ ...source, ...sourceHealth[source.source_id] })),
  shapes: { ...stats.shapes, rows: [{ ...stats.shapes.rows[0], avg_bytes_read: 3.8 * GiB }, stats.shapes.rows[1]] },
  failures: {
    by_reason: [
      { reason: "timeout", count: 128, prior: 78 },
      { reason: "invalid_query", count: 94, prior: 90 },
      { reason: "gremlins", count: 3, prior: null },
    ],
    top: [
      { source_id: 1, source_name: "HTTP Access", source_type: "clickhouse", user_id: 5, email: "alice@example.test", full_name: "Alice Example", account_type: "human", count: 89 },
      { source_id: 2, source_name: "", source_type: "", user_id: 90, email: "svc-1@service.logchef.internal", full_name: "Error Digest Worker", account_type: "service", count: 12 },
    ],
  },
};

// The fixture window's whole UTC days: 2026-10-22T00:00Z up to 2026-11-21T00:00Z.
const windowRange = "start=1792627200000&end=1795219200000";

let app: App | undefined;
let root: HTMLDivElement;
let router: Router;
let network: ReturnType<typeof controlledApi>;
async function flush() {
  for (let i = 0; i < 5; i++) {
    await new Promise(resolve => setTimeout(resolve, 0));
    await nextTick();
  }
}
const blank = { template: "<div />" };
async function mount(path = "/") {
  network = controlledApi();
  router = createRouter({ history: createMemoryHistory(), routes: [
    { path: "/", component: QueryActivity },
    { path: "/admin/audit-log", name: "AdminAuditLog", component: blank },
    { path: "/admin/sources/stats", name: "SourceInspection", component: blank },
    { path: "/admin/sources/:sourceId/edit", name: "EditSource", component: blank },
    { path: "/logs/explore", name: "LogExplorer", component: blank },
  ] });
  await router.push(path);
  root = document.createElement("div"); document.body.appendChild(root);
  app = createApp({ template: "<router-view />" });
  app.use(createPinia()).use(i18n).use(router).mount(root);
  await flush();
}
function last() {
  const found = network.requests.at(-1);
  if (!found?.config.url?.startsWith("/admin/query-stats")) throw new Error("Missing query-stats request");
  return found;
}
function params() {
  return Object.fromEntries(new URL(last().config.url!, "http://fixture.test").searchParams);
}
async function respond(data: QueryStatsResponse = stats) {
  last().succeed(data); await flush();
}
function button(text: string) {
  const found = [...root.querySelectorAll("button")].find(item => item.textContent?.trim().startsWith(text));
  if (!found) throw new Error(`Missing button ${text}`);
  return found;
}
function section(name: string) {
  const found = root.querySelector<HTMLElement>(`[data-section="${name}"]`);
  if (!found) throw new Error(`Missing section ${name}`);
  return found;
}
function hrefs(scope: ParentNode, label: string) {
  return [...scope.querySelectorAll<HTMLAnchorElement>(`a[aria-label="${label}"]`)].map(a => decodeURIComponent(a.getAttribute("href") ?? ""));
}
afterEach(() => {
  app?.unmount(); app = undefined; root?.remove(); network?.restore();
});

describe("Query Activity", () => {
  it("asks for humans over 30 days and compares every headline number with the prior window", async () => {
    await mount();
    expect(params()).toEqual({ days: "30", account: "human" });
    await respond();
    const metric = (key: string) => section("headline").querySelector(`[data-metric="${key}"]`)?.textContent?.replace(/\s+/g, " ") ?? "";
    expect(metric("users")).toContain("42of 68 users");
    expect(metric("users")).toContain("↑ +17%");
    expect(metric("users")).toContain("prior: 36");
    expect(metric("queries")).toContain("14,820");
    expect(metric("queries")).toContain("↑ +24%");
    expect(metric("queries")).toContain("253.7K service account queries hidden");
    expect(metric("queries")).toContain("10 sources");
    expect(metric("latency")).toContain("avg 1.48 s");
    expect(metric("latency")).toContain("↑ +48% slower");
    expect(metric("latency")).toContain("prior: 1.00 s");
  });

  it("shows a dash for latency percentiles and failure rate before they were collected", async () => {
    await mount();
    await respond();
    const tile = (key: string) => section("headline").querySelector<HTMLElement>(`[data-metric="${key}"]`)!;
    const value = (key: string) => tile(key).querySelector("dd span")!;
    expect(value("latency").textContent).toBe("—");
    expect(value("latency").getAttribute("title")).toBe("Not collected yet");
    expect(value("failures").textContent).toBe("—");
    button("Refresh").click(); await flush();
    await respond({ ...stats, collected_since: "2026-11-10" });
    expect(value("latency").getAttribute("title")).toBe("Collected from Nov 10");
    expect(section("failures").textContent).toContain("No failed queries in this window.");
  });

  it("shows p50 and p95 latency and the failure rate beside the prior window", async () => {
    await mount();
    await respond(healthy);
    const metric = (key: string) => section("headline").querySelector(`[data-metric="${key}"]`)?.textContent?.replace(/\s+/g, " ") ?? "";
    expect(metric("latency")).toContain("640 ms");
    expect(metric("latency")).toContain("p95 6.80 s");
    expect(metric("latency")).toContain("avg 1.48 s");
    expect(metric("latency")).toContain("↑ +21% slower");
    expect(metric("latency")).toContain("prior: p95 5.60 s");
    expect(metric("failures")).toContain("2.8%");
    expect(metric("failures")).toContain("415 failed");
    expect(metric("failures")).toContain("128 timed out · 94 invalid query");
    expect(metric("failures")).toContain("↑ +0.9 pp");
    expect(metric("failures")).toContain("prior: 1.9%");
    // The window starts before collection, so both values cover only part of it.
    const title = (key: string) => section("headline").querySelector(`[data-metric="${key}"] dd span`)!.getAttribute("title");
    expect(title("latency")).toBe("Latency, scan and failure data start on Nov 10. Earlier days count queries only.");
    expect(title("failures")).toBe(title("latency"));
  });

  it("compares latency by average when a p95 is past the last bucket", async () => {
    await mount();
    const current = { ...healthy.summary.current, cost: { ...healthy.summary.current.cost!, p95: { ms: 60_000, open: true } } };
    await respond({ ...healthy, summary: { ...healthy.summary, current } });
    const metric = section("headline").querySelector('[data-metric="latency"]')?.textContent?.replace(/\s+/g, " ") ?? "";
    expect(metric).toContain("p95 >60.0 s");
    expect(metric).toContain("↑ +48% slower");
  });

  it("switches accounts and windows through the URL and ignores superseded responses", async () => {
    await mount();
    await respond();
    expect(button("Service accounts").textContent).toContain("253.7K");
    button("Service accounts").click(); await flush();
    const superseded = last();
    expect(params()).toEqual({ days: "30", account: "service" });
    button("7 days").click(); await flush();
    expect(params()).toEqual({ days: "7", account: "service" });
    expect(router.currentRoute.value.query).toEqual({ account: "service", days: "7" });
    await respond({ ...stats, window: { ...stats.window, days: 7 }, filters: { ...stats.filters, account: "service" }, service_summary: null });
    superseded.succeed({ ...stats, summary: { ...stats.summary, current: { ...stats.summary.current, query_count: 999 } } }); await flush();
    expect(section("headline").textContent).not.toContain("999");
    expect(root.querySelector('[data-section="service-summary"]')).toBeNull();
  });

  it("scopes the page to a clicked source or user and removes each filter chip", async () => {
    await mount();
    await respond();
    button("HTTP Access").click(); await flush();
    expect(params()).toEqual({ days: "30", account: "human", source_id: "1" });
    await respond({ ...stats, filters: { ...stats.filters, source_id: 1, source_name: "HTTP Access" } });
    button("Alice Example").click(); await flush();
    expect(params()).toEqual({ days: "30", account: "human", source_id: "1", user_id: "5" });
    await respond({ ...stats, filters: { ...stats.filters, source_id: 1, source_name: "HTTP Access", user_id: 5, user_name: "Alice Example" } });
    expect(button("Source: HTTP Access").getAttribute("aria-label")).toBe("Remove filter Source: HTTP Access");
    button("Source: HTTP Access").click(); await flush();
    expect(params()).toEqual({ days: "30", account: "human", user_id: "5" });
    await respond({ ...stats, filters: { ...stats.filters, user_id: 5, user_name: "Alice Example" } });
    button("User: Alice Example").click(); await flush();
    expect(params()).toEqual({ days: "30", account: "human" });
  });

  it("opens a linked scope from the URL and labels a chip before the name loads", async () => {
    await mount("/?account=all&source=3&days=90");
    expect(params()).toEqual({ days: "90", account: "all", source_id: "3" });
    expect(button("Source: Source #3")).toBeTruthy();
  });

  it("lists users with their deltas and audit links, and names service accounts by their full name", async () => {
    await mount();
    await respond();
    const adoption = section("adoption");
    const rows = [...adoption.querySelectorAll('[data-table="users"] tbody tr')].map(row => row.textContent?.replace(/\s+/g, " ").trim());
    expect(rows[0]).toContain("Alice Example");
    expect(rows[0]).toContain("alice@example.test");
    expect(rows[0]).toContain("24 / 30");
    expect(rows[0]).toContain("↑ +31%");
    expect(rows[1]).toContain("user6@example.test");
    expect(rows[1]).toContain("new");
    expect(hrefs(adoption, "Query runs in the Audit Log")[0]).toBe(`/admin/audit-log?action=query.run&${windowRange}&actor_id=5`);
    const service = section("service-summary").textContent ?? "";
    expect(service).toContain("3 service accounts");
    expect(service).toContain("Error Digest Worker (241K)");
    expect(service).not.toContain("service.logchef.internal");
    button("Show service accounts").click(); await flush();
    expect(params().account).toBe("service");
  });

  it("shows the language mix, the daily charts and their values", async () => {
    await mount();
    await respond();
    const languages = [...section("languages").querySelectorAll("li")].map(li => li.textContent?.replace(/\s+/g, " ").trim());
    expect(languages).toEqual(["64% LogchefQL", "31% ClickHouse SQL", "5% LogQL"]);
    expect(root.querySelectorAll("[data-chart]")).toHaveLength(2);
    expect(root.textContent).toContain("Prior average 398.3");
    expect(root.querySelectorAll("details tbody tr")).toHaveLength(30);
  });

  it("shows the hovered day's numbers in both charts' readout", async () => {
    await mount();
    await respond();
    const readout = () => root.querySelector("[data-chart-readout]")!.textContent!.trim();
    expect(readout()).toBe("Hover a day to see its numbers");
    const days = root.querySelectorAll('[data-chart="users"] [data-day]');
    days[0].dispatchEvent(new Event("mouseenter"));
    await flush();
    expect(readout()).toMatch(/: [\d,]+ queries, [\d,.]+ active users$/);
    root.querySelector('[data-chart="queries"] [data-day]')!.closest(".space-y-4")!.dispatchEvent(new Event("mouseleave"));
    await flush();
    expect(readout()).toBe("Hover a day to see its numbers");
  });

  it("links each source to Explorer, its settings, its query runs and its advisor findings", async () => {
    await mount();
    await respond();
    const sources = section("sources");
    const row = (id: number) => sources.querySelector(`[data-source="${id}"]`)!;
    expect(row(1).textContent).toContain("+178.6K service account queries");
    expect(row(1).textContent).toContain("1.82 s");
    expect(row(1).querySelector('[data-link="advisor"]')?.textContent?.trim()).toBe("1 high · 2 low");
    expect(row(1).querySelector('[data-link="advisor"]')?.getAttribute("href")).toBe("/admin/sources/stats?sourceId=1");
    expect(row(2).querySelector('[data-link="advisor"]')?.textContent?.trim()).toBe("Not analyzed");
    expect(row(3).querySelector('[data-link="advisor"]')).toBeNull();
    expect(hrefs(row(1), "Open in Explorer")).toEqual(["/logs/explore?team=4&source=1"]);
    expect(hrefs(row(1), "Source settings")).toEqual(["/admin/sources/1/edit"]);
    expect(hrefs(row(1), "Query runs in the Audit Log")).toEqual([`/admin/audit-log?action=query.run&${windowRange}&target_type=source&target_id=1`]);
    const header = (text: string) => decodeURIComponent([...root.querySelectorAll("a")].find(a => a.textContent?.trim() === text)?.getAttribute("href") ?? "");
    expect(header("Failed queries")).toBe(`/admin/audit-log?action=query.run&${windowRange}&outcome=failure`);
    expect(header("Audit Log")).toBe(`/admin/audit-log?action=query.run&${windowRange}`);
  });

  it("disables the Explorer link of a source no team links", async () => {
    await mount();
    await respond();
    const row = section("sources").querySelector('[data-source="3"]')!;
    expect(hrefs(row, "Open in Explorer")).toEqual([]);
    const disabled = row.querySelector('[aria-label="Open in Explorer"]');
    expect(disabled?.getAttribute("aria-disabled")).toBe("true");
    expect(disabled?.getAttribute("title")).toBe("No team links this source");
  });

  it("ranks filter shapes with their advisor finding and counts the unanalyzed queries apart", async () => {
    await mount();
    await respond();
    const patterns = section("patterns");
    const [first, second] = [...patterns.querySelectorAll("tbody tr")];
    expect(first.textContent).toContain("http_referer like");
    expect(first.textContent).toContain("attrs['merchant_id'] eq");
    expect(first.textContent).toContain("max 30d lookback");
    expect(first.textContent).toContain("1 h");
    expect(second.textContent).toContain("10.0 s");
    expect(first.textContent).toContain("↑ +40%");
    expect(first.querySelector('[data-link="advisor"]')?.textContent?.trim()).toBe("Add a bloom filter index on http_referer");
    expect(second.querySelector('[data-link="advisor"]')?.textContent?.trim()).toBe("Open Schema Advisor");
    expect(second.textContent).not.toContain("new");
    expect(second.textContent).not.toContain("%");
    expect(patterns.textContent).toContain("148 queries have no single filter shape");
    expect(patterns.textContent).not.toContain("kept for 90 days");
  });

  it("keeps the top users table inside its column: fixed widths, truncated names, wrapping headers", async () => {
    await mount();
    await respond();
    const table = section("adoption").querySelector<HTMLTableElement>('[data-table="users"] table')!;
    expect(table.classList).toContain("table-fixed");
    expect([...table.classList].some(c => c.startsWith("min-w-"))).toBe(false);
    const widths = [...table.querySelectorAll("colgroup col")].map(col => col.className);
    expect(widths).toHaveLength(5);
    expect(widths[0]).toBe("");
    expect(widths.slice(1).every(c => /^w-/.test(c))).toBe(true);
    expect(table.querySelector('[data-cell="user"]')!.classList).toContain("truncate");
    for (const head of [...table.querySelectorAll("thead th")].slice(1, 4)) expect(head.classList).toContain("whitespace-normal");
    // The latency delta drops its word to fit.
    expect(table.textContent).toContain("→ 0%");
    expect(table.textContent).not.toContain("slower");
  });

  it("shows each source's percentiles, daily p95, scan cost and failure rate", async () => {
    await mount();
    await respond(healthy);
    const row = (id: number) => section("sources").querySelector(`[data-source="${id}"]`)!;
    const cell = (id: number, name: string) => row(id).querySelector(`[data-cell="${name}"]`)!.textContent!.replace(/\s+/g, " ").trim();
    expect(cell(1, "latency")).toContain("1.82 s / >60.0 s");
    expect(cell(1, "latency")).toContain("avg 1.82 s");
    expect(cell(1, "scanned")).toContain("2.14 GiB");
    expect(cell(1, "scanned")).toContain("24.8M rows");
    expect(cell(1, "failures")).toBe("4.5%232 failed");
    // Days 28 and 30 are runs of one day each: two dots, no line.
    const spark = row(1).querySelector("[data-sparkline]")!;
    expect(spark.getAttribute("aria-label")).toBe("p95 latency by day");
    expect(spark.querySelectorAll("circle")).toHaveLength(2);
    expect(spark.querySelectorAll("polyline")).toHaveLength(0);
    expect(row(2).querySelector("[data-sparkline]")).toBeNull();
    expect(cell(2, "scanned")).toContain("184 MiB");
    expect(cell(2, "failures")).toBe("0.0%");
    expect(cell(3, "failures")).toBe("1.7%11 failed");
    // Loki reports no scan.
    const scan = row(3).querySelector('[data-cell="scanned"] span')!;
    expect(scan.textContent).toBe("—");
    expect(scan.getAttribute("title")).toBe("Only ClickHouse reports what a query scanned");
  });

  it("reads a rise past 999% as a multiple and a window with none before as new", async () => {
    await mount();
    await respond(healthy);
    const queries = section("sources").querySelector('[data-source="2"] td:nth-child(2)')!.textContent;
    expect(queries).toContain("↑ 26×");
    expect(queries).not.toContain("%");
    const users = [...section("adoption").querySelectorAll('[data-table="users"] tbody tr')].map(row => row.textContent);
    expect(users[1]).toContain("new");
  });

  it("shows bytes scanned per run of a filter shape", async () => {
    await mount();
    await respond(healthy);
    const [first, second] = [...section("patterns").querySelectorAll('tbody [data-cell="scanned"]')];
    expect(first.textContent?.trim()).toBe("3.8 GiB");
    expect(second.textContent?.trim()).toBe("—");
    expect(second.querySelector("span")?.getAttribute("title")).toBe("Collected from Nov 10");
  });

  it("lists failures by reason and the pairs to investigate, each linked to its failed runs", async () => {
    await mount("/?user=5");
    await respond(healthy);
    const failures = section("failures");
    expect(failures.textContent).toContain("What is failing or timing out?");
    expect(failures.textContent).toContain("415 failed queries in this window, a 2.8% failure rate.");
    const reasons = [...failures.querySelectorAll('[data-list="reasons"] li')].map(li => li.textContent?.replace(/\s+/g, " ").trim());
    // An unknown reason keeps its code; one without a comparable prior has no delta.
    expect(reasons).toEqual(["Timed out128↑ +64%", "Invalid query94→ +4%", "gremlins3"]);
    const pairs = [...failures.querySelectorAll('[data-list="failure-pairs"] li')].map(li => li.textContent?.replace(/\s+/g, " ").trim());
    expect(pairs[0]).toBe("HTTP Access · 89 failedAlice ExampleAudit Log →");
    expect(pairs[1]).toBe("Source #2 · 12 failedError Digest Worker · Service accountAudit Log →");
    expect(hrefs(failures, "Failed runs in the Audit Log")).toEqual([
      `/admin/audit-log?action=query.run&${windowRange}&actor_id=5&target_type=source&target_id=1&outcome=failure`,
      `/admin/audit-log?action=query.run&${windowRange}&actor_id=90&target_type=source&target_id=2&outcome=failure`,
    ]);
    // The page's user filter carries into the links to every failure.
    const all = decodeURIComponent(failures.querySelector('[data-link="all-failures"]')!.getAttribute("href")!);
    expect(all).toBe(`/admin/audit-log?action=query.run&${windowRange}&actor_id=5&outcome=failure`);
    expect(failures.textContent).toContain("Latency, scan and failure data start on Nov 10.");
  });

  it("retries the same scope after a failure", async () => {
    await mount("/?source=2");
    const url = last().config.url;
    last().respond(500, { status: "error", message: "rollup unavailable", error_type: "DatabaseError" }); await flush();
    expect(root.querySelector('[role="alert"]')?.textContent).toContain("rollup unavailable");
    button("Retry").click(); await flush();
    expect(last().config.url).toBe(url);
    await respond({ ...stats, sources: [], top_users: [], service_summary: null, shapes: { ...stats.shapes, rows: [], unanalyzed_query_count: 0 }, summary: { current: { ...stats.summary.current, query_count: 0 }, prior: stats.summary.prior } });
    expect(root.textContent).toContain("No usage in this window.");
    expect(root.textContent).toContain("No filter shapes in this window.");
  });
});
