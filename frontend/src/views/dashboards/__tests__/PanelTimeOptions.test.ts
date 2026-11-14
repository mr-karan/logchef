import { afterEach, describe, expect, it } from "vitest";
import { createApp, h, nextTick, type App } from "vue";
import { i18n } from "@/i18n";
import type { DashboardPanelQuery } from "@/api/dashboards";
import type { TimeWindow } from "@/utils/timeWindow";
import PanelTimeOptions from "../components/PanelTimeOptions.vue";

let app: App | null = null;
afterEach(() => {
  app?.unmount();
  app = null;
});

const ROLLING: TimeWindow = { kind: "relative", spec: "7d" };
const CALENDAR: TimeWindow = { kind: "relative", spec: "today-6d" };
const count: DashboardPanelQuery = { mode: "count", language: "logsql", text: "*", interval: "auto" };
const result: DashboardPanelQuery = { mode: "result", language: "clickhouse-sql", text: "SELECT 1 FROM logs WHERE {{__time_filter}}" };

function mount(query: DashboardPanelQuery, dashboardWindow: TimeWindow = CALENDAR) {
  const changes: Partial<DashboardPanelQuery>[] = [];
  const host = document.createElement("div");
  app = createApp({ render: () => h(PanelTimeOptions, { query, dashboardWindow, "onUpdate:query": (patch: Partial<DashboardPanelQuery>) => changes.push(patch) }) });
  app.use(i18n).mount(host);
  const select = (id: string) => {
    const el = host.querySelector<HTMLSelectElement>(`#${id}`);
    if (!el) throw new Error(`missing #${id}`);
    return el;
  };
  const choose = async (id: string, value: string) => {
    const el = select(id);
    el.value = value;
    el.dispatchEvent(new Event("change", { bubbles: true }));
    await nextTick();
  };
  const shows = (testid: string) => host.querySelector(`[data-testid="${testid}"]`) !== null;
  return { host, changes, select, choose, shows };
}

describe("panel time options", () => {
  it("offers auto and the sixteen histogram windows", () => {
    const { select } = mount(count);
    expect([...select("panel-interval").options].map((option) => option.value)).toEqual(["auto", "1s", "5s", "10s", "15s", "30s", "1m", "5m", "10m", "15m", "30m", "1h", "2h", "3h", "6h", "12h", "24h"]);
    expect(select("panel-interval").value).toBe("auto");
  });

  it("stores a fixed bucket size, and auto by name in count mode", async () => {
    const { changes, choose } = mount(count);
    await choose("panel-interval", "24h");
    await choose("panel-interval", "auto");
    expect(changes).toEqual([{ interval: "24h" }, { interval: "auto" }]);
  });

  it("removes the interval of a result query that goes back to auto, so the feature disappears", async () => {
    const { changes, choose, select } = mount({ ...result, interval: "1h" });
    expect(select("panel-interval").value).toBe("1h");
    await choose("panel-interval", "auto");
    expect(changes).toEqual([{ interval: undefined }]);
  });

  it("stores a panel range and removes it when the panel follows the dashboard again", async () => {
    const { changes, choose, select } = mount({ ...count, range: "today-6d" });
    expect(select("panel-range").value).toBe("today-6d");
    await choose("panel-range", "7d");
    await choose("panel-range", "");
    expect(changes).toEqual([{ range: "7d" }, { range: undefined }]);
  });

  it("takes a custom range only when it matches the grammar", async () => {
    const { host, changes, choose, shows } = mount(count);
    await choose("panel-range", "custom");
    const input = host.querySelector<HTMLInputElement>("#panel-range-custom");
    if (!input) throw new Error("custom range input missing");
    const type = async (value: string) => {
      input.value = value;
      input.dispatchEvent(new Event("input", { bubbles: true }));
      await nextTick();
    };
    await type("last week");
    expect(shows("panel-range-invalid")).toBe(true);
    expect(changes).toEqual([]);
    await type("today-13d");
    expect(shows("panel-range-invalid")).toBe(false);
    expect(changes).toEqual([{ range: "today-13d" }]);
  });

  it("opens the custom input for a stored range outside the presets", () => {
    const { host, select } = mount({ ...count, range: "today-13d" });
    expect(select("panel-range").value).toBe("custom");
    expect(host.querySelector<HTMLInputElement>("#panel-range-custom")?.value).toBe("today-13d");
  });

  it("says that day buckets need a calendar range", () => {
    expect(mount({ ...count, interval: "24h" }, ROLLING).shows("panel-day-buckets-hint")).toBe(true);
    expect(mount({ ...count, interval: "24h", range: "30d" }, CALENDAR).shows("panel-day-buckets-hint")).toBe(true);
    expect(mount({ ...count, interval: "24h" }, CALENDAR).shows("panel-day-buckets-hint")).toBe(false);
    expect(mount({ ...count, interval: "24h", range: "today-6d" }, ROLLING).shows("panel-day-buckets-hint")).toBe(false);
    expect(mount({ ...count, interval: "12h" }, ROLLING).shows("panel-day-buckets-hint")).toBe(false);
    expect(mount({ ...count, interval: "24h" }, { kind: "absolute", start: 0, end: 1 } as TimeWindow).shows("panel-day-buckets-hint")).toBe(false);
  });

  it("locks the range of a ClickHouse SQL query that sets its own window", () => {
    const fixed = mount({ ...result, text: "SELECT count() FROM logs WHERE ts >= today() - 10" });
    expect(fixed.shows("panel-fixed-window-hint")).toBe(true);
    expect(fixed.select("panel-range").disabled).toBe(true);
    expect(fixed.host.textContent).toContain("Add {{__time_filter}} to follow the time picker.");
    const bound = mount(result);
    expect(bound.shows("panel-fixed-window-hint")).toBe(false);
    expect(bound.select("panel-range").disabled).toBe(false);
    expect(bound.host.textContent).toContain("Feeds {{__interval}} in the query.");
  });
});
