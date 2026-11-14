import { createApp, defineComponent, h, nextTick } from "vue";
import { createPinia } from "pinia";
import { expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import type { DashboardQueryPanel } from "@/api/dashboards";
import { resultFrame } from "@/utils/panelFrame";

const captured: { template?: (...args: unknown[]) => unknown; data?: unknown[] } = {};
vi.mock("@unovis/vue", () => {
  const stub = (name: string) => defineComponent({ name, inheritAttrs: false, setup(_, { attrs, slots }) {
    if (name === "VisCrosshair") {
      const template = attrs.template;
      captured.template = (...args) => {
        if (typeof template !== "function") throw new Error("crosshair template missing");
        return template(...args);
      };
    }
    if (name === "VisXYContainer" && Array.isArray(attrs.data)) captured.data = attrs.data;
    return () => h("div", slots.default?.());
  } });
  return { VisXYContainer: stub("VisXYContainer"), VisLine: stub("VisLine"), VisCrosshair: stub("VisCrosshair"), VisTooltip: stub("VisTooltip") };
});
import PanelStat from "../PanelStat.vue";

it("crosshair template accepts the single datum Unovis passes", async () => {
  const panel: DashboardQueryPanel = { id: "s", kind: "query", title: "", team_id: 1, source_id: 1, query: { mode: "result", language: "clickhouse-sql", text: "x" }, viz: { type: "stat", value: "n", order_by: "day", sparkline: true } };
  const frame = resultFrame({ columns: [{ name: "day", type: "Date" }, { name: "n", type: "UInt64" }], rows: [{ day: "2026-11-11", n: 1 }, { day: "2026-11-12", n: 2 }] });
  const host = document.createElement("div");
  const app = createApp(PanelStat, { frame, panel }).use(createPinia()).use(i18n);
  try {
    app.mount(host); await nextTick();
    const data = captured.data!;
    // Unovis 1.7 crosshair: config.template(datum, xValue, datamodel.data, nearestDatumIndex)
    expect(() => captured.template!(data[1], 0, data, 1)).not.toThrow();
    expect(String(captured.template!(data[1], 0, data, 1))).toContain("2026-11-12: 2");
  } finally { app.unmount(); }
});

it("announces exact points on keyboard steps outside the image, and stays silent on hover", async () => {
  const panel: DashboardQueryPanel = { id: "s", kind: "query", title: "", team_id: 1, source_id: 1, query: { mode: "result", language: "clickhouse-sql", text: "SELECT day, n" }, viz: { type: "stat", value: "n", order_by: "day", sparkline: true } };
  const frame = resultFrame({ columns: [{ name: "day", type: "Date" }, { name: "n", type: "UInt64" }], rows: [{ day: "2026-11-12", n: "9007199254740993" }, { day: "2026-11-11", n: 1 }] });
  const host = document.createElement("div");
  const app = createApp(PanelStat, { frame, panel }).use(createPinia()).use(i18n);
  try {
    app.mount(host); await nextTick();
    const image = host.querySelector<HTMLElement>('[role="img"]');
    const live = host.querySelector('[aria-live="polite"]');
    expect(image?.getAttribute("tabindex")).toBe("0");
    expect(image?.getAttribute("aria-label")).toContain("arrow keys");
    expect(live).not.toBeNull();
    expect(image?.contains(live)).toBe(false);
    expect(live?.textContent).toBe("");
    image?.dispatchEvent(new KeyboardEvent("keydown", { key: "Home" })); await nextTick();
    expect(live?.textContent).toBe("2026-11-11: 1");
    image?.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight" })); await nextTick();
    expect(live?.textContent).toBe("2026-11-12: 9,007,199,254,740,993");
    image?.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" })); await nextTick();
    expect(live?.textContent).toBe("");
    image?.dispatchEvent(new Event("pointermove"));
    const data = captured.data!;
    captured.template!(data[1], 0, data, 1); await nextTick();
    expect(live?.textContent).toBe("");
  } finally { app.unmount(); }
});
