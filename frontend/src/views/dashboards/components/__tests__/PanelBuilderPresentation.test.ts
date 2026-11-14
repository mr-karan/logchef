import { createApp, h, nextTick } from "vue";
import { createPinia } from "pinia";
import { expect, it } from "vitest";
import { i18n } from "@/i18n";
import type { DashboardViz } from "@/api/dashboards";
import { resultFrame } from "@/utils/panelFrame";
import PanelBuilderOptions from "../PanelBuilderOptions.vue";

it("removing the last visible column keeps rules and column formats", async () => {
  const emitted: DashboardViz[] = [];
  const viz: DashboardViz = { type: "table", visible_columns: ["details"], rules: [{ columns: ["details"], op: "contains", value: "retry", color: "red", scope: "row" }], column_formats: { ts: { kind: "time", time: "seconds" } } };
  const host = document.createElement("div");
  const app = createApp({ render: () => h(PanelBuilderOptions, { query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1" }, viz, frame: resultFrame({ columns: [{ name: "details", type: "String" }], rows: [] }), fieldSuggestions: [], "onUpdate:viz": (v: DashboardViz) => emitted.push(v) }) }).use(createPinia()).use(i18n);
  try {
    app.mount(host);
    await nextTick();
    const chip = [...host.querySelectorAll("span")].find(s => s.textContent?.trim() === "details")?.querySelector("button");
    expect(chip).toBeTruthy();
    if (!chip) throw new Error("Missing visible column removal button");
    chip.click();
    await nextTick();

    expect(emitted.at(-1)).toMatchObject({ rules: viz.rules, column_formats: { ts: { kind: "time", time: "seconds" } } });
    expect(emitted.at(-1)).not.toHaveProperty("visible_columns");
    expect(host.querySelectorAll('input[type="checkbox"]')).toHaveLength(0);
  } finally { app.unmount(); }
});
