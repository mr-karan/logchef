import { createApp, nextTick } from "vue";
import { createPinia } from "pinia";
import { afterEach, describe, expect, it } from "vitest";
import { countFrame, resultFrame, type PanelFrame } from "@/utils/panelFrame";
import type { ChartViz } from "@/utils/panelCharts";
import PanelChart from "../PanelChart.vue";

const cleanups: (() => void)[] = [];
afterEach(() => cleanups.splice(0).forEach((f) => f()));
async function render(frame: PanelFrame, viz: ChartViz, count = false) {
  const host = document.createElement("div");
  document.body.append(host);
  const app = createApp(PanelChart, { frame, viz, count, height: 260, range: { start: 0, end: Date.parse("2026-11-13"), zone: "UTC" } }).use(createPinia());
  app.mount(host);
  cleanups.push(() => { app.unmount(); host.remove(); });
  await nextTick();
  return host;
}
const daily = resultFrame({ columns: [{ name: "day", type: "Date" }, { name: "a", type: "UInt64" }, { name: "b", type: "UInt64" }, { name: "c", type: "UInt64" }], rows: [{ day: "2026-11-12", a: "30", b: "20", c: "10" }, { day: "2026-11-10", a: "10", b: "5", c: "2" }] });

describe("PanelChart", () => {
  it("draws one bar per date in query order and shows value labels", async () => {
    const host = await render(daily, { type: "chart", x: "day", y: ["a"], mark: "bar", x_scale: "category", show_values: true });
    const bars = [...host.querySelectorAll(".panel-chart__bar")];
    expect(bars).toHaveLength(2);
    expect(Number(bars[0].getAttribute("x"))).toBeLessThan(Number(bars[1].getAttribute("x")));
    expect([...host.querySelectorAll(".panel-chart__tick")].map((e) => e.textContent)).toEqual(["2026-11-122026-11-12", "2026-11-102026-11-10"]);
    expect([...host.querySelectorAll(".panel-chart__value")].map((e) => e.textContent)).toEqual(["30", "10"]);
  });
  it.each(["line", "area"] as const)("draws a daily %s in time order without histogram padding", async (mark) => {
    const host = await render(daily, { type: "chart", x: "day", y: ["a"], mark });
    expect(host.querySelectorAll("circle")).toHaveLength(2);
    expect(host.querySelectorAll("path")).toHaveLength(1);
    expect(host.querySelector("path")?.getAttribute("fill")).toBe(mark === "area" ? "var(--chart-1)" : "none");
    expect(host.querySelector(".panel-chart__point")?.getAttribute("aria-label")).toContain("2026-11-10");
  });
  it("stacks three metrics in stored order and groups them when stack is none", async () => {
    const stacked = await render(daily, { type: "chart", x: "day", y: ["c", "a", "b"], mark: "bar", stack: "stacked" });
    expect(stacked.querySelectorAll("rect")).toHaveLength(6);
    expect([...stacked.querySelectorAll(".panel-chart__legend span")].map((e) => e.textContent)).toEqual(["c", "a", "b"]);
    const bars = [...stacked.querySelectorAll("rect")];
    expect(bars[0].getAttribute("x")).toBe(bars[1].getAttribute("x"));
    const grouped = await render(daily, { type: "chart", x: "day", y: ["a", "b"], mark: "bar", stack: "none" });
    expect(grouped.querySelectorAll("rect")[0].getAttribute("x")).not.toBe(grouped.querySelectorAll("rect")[1].getAttribute("x"));
  });
  it("keeps result series separate and rejects duplicate keys", async () => {
    const columns = [{ name: "x", type: "String" }, { name: "s", type: "String" }, { name: "n", type: "UInt32" }];
    const rows = [{ x: "x", s: "a", n: 10 }, { x: "x", s: "b", n: 20 }];
    const viz: ChartViz = { type: "chart", x: "x", y: ["n"], series: "s", mark: "bar" };
    const host = await render(resultFrame({ columns, rows }), viz);
    expect(host.querySelectorAll("rect")).toHaveLength(2);
    expect(host.querySelectorAll(".panel-chart__legend span")).toHaveLength(2);
    const bad = await render(resultFrame({ columns, rows: [...rows, rows[0]] }), viz);
    expect(bad.querySelector("[role=alert]")?.textContent).toContain('1 duplicate key(s). First key: x, a. Group the query by "x" and "s".');
    expect(bad.querySelector("svg")).toBeNull();
  });
  it("labels approximate coordinates and exposes exact digits in inert tooltip text", async () => {
    const malicious = '<img src=x onerror="alert(1)">';
    const source = resultFrame({ columns: [{ name: "x", type: "String" }, { name: "n", type: "UInt64" }], rows: [{ x: malicious, n: "9007199254740993" }] });
    const host = await render(source, { type: "chart", x: "x", y: ["n"], mark: "bar", show_values: true });
    expect(host.querySelector(".panel-chart__value")?.textContent).toContain("≈");
    expect(host.textContent).toContain("coordinates are approximate");
    expect(host.querySelector(".panel-chart__approximate")?.textContent).toContain("≈");
    host.querySelector(".panel-chart__point")?.dispatchEvent(new MouseEvent("mouseenter"));
    await nextTick();
    expect(host.querySelector("[role=tooltip]")?.textContent).toContain("9,007,199,254,740,993");
    expect(host.querySelector("[role=tooltip]")?.textContent).toContain(malicious);
    expect(host.querySelector("img")).toBeNull();
  });
  it("renders count Other, NULL, empty and remainder as four series", async () => {
    const source = countFrame([
      { bucket: "2026-11-10T00:00:00Z", log_count: 1, group_value: "Other" },
      { bucket: "2026-11-10T00:00:00Z", log_count: 2, is_null: true },
      { bucket: "2026-11-10T00:00:00Z", log_count: 3, group_value: "" },
      { bucket: "2026-11-10T00:00:00Z", log_count: 4, is_other: true },
    ], "1h", true, "Top groups only");
    const host = await render(source, { type: "chart", mark: "bar", stack: "stacked" }, true);
    expect(host.querySelectorAll("rect")).toHaveLength(4);
    expect([...host.querySelectorAll(".panel-chart__legend span")].map((e) => e.textContent)).toEqual(["Other", "(null)", "(empty)", "Other"]);
    expect(host.textContent).toContain("Top groups only");
  });
  it("draws horizontal bars and hides the legend on request", async () => {
    const host = await render(daily, { type: "chart", x: "day", y: ["a", "b"], mark: "bar", orientation: "horizontal", legend: false });
    expect(host.querySelector("rect")?.getAttribute("width")).not.toBe("0");
    expect(host.querySelector(".panel-chart__legend")).toBeNull();
  });
});
