import { afterEach, describe, expect, it } from "vitest";
import { createApp, nextTick, type App } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/i18n";
import { controlledApi } from "@/test/controlledApi";
import { useMetaStore } from "@/stores/meta";
import type { QueryActivityResponse, QueryStatsResponse } from "@/api/admin";
import QueryActivity from "../QueryActivity.vue";

const stats: QueryStatsResponse = {
  since: "2026-11-01", days: 3,
  summary: { query_count: 12345, active_users: 42, active_sources: 23, avg_duration_ms: 456 },
  top_sources: Array.from({ length: 10 }, (_, i) => ({ source_id: i + 1, source_name: `Source ${i + 1}`, query_count: 100 - i, avg_duration_ms: 10 })),
  top_users: Array.from({ length: 10 }, (_, i) => ({ user_id: i + 1, user_email: `user${i + 1}@example.test`, query_count: 50 - i })),
  volume_by_day: [{ date: "2026-11-01", query_count: 12345 }],
};
const record = {
  id: 1, user_id: 1, user_email: "user1@example.test", team_id: 1,
  source_id: 1, source_name: "Source 1", query_language: "logchefql",
  query_text: "level = error\nAND message contains 'inspect this entire query'", duration_ms: 25, row_count: 12,
  created_at: "2026-11-01T12:00:00Z",
};
const activity: QueryActivityResponse = {
  total: 140, window_size: 2000, window_limit: 2000,
  recent: Array.from({ length: 25 }, (_, i) => ({ ...record, id: i + 1 })),
  slowest: [{ ...record, id: 50 }], by_source: [], by_language: [],
};
let app: App | undefined;
let root: HTMLDivElement;
let network: ReturnType<typeof controlledApi>;
async function flush() {
  for (let i = 0; i < 5; i++) {
    await new Promise(resolve => setTimeout(resolve, 0));
    await nextTick();
  }
}
async function mount(demo = false) {
  network = controlledApi();
  const pinia = createPinia();
  if (demo) {
    const loading = useMetaStore(pinia).loadMeta();
    network.requests[0].succeed({ version: "test", max_query_limit: 100, demo_read_only: true });
    await loading;
  }
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: "/", component: QueryActivity },
    { path: "/admin/audit-log", name: "AdminAuditLog", component: { template: "<div />" } },
  ] });
  await router.push("/");
  root = document.createElement("div"); document.body.appendChild(root);
  app = createApp(QueryActivity);
  app.use(pinia).use(i18n).use(router).mount(root);
  await flush();
}
function request(path: string) {
  const found = [...network.requests].reverse().find(item => item.config.url?.includes(path));
  if (!found) throw new Error(`Missing request ${path}`);
  return found;
}
async function ready() {
  request("query-stats").succeed(stats);
  request("query-activity").succeed(activity);
  await flush();
}
function button(text: string) {
  const found = [...root.querySelectorAll("button")].find(item => item.textContent?.trim() === text);
  if (!found) throw new Error(`Missing button ${text}`);
  return found;
}
function recentSection() {
  const heading = [...root.querySelectorAll("h3")].find(item => item.textContent === "Recent queries");
  if (!heading?.parentElement?.parentElement) throw new Error("Missing recent section");
  return heading.parentElement.parentElement;
}
afterEach(() => {
  app?.unmount(); app = undefined; root?.remove(); network?.restore(); localStorage.clear();
});

describe("Query Activity usage and investigation", () => {
  it("uses summary metrics, all ten rankings and accessible daily values", async () => {
    await mount();
    expect(request("query-activity").config.url).toBe("/admin/query-activity?limit=100");
    await ready();
    expect([...root.querySelectorAll("dd")].map(item => item.textContent)).toEqual(["12,345", "23", "42", "456 ms"]);
    expect(button("Source 10")).toBeTruthy(); expect(button("user10@example.test")).toBeTruthy();
    const daily = root.querySelector("details");
    expect(daily?.querySelector("summary")?.textContent).toBe("View daily values");
    expect(daily?.textContent).toContain("2026-11-02");
    expect(daily?.querySelectorAll("tbody tr")).toHaveLength(3);
    expect(root.textContent).toContain("140 matches in 2,000 scanned records");
  });

  it("combines drilldowns, language and submitted search, then clears filters", async () => {
    await mount(); await ready();
    button("Source 10").click(); await flush();
    button("user10@example.test").click(); await flush();
    const input = root.querySelector<HTMLInputElement>('input[type="search"]')!;
    input.value = "needle & value"; input.dispatchEvent(new Event("input", { bubbles: true })); await flush();
    expect(network.requests.filter(item => item.config.url?.includes("query-activity"))).toHaveLength(3);
    input.closest("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); await flush();
    const select = root.querySelector<HTMLSelectElement>("#activity-language")!;
    select.value = "logql"; select.dispatchEvent(new Event("change", { bubbles: true })); await flush();
    const params = new URL(request("query-activity").config.url!, "http://fixture.test").searchParams;
    expect(Object.fromEntries(params)).toEqual({ limit: "100", source_id: "10", user_id: "10", q: "needle & value", language: "logql" });
    request("query-activity").succeed({ ...activity, total: 0, recent: [], slowest: [] }); await flush();
    expect(root.textContent).toContain("No queries match this filter.");
    expect(root.textContent).toContain("Source: Source 10");
    button("Clear filters").click(); await flush();
    expect(request("query-activity").config.url).toBe("/admin/query-activity?limit=100");
    request("query-activity").succeed(activity); await flush();
    expect(input.value).toBe(""); expect(root.querySelectorAll("dd")[0].textContent).toBe("12,345");
  });

  it("ignores superseded feed requests and usage periods", async () => {
    await mount(); await ready();
    button("Source 9").click(); await flush(); const old = request("query-activity");
    button("Source 10").click(); await flush();
    request("query-activity").succeed({ ...activity, recent: [{ ...record, query_text: "newest scope" }] }); await flush();
    old.succeed({ ...activity, recent: [{ ...record, query_text: "stale scope" }] }); await flush();
    expect(root.textContent).toContain("newest scope"); expect(root.textContent).not.toContain("stale scope");
    const days = root.querySelector<HTMLSelectElement>("#stats-days")!;
    days.value = "7"; days.dispatchEvent(new Event("change", { bubbles: true })); await flush();
    const oldStats = request("query-stats");
    days.value = "90"; days.dispatchEvent(new Event("change", { bubbles: true })); await flush();
    expect(request("query-stats").config.url).toBe("/admin/query-stats?days=90");
    request("query-stats").succeed(stats); await flush();
    oldStats.succeed({ ...stats, summary: { ...stats.summary, query_count: 999 } }); await flush();
    expect(root.querySelectorAll("dd")[0].textContent).toBe("12,345");
  });

  it("expands query text locally and shows more records without another request", async () => {
    await mount(); await ready();
    const section = recentSection(); expect(section.querySelectorAll("tbody tr")).toHaveLength(20);
    const details = section.querySelector("details")!; details.open = true;
    expect(details.querySelector("pre")?.textContent).toBe(record.query_text);
    expect(details.textContent).not.toContain(record.user_email);
    button("Show 5 more").click(); await flush();
    expect(section.querySelectorAll("tbody tr")).toHaveLength(25); expect(network.requests).toHaveLength(2);
    expect(root.querySelector('a[href="/admin/audit-log"]')).toBeTruthy();
  });

  it("preserves filters after failure and retries the same scope", async () => {
    await mount(); await ready(); button("Source 10").click(); await flush();
    const url = request("query-activity").config.url;
    request("query-activity").respond(500, { status: "error", message: "history unavailable", error_type: "DatabaseError" }); await flush();
    expect(root.querySelector('[role="alert"]')?.textContent).toContain("history unavailable");
    expect(button("Clear filters")).toBeTruthy(); button("Retry").click(); await flush();
    expect(request("query-activity").config.url).toBe(url);
    request("query-activity").succeed({ ...activity, total: 0, window_size: 0, recent: [], slowest: [] }); await flush();
    button("Clear filters").click(); await flush();
    request("query-activity").succeed({ ...activity, total: 0, window_size: 0, recent: [], slowest: [] }); await flush();
    expect(root.textContent).toContain("No recent queries."); expect(root.textContent).not.toContain("No queries match this filter.");
  });

  it("keeps recent investigation available when usage fails and handles a zero-usage window", async () => {
    await mount();
    request("query-stats").respond(500, { status: "error", message: "usage unavailable", error_type: "DatabaseError" });
    request("query-activity").succeed(activity); await flush();
    expect(root.querySelector('[role="alert"]')?.textContent).toContain("usage unavailable");
    expect(recentSection().querySelectorAll("tbody tr")).toHaveLength(20);
    button("Retry").click(); await flush();
    request("query-stats").succeed({ ...stats, summary: { query_count: 0, active_users: 0, active_sources: 0, avg_duration_ms: 0 }, top_sources: [], top_users: [], volume_by_day: [] }); await flush();
    expect(root.querySelectorAll("dd")[0].textContent).toBe("0");
    expect(root.textContent).toContain("No usage in this window.");
    expect(recentSection().querySelectorAll("tbody tr")).toHaveLength(20);
  });

  it("does not fetch retained text or offer investigation controls on the demo", async () => {
    await mount(true); request("query-stats").succeed({ ...stats, top_users: [] }); await flush();
    expect(network.requests.some(item => item.config.url?.includes("query-activity"))).toBe(false);
    expect(root.textContent).toContain("Aggregate-only on the public demo");
    expect(root.querySelector('input[type="search"]')).toBeNull(); expect(root.textContent).not.toContain(record.query_text);
    expect([...root.querySelectorAll("button")].some(item => item.textContent === "Source 10")).toBe(false);
  });
});
