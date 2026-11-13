import { createApp, nextTick } from "vue";
import { createPinia } from "pinia";
import { afterEach, describe, expect, it } from "vitest";
import { i18n } from "@/i18n";
import type { DashboardPanel as Panel } from "@/api/dashboards";
import type { PanelState } from "@/stores/dashboards";
import { resultFrame } from "@/utils/panelFrame";
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

async function render(state: PanelState, chrome = true, shownPanel: Panel = panel) {
  host = document.createElement("div");
  document.body.appendChild(host);
  createApp(DashboardPanel, { panel: shownPanel, heightPx: 240, state, chrome }).use(createPinia()).use(i18n).mount(host);
  await nextTick();
  return host;
}

function tableState(truncation: "none" | "rows" | "bytes" | "unknown"): PanelState {
  return {
    status: "success",
    frame: resultFrame({ columns: [{ name: "n", type: "UInt64" }], rows: [{ n: "9007199254740993" }] }),
    stats: { execution_time_ms: 1, rows_read: 2, bytes_read: 0, rows_returned: 1, limit_applied: 1, limit_source: "request", truncation },
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

  it("names a byte cut that left the result empty", async () => {
    const root = await render({
      status: "empty",
      table: {
        columns: [],
        rows: [],
        stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 0, rows_returned: 0, limit_applied: 100, limit_source: "request", truncation: "bytes" },
      },
    });
    expect(root.querySelector(".dash-panel__header .dash-panel__chip")?.textContent).toBe("Cut by size");
    expect(root.textContent).toContain("No data for this time range");
  });

  it("shows no chip on an empty result that nothing cut", async () => {
    const root = await render({
      status: "empty",
      table: {
        columns: [],
        rows: [],
        stats: { execution_time_ms: 1, rows_read: 0, bytes_read: 0, rows_returned: 0, limit_applied: 100, limit_source: "request", truncation: "none" },
      },
    });
    expect(root.querySelector(".dash-panel__chip")).toBeNull();
  });

  it("shows no chip when nothing was cut", async () => {
    const root = await render(tableState("none"));
    expect(root.querySelector(".dash-panel__chip")).toBeNull();
    expect(root.textContent).toContain("Slow requests");
    expect(root.textContent).toContain("9007199254740993");
  });
});

describe("empty stat frames", () => {
  it("shows No data when an untyped query returns no rows or columns", async () => {
    const root = await render({ status: "empty", frame: resultFrame({ columns: [], rows: [] }) }, true, { ...panel, viz: { type: "stat", value: "missing" } });
    expect(root.textContent).toContain("No data");
  });
  it("shows No data for an empty result with a valid value column", async () => {
    const root = await render({ status: "empty", frame: resultFrame({ columns: [{ name: "n", type: "UInt64" }], rows: [] }) }, true, { ...panel, viz: { type: "stat", value: "n" } });
    expect(root.querySelector(".panel-stat__value")?.textContent).toBe("No data");
  });
});
