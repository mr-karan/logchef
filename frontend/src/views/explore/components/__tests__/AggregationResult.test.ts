import { ResizeObserver as PolyfillResizeObserver } from "@juggle/resize-observer";
import { createApp, h, markRaw, nextTick } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import type { AggregationInfo, ColumnInfo } from "@/api/explore";
import { epochMs, parseZoneId } from "@/utils/timeWindow";
import AggregationResult from "../AggregationResult.vue";

markRaw(PolyfillResizeObserver.prototype);

vi.mock("@/api/dashboards", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/dashboards")>();
  const summary = (id: number, name: string, can_edit: boolean) => ({ id, name, description: "", panel_count: 0, panel_types: [], tab_count: 1, revision: 1, can_edit, corrupt: false, created_at: "", updated_at: "" });
  return { ...actual, dashboardsApi: { ...actual.dashboardsApi, list: async () => ({ status: "success", data: [summary(4, "Errors", true), summary(5, "Read only", false)] }) } };
});

const cleanups: (() => void)[] = [];
afterEach(() => {
  cleanups.splice(0).forEach((f) => f());
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});

const range = { start: epochMs(Date.parse("2026-11-12T00:00:00Z")), end: epochMs(Date.parse("2026-11-12T00:02:00Z")), zone: parseZoneId("UTC")! };
const STATS = { execution_time_ms: 1, rows_read: 10, bytes_read: 100, truncation: "none" as const };

type Role = AggregationInfo["columns"][number]["role"];
function described(shape: AggregationInfo["shape"], columns: [string, Role, string][], interval = ""): { aggregation: AggregationInfo; columns: ColumnInfo[] } {
  return {
    aggregation: { shape, interval, limit: 0, columns: columns.map(([name, role, type]) => ({ name, role, type })) },
    columns: columns.map(([name, , type]) => ({ name, type })),
  };
}

async function mount(query: string, shape: ReturnType<typeof described>, rows: Record<string, unknown>[]) {
  localStorage.setItem("logchef_user_preferences", JSON.stringify({ timezone: "utc" }));
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("IntersectionObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(640);
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 640, 260));
  Object.defineProperty(SVGElement.prototype, "getComputedTextLength", { configurable: true, value() { return (this.textContent?.length ?? 0) * 6; } });
  Object.defineProperty(SVGElement.prototype, "getBBox", { configurable: true, value() { return new DOMRect(0, 0, 40, 12); } });
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: "/", component: { render: () => null } }, { path: "/dashboards/:id", component: { render: () => null } }] });
  await router.push("/");
  const conditions: [string, unknown][] = [];
  const host = document.createElement("div");
  document.body.append(host);
  const pinia = createPinia();
  setActivePinia(pinia);
  const app = createApp({
    render: () => h(AggregationResult, {
      ...shape, rows, stats: STATS, query, teamId: 1, sourceId: 2, range, alignment: "calendar",
      onAddCondition: (field: string, value: unknown) => conditions.push([field, value]),
    }),
  }).use(pinia).use(i18n).use(router);
  app.mount(host);
  cleanups.push(() => { app.unmount(); host.remove(); });
  await settle();
  return { host, conditions, router };
}

async function settle() {
  await nextTick();
  await new Promise((resolve) => setTimeout(resolve, 30));
}

const chartButton = (host: HTMLElement) => host.querySelector<HTMLButtonElement>("[data-view=chart]")!;
const tableButton = (host: HTMLElement) => host.querySelector<HTMLButtonElement>("[data-view=table]")!;
const tableText = (host: HTMLElement) => [...host.querySelectorAll("tbody tr")].map((tr) => [...tr.querySelectorAll("td")].map((td) => td.textContent?.trim()));

it("charts a timeseries by default, with each value column as a series", async () => {
  const shape = described("timeseries", [["time", "time", "DateTime"], ["count", "value", "UInt64"], ["p95_duration_ms", "value", "Float64"]], "1m");
  const { host } = await mount("| count(), p95(duration_ms) by time", shape, [
    { time: "2026-11-12T00:00:00Z", count: "4", p95_duration_ms: 12.5 },
    { time: "2026-11-12T00:02:00Z", count: "1", p95_duration_ms: null },
  ]);
  expect(chartButton(host).getAttribute("aria-pressed")).toBe("true");
  expect(host.querySelector(".panel-chart [role=alert]")).toBeNull();
  expect([...host.querySelectorAll(".panel-chart__legend span")].map((s) => s.textContent?.trim())).toEqual(["count", "p95_duration_ms"]);
  expect(host.querySelector("table")).toBeNull();
});

it("charts a grouped timeseries with its one group field as the series", async () => {
  const shape = described("grouped_timeseries", [["time", "time", "DateTime"], ["service", "group", "String"], ["count", "value", "UInt64"]], "1m");
  const { host } = await mount("| count() by time, service", shape, [
    { time: "2026-11-12T00:00:00Z", service: "api", count: "4" },
    { time: "2026-11-12T00:00:00Z", service: "web", count: "2" },
  ]);
  expect(chartButton(host).getAttribute("aria-pressed")).toBe("true");
  expect([...host.querySelectorAll(".panel-chart__legend span")].map((s) => s.textContent?.trim())).toEqual(["api", "web"]);
});

it("reads the table for two hosts in one service and bucket, with the chart disabled", async () => {
  const shape = described("grouped_timeseries", [["time", "time", "DateTime"], ["service", "group", "String"], ["host", "group", "String"], ["count", "value", "UInt64"]], "1m");
  const { host } = await mount("| count() by time, service, host", shape, [
    { time: "2026-11-12T00:00:00Z", service: "api", host: "a", count: "3" },
    { time: "2026-11-12T00:00:00Z", service: "api", host: "b", count: "2" },
  ]);
  expect(chartButton(host).disabled).toBe(true);
  expect(tableButton(host).getAttribute("aria-pressed")).toBe("true");
  expect(tableText(host)).toEqual([["2026-11-12 00:00:00", "api", "a", "3"], ["2026-11-12 00:00:00", "api", "b", "2"]]);
  chartButton(host).click();
  await settle();
  expect(host.querySelector(".panel-chart")).toBeNull();
});

it("tables a grouped result by default, adds a group condition on click, and charts a top list", async () => {
  const shape = described("grouped", [["svc", "group", "String"], ["errors", "value", "UInt64"]]);
  const { host, conditions } = await mount('level=error | count() as errors by "service" as svc', shape, [
    { svc: "api", errors: "9" },
    { svc: null, errors: "2" },
  ]);
  expect(tableButton(host).getAttribute("aria-pressed")).toBe("true");
  const buttons = host.querySelectorAll<HTMLButtonElement>("tbody button");
  expect([...buttons].map((b) => b.textContent)).toEqual(["api"]);
  buttons[0].click();
  expect(conditions).toEqual([['"service"', "api"]]);

  chartButton(host).click();
  await settle();
  expect(host.querySelector("table")).toBeNull();
  expect([...host.querySelectorAll(".panel-toplist__label")].map((l) => l.textContent)).toEqual(["api", "(null)"]);
});

it("shows one scalar value as a stat", async () => {
  const shape = described("scalar", [["count", "value", "UInt64"]]);
  const { host } = await mount("level=error | count()", shape, [{ count: "18446744073709551615" }]);
  expect(tableText(host)).toEqual([["18446744073709551615"]]);
  chartButton(host).click();
  await settle();
  expect(host.textContent).toContain("18,446,744,073,709,551,615");
});

it("has no chart for a scalar with two values", async () => {
  const shape = described("scalar", [["count", "value", "UInt64"], ["avg_bytes", "value", "Float64"]]);
  const { host } = await mount("| count(), avg(bytes)", shape, [{ count: "3", avg_bytes: 1.5 }]);
  expect(chartButton(host).disabled).toBe(true);
  expect(tableText(host)).toEqual([["3", "1.5"]]);
});

it("adds the result to a chosen dashboard as a LogchefQL result panel", async () => {
  const shape = described("timeseries", [["time", "time", "DateTime"], ["count", "value", "UInt64"]], "1m");
  const query = "level=error | count() by time";
  const { host, router } = await mount(query, shape, [{ time: "2026-11-12T00:00:00Z", count: "4" }]);
  const push = vi.spyOn(router, "push");
  host.querySelector<HTMLButtonElement>("[data-add-to-dashboard]")!.click();
  await settle();
  document.querySelector<HTMLButtonElement>("[role=dialog] button[role=combobox]")!.click();
  await settle();
  const options = [...document.querySelectorAll<HTMLElement>("[data-item]")];
  expect(options.map((o) => o.textContent?.trim())).toEqual(["Errors"]);
  options[0].click();
  await settle();
  document.querySelector<HTMLButtonElement>("[data-confirm-add]")!.click();
  await settle();
  expect(push).toHaveBeenCalledWith({
    path: "/dashboards/4",
    state: {
      logchefPanelSeed: {
        title: "count() by time", team_id: 1, source_id: 2,
        query: { mode: "result", language: "logchefql", text: query },
        viz: { type: "chart", x: "time", y: ["count"], mark: "line", stack: "none" },
      },
    },
  });
});
