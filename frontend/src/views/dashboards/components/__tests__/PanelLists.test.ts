import { createApp, nextTick } from "vue";
import { createPinia } from "pinia";
import { afterEach, describe, expect, it } from "vitest";
import { countFrame, resultFrame, type PanelFrame } from "@/utils/panelFrame";
import { i18n } from "@/i18n";
import PanelPie from "../PanelPie.vue";
import PanelTopList from "../PanelTopList.vue";
import type { ListViz } from "@/utils/panelCharts";

const cleanups: (() => void)[] = [];
afterEach(() => cleanups.splice(0).forEach((f) => f()));
async function render(frame: PanelFrame, viz: ListViz, count = false) {
  const host = document.createElement("div");
  document.body.append(host);
  const app = viz.type === "pie" ? createApp(PanelPie, { frame, viz, count }) : createApp(PanelTopList, { frame, viz, count });
  app.use(createPinia()).use(i18n).mount(host);
  cleanups.push(() => { app.unmount(); host.remove(); });
  await nextTick();
  return host;
}
const columns = [{ name: "service", type: "String" }, { name: "n", type: "UInt64" }];
const source = resultFrame({ columns, rows: [{ service: "a very long category name that should truncate", n: "990" }, { service: "web", n: "6" }, { service: "db", n: "4" }] });

describe("PanelTopList and PanelPie", () => {
  it("renders bars in query order with a long-label tooltip, row bound and notices", async () => {
    const host = await render({ ...source, notices: ["Top groups only"] }, { type: "toplist", label: "service", value: "n", max_rows: 2 });
    expect(host.querySelectorAll(".panel-toplist__row")).toHaveLength(2);
    expect(host.querySelector(".panel-toplist__row")?.getAttribute("title")).toContain("very long");
    expect([...host.querySelectorAll(".panel-toplist__value")].map((e) => e.textContent)).toEqual(["990", "6"]);
    expect(host.textContent).toContain("2 of 3 rows shown");
    expect(host.textContent).toContain("Top groups only");
  });
  it("renders a real pie and requested remainder in its legend", async () => {
    const host = await render(source, { type: "pie", label: "service", value: "n", min_slice_percent: 1, donut: true });
    expect(host.querySelector("svg")).not.toBeNull();
    expect(host.querySelectorAll(".panel-pie__item")).toHaveLength(2);
    expect(host.textContent).toContain("Other10 · 1.0%");

  });
  it.each(["pie", "toplist"] as const)("renders a duplicate error in %s", async (type) => {
    const host = await render(resultFrame({ columns, rows: [{ service: "web", n: "10" }, { service: "web", n: "20" }] }), { type, label: "service", value: "n" });
    expect(host.querySelector("[role=alert]")?.textContent).toContain("1 duplicate key(s)");
  });
  it.each(["pie", "toplist"] as const)("keeps structural count identities in %s", async (type) => {
    const frame = countFrame([
      { bucket: "2026-11-10T00:00:00Z", group_value: "Other", log_count: 10 },
      { bucket: "2026-11-10T00:00:00Z", is_other: true, log_count: 9 },
      { bucket: "2026-11-10T00:00:00Z", is_null: true, log_count: 8 },
      { bucket: "2026-11-10T00:00:00Z", group_value: "", log_count: 7 },
    ], "1h", true);
    const host = await render(frame, { type }, true);
    expect(host.querySelectorAll(type === "pie" ? ".panel-pie__item" : ".panel-toplist__row")).toHaveLength(4);
    expect(host.textContent).toContain("(null)");
    expect(host.textContent).toContain("(empty)");
    expect(host.querySelector("[role=alert]")).toBeNull();
  });
  it("preserves a NULL toplist value as a row", async () => {
    const host = await render(resultFrame({ columns, rows: [{ service: "a", n: null }, { service: "b", n: "2" }] }), { type: "toplist", label: "service", value: "n", max_rows: 1 });
    expect(host.querySelector(".panel-toplist__value")?.textContent).toBe("NULL");
    expect(host.textContent).toContain("1 of 2 rows shown");
  });
});

it.each(['pie', 'toplist'] as const)('uses the range-empty message for %s', async type => {
  const host = await render(resultFrame({ columns: [], rows: [] }), { type });
  expect(host.textContent).toContain('No data for this time range');
});
