import { afterEach, describe, expect, it } from "vitest";
import { createApp, nextTick, type App } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import { i18n } from "@/i18n";
import { controlledApi } from "@/test/controlledApi";
import type { QueryStatsResponse, QueryTotals, UserQueryStat } from "@/api/admin";
import QueryActivity from "../QueryActivity.vue";

const totals = (query_count: number, total_duration_ms: number): QueryTotals => ({ query_count, total_duration_ms });
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
    current: { query_count: 14820, total_duration_ms: 14820 * 1480, active_users: 42, active_sources: 10, avg_daily_active_users: 14.6 },
    prior: { query_count: 11950, total_duration_ms: 11950 * 1000, active_users: 36, active_sources: 10, avg_daily_active_users: 12 },
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
    { source_id: 1, source_name: "HTTP Access", source_type: "clickhouse", team_id: 4, service_query_count: 178600, current: totals(4920, 4920 * 1820), prior: totals(3814, 3814 * 1200), advisor: { analyzed: true, high: 1, medium: 0, low: 2 } },
    { source_id: 2, source_name: "Order Engine", source_type: "clickhouse", team_id: 4, service_query_count: 0, current: totals(3410, 3410 * 340), prior: totals(3045, 3045 * 374), advisor: { analyzed: false, high: 0, medium: 0, low: 0 } },
    { source_id: 3, source_name: "Edge Proxy", source_type: "loki", team_id: null, service_query_count: 0, current: totals(620, 620 * 410), prior: totals(660, 660 * 406), advisor: null },
  ],
  shapes: {
    since: "2026-10-22",
    unanalyzed_query_count: 148,
    rows: [
      { source_id: 1, source_name: "HTTP Access", source_type: "clickhouse", query_language: "clickhouse-sql", shape_hash: "a", shape: [{ column: "http_referer", kind: "like" }, { column: "attrs", path: ["merchant_id"], access: "map", kind: "eq" }], max_lookback_s: 30 * 86400, current: totals(842, 842 * 4380), prior: totals(600, 600 * 4000), finding: { id: "f1", title: "Add a bloom filter index on http_referer", impact: "high" } },
      { source_id: 2, source_name: "Order Engine", source_type: "clickhouse", query_language: "logchefql", shape_hash: "b", shape: [{ column: "status", kind: "in" }], max_lookback_s: null, current: totals(100, 10000), prior: null, finding: null },
    ],
  },
  language_mix: [{ language: "logchefql", query_count: 640 }, { language: "clickhouse-sql", query_count: 310 }, { language: "logql", query_count: 50 }],
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
    expect(metric("latency")).toContain("1.48 s");
    expect(metric("latency")).toContain("↑ +48% slower");
    expect(metric("latency")).toContain("prior: 1.00 s");
    expect(metric("sources")).toContain("→ 0%");
    expect(root.textContent).not.toContain("Failure rate");
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
