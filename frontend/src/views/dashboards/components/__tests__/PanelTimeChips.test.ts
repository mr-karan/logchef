import { createApp, nextTick } from "vue";
import { createPinia } from "pinia";
import { afterEach, describe, expect, it } from "vitest";
import { i18n } from "@/i18n";
import type { DashboardQueryPanel } from "@/api/dashboards";
import type { PanelState } from "@/stores/dashboards";
import { resultFrame } from "@/utils/panelFrame";
import { epochMs, parseZoneId, type ZoneId } from "@/utils/timeWindow";
import DashboardPanel from "@/views/dashboards/components/DashboardPanel.vue";

let host: HTMLDivElement | null = null;
afterEach(() => {
  host?.remove();
  host = null;
});

function panel(query: Partial<DashboardQueryPanel["query"]>): DashboardQueryPanel {
  return {
    id: "p", kind: "query", title: "Orders", team_id: 1, source_id: 1,
    query: { mode: "result", language: "clickhouse-sql", text: "SELECT count() AS n FROM logs WHERE {{__time_filter}}", ...query },
    viz: { type: "table" },
  };
}

const success: PanelState = { status: "success", frame: resultFrame({ columns: [{ name: "n", type: "UInt64" }], rows: [{ n: "1" }] }) };

async function chips(shown: DashboardQueryPanel, state: PanelState = success, chrome = true) {
  host = document.createElement("div");
  document.body.appendChild(host);
  createApp(DashboardPanel, { panel: shown, heightPx: 240, state, chrome }).use(createPinia()).use(i18n).mount(host);
  await nextTick();
  const text = (id: string) => host?.querySelector(`[data-testid="${id}"]`)?.textContent ?? null;
  const title = (id: string) => host?.querySelector(`[data-testid="${id}"]`)?.getAttribute("title") ?? null;
  return { fixed: text("chip-fixed-window"), range: text("chip-range"), interval: text("chip-interval"), title };
}

describe("panel time chips", () => {
  it("shows no chip on a panel that follows the dashboard range with auto buckets", async () => {
    expect(await chips(panel({}))).toMatchObject({ fixed: null, range: null, interval: null });
    expect(await chips(panel({ mode: "count", language: "logchefql", text: 'level="error"', interval: "auto" }))).toMatchObject({ fixed: null, range: null, interval: null });
  });

  it("marks ClickHouse SQL that sets its own window as a fixed window", async () => {
    const shown = await chips(panel({ text: "SELECT count() AS n FROM logs WHERE ts >= today() - 10" }));
    expect(shown.fixed).toBe("Fixed window");
    expect(shown.title("chip-fixed-window")).toBe("This query sets its own time window. The time picker does not change it.");
    expect(shown.range).toBeNull();
  });

  it("never marks LogchefQL or LogsQL as a fixed window", async () => {
    expect((await chips(panel({ language: "logsql", text: "error | stats count() as n" }))).fixed).toBeNull();
    expect((await chips(panel({ mode: "count", language: "logchefql", text: 'level="error"' }))).fixed).toBeNull();
  });

  it("names a panel's own range, with the range and zone it ran over", async () => {
    const zone = parseZoneId("Asia/Kolkata") as ZoneId;
    const range = { start: epochMs(Date.parse("2026-11-05T18:30:00Z")), end: epochMs(Date.parse("2026-11-12T10:15:00Z")), zone };
    const shown = await chips(panel({ range: "today-6d" }), { ...success, range });
    expect(shown.range).toBe("Last 7 calendar days");
    expect(shown.title("chip-range")).toBe("This panel uses its own time range: Nov 6, 12:00 AM to Nov 12, 03:45 PM (Asia/Kolkata).");
    expect((await chips(panel({ range: "7d" }))).range).toBe("Last 7 days");
    expect((await chips(panel({ range: "today" }))).range).toBe("Today");
    expect((await chips(panel({ range: "yesterday" }))).range).toBe("Yesterday");
  });

  it("drops the range chip on a fixed window, which no range reaches", async () => {
    const shown = await chips(panel({ text: "SELECT 1 AS n", range: "7d" }));
    expect(shown).toMatchObject({ fixed: "Fixed window", range: null });
  });

  it("shows a fixed bucket size, and the size used when the range raised it", async () => {
    const fixed = await chips(panel({ mode: "count", language: "logsql", text: "*", interval: "24h" }), { ...success, interval: "24h", intervalRaised: false });
    expect(fixed.interval).toBe("24h");
    expect(fixed.title("chip-interval")).toBe("Bucket size 24h");

    const raised = await chips(panel({ mode: "count", language: "logsql", text: "*", interval: "10m" }), { ...success, interval: "15m", intervalRaised: true });
    expect(raised.interval).toBe("15m");
    expect(raised.title("chip-interval")).toBe("10m would give more than 1,000 buckets over this range, so the panel used 15m.");
  });

  it("keeps its chips over the body when the panel has no header", async () => {
    const shown = await chips(panel({ text: "SELECT 1 AS n", interval: "1h" }), success, false);
    expect(shown).toMatchObject({ fixed: "Fixed window", interval: "1h" });
    expect(host?.querySelector(".dash-panel__body .dash-panel__chips--overlay [data-testid='chip-fixed-window']")).not.toBeNull();
  });
});
