import { describe, expect, it } from "vitest";
import { createApp, h, nextTick } from "vue";
import { i18n } from "@/i18n";
import type { DashboardPanelQuery } from "@/api/dashboards";
import PanelBuilderOptions from "../components/PanelBuilderOptions.vue";

describe("table builder limit", () => {
  it("caps both the input and typed values at the execution limit", async () => {
    const changes: Partial<DashboardPanelQuery>[] = [];
    const host = document.createElement("div");
    const app = createApp({
      render: () => h(PanelBuilderOptions, {
        query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1", limit: 100 },
        viz: { type: "table" },
        fieldSuggestions: [],
        "onUpdate:query": (patch) => changes.push(patch),
      }),
    });
    try {
      app.use(i18n).mount(host);
      const input = host.querySelector<HTMLInputElement>("#panel-limit");
      if (!input) throw new Error("table limit input missing");
      expect(input.max).toBe("1000");
      input.value = "10000";
      input.dispatchEvent(new Event("input", { bubbles: true }));
      await nextTick();
      expect(changes.at(-1)).toEqual({ limit: 1000 });
    } finally {
      app.unmount();
    }
  });
});
