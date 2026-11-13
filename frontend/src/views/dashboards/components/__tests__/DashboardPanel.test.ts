import { createApp, nextTick } from "vue";
import { createPinia } from "pinia";
import { afterEach, describe, expect, it } from "vitest";
import { i18n } from "@/i18n";
import type { DashboardPanel as Panel } from "@/api/dashboards";
import type { PanelState } from "@/stores/dashboards";
import DashboardPanel from "@/views/dashboards/components/DashboardPanel.vue";

const panel: Panel = {
  id: "t1",
  kind: "query",
  title: "Slow requests",
  team_id: 1,
  source_id: 1,
  query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1", limit: 100 },
  viz: { type: "table" },
};

let host: HTMLDivElement | null = null;

afterEach(() => {
  host?.remove();
  host = null;
});

async function render(state: PanelState, chrome = true) {
  host = document.createElement("div");
  document.body.appendChild(host);
  createApp(DashboardPanel, { panel, heightPx: 240, state, chrome }).use(createPinia()).use(i18n).mount(host);
  await nextTick();
  return host;
}

function tableState(truncation: "none" | "rows" | "bytes" | "unknown"): PanelState {
  return {
    status: "success",
    table: {
      columns: [{ name: "n", type: "UInt64" }],
      rows: [{ n: 1 }],
      stats: { execution_time_ms: 1, rows_read: 2, bytes_read: 0, rows_returned: 1, limit_applied: 1, limit_source: "request", truncation },
    },
  };
}

describe("DashboardPanel truncation chip", () => {
  it("says how many rows a cut result kept, in the header", async () => {
    const root = await render(tableState("rows"));
    expect(root.querySelector(".dash-panel__header .dash-panel__chip")?.textContent).toBe("First 1 row");
    expect(root.querySelectorAll(".dash-panel__chip")).toHaveLength(1);
  });

  it("floats the chip over the body when the panel has no header", async () => {
    const root = await render(tableState("rows"), false);
    expect(root.querySelector(".dash-panel__body .dash-panel__chip--overlay")?.textContent).toBe("First 1 row");
    expect(root.querySelectorAll(".dash-panel__chip")).toHaveLength(1);
  });

  it("names an unproven cap", async () => {
    const root = await render(tableState("unknown"));
    expect(root.querySelector(".dash-panel__chip")?.textContent).toBe("May be incomplete");
  });

  it("shows no chip when nothing was cut", async () => {
    const root = await render(tableState("none"));
    expect(root.querySelector(".dash-panel__chip")).toBeNull();
    expect(root.textContent).toContain("Slow requests");
  });
});
