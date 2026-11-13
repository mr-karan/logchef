import { createApp, h, nextTick, ref } from "vue";
import { createPinia } from "pinia";
import { describe, expect, it } from "vitest";
import type { DashboardQueryPanel } from "@/api/dashboards";
import { resultFrame } from "@/utils/panelFrame";
import PanelStat from "../PanelStat.vue";

 describe("stat frame rendering", () => {
  it("shows exact digits in the value and tooltip, then scales without another query", async () => {
    const panel = ref<DashboardQueryPanel>({ id: "s", kind: "query", title: "", team_id: 1, source_id: 1, query: { mode: "result", language: "clickhouse-sql", text: "SELECT n" }, viz: { type: "stat", value: "n" } });
    const host = document.createElement("div");
    const frame = resultFrame({ columns: [{ name: "n", type: "UInt64" }], rows: [{ n: "9007199254740993" }] });
    const app = createApp({ render: () => h(PanelStat, { frame, panel: panel.value }) }).use(createPinia());
    try {
      app.mount(host);
      await nextTick();
      const value = host.querySelector(".panel-stat__value");
      expect(value?.textContent).toBe("9,007,199,254,740,993");
      expect(value?.getAttribute("title")).toBe("9,007,199,254,740,993");
      panel.value = { ...panel.value, viz: { type: "stat", value: "n", format: { scale: 1e-7, decimals: 2, suffix: " Cr" } } };
      await nextTick();
      expect(value?.textContent).toBe("900,719,925.47 Cr");
    } finally { app.unmount(); }
  });
  it("keeps the latest NULL beside its date and reports ties", async () => {
    const host = document.createElement("div");
    const panel: DashboardQueryPanel = { id: "s", kind: "query", title: "", team_id: 1, source_id: 1, query: { mode: "result", language: "logsql", text: "* | stats count()" }, viz: { type: "stat", value: "n", order_by: "day", columns: { n: { kind: "number", confidence: "confirmed" }, day: { kind: "date", confidence: "confirmed" } } } };
    const frame = resultFrame({ columns: [{ name: "n", type: "String" }, { name: "day", type: "String" }], rows: [{ n: "5", day: "2026-11-10" }, { n: "6", day: "2026-11-12" }, { n: "", day: "2026-11-12" }], untyped: true });
    const app = createApp(PanelStat, { frame, panel }).use(createPinia());
    try {
      app.mount(host);
      await nextTick();
      expect(host.querySelector(".panel-stat__value")?.textContent).toBe("NULL");
      expect(host.textContent).toContain("2026-11-12");
      expect(host.textContent).toContain("2 rows share");
    } finally { app.unmount(); }
  });
});
