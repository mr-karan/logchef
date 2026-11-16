import { createApp, defineComponent, nextTick, type PropType } from "vue";
import { createPinia } from "pinia";
import { afterEach, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import type { DashboardPanel as Panel } from "@/api/dashboards";
import type { AggregationInfo } from "@/api/explore";
import type { PanelState } from "@/stores/dashboards";
import { resultFrame, type PanelFrame } from "@/utils/panelFrame";
import { epochMs, parseZoneId } from "@/utils/timeWindow";
import DashboardPanel from "../DashboardPanel.vue";

// The chart and the table record the frame they are given.
const shown = vi.hoisted(() => ({ frames: [] as PanelFrame[] }));
function capture(name: string) {
  return defineComponent({
    name,
    props: { frame: { type: Object as PropType<PanelFrame>, required: true } },
    setup(props) {
      shown.frames.push(props.frame);
      return () => null;
    },
  });
}
vi.mock("../PanelChart.vue", () => ({ default: capture("PanelChart") }));
vi.mock("../PanelTable.vue", () => ({ default: capture("PanelTable") }));

let host: HTMLDivElement | null = null;
afterEach(() => {
  host?.remove();
  host = null;
  shown.frames = [];
});

const aggregation: AggregationInfo = { shape: "timeseries", interval: "5m", limit: 0, columns: [{ name: "time", role: "time", type: "DateTime" }, { name: "count", role: "value", type: "UInt64" }] };
const state: PanelState = {
  status: "success",
  frame: { ...resultFrame({ columns: [{ name: "time", type: "DateTime" }, { name: "count", type: "UInt64" }], rows: [{ time: "2026-11-12T00:00:00Z", count: "4" }, { time: "2026-11-12T00:10:00Z", count: "2" }] }), interval: "5m" },
  aggregation,
  alignment: "calendar",
  range: { start: epochMs(Date.parse("2026-11-12T00:00:00Z")), end: epochMs(Date.parse("2026-11-12T00:10:00Z")), zone: parseZoneId("UTC")! },
};

async function render(viz: Panel["viz"]) {
  const panel: Panel = { id: "agg", kind: "query", title: "Errors", team_id: 1, source_id: 1, query: { mode: "result", language: "logchefql", text: "| count() by time(5m)" }, viz };
  host = document.createElement("div");
  document.body.appendChild(host);
  createApp(DashboardPanel, { panel, heightPx: 240, state }).use(createPinia()).use(i18n).mount(host);
  await nextTick();
  return shown.frames.at(-1);
}

const times = (frame: PanelFrame | undefined) => frame?.rows.map((row) => Date.parse(String(row.time))).sort((a, b) => a - b);

it("charts an aggregation with its empty buckets as zero counts", async () => {
  const frame = await render({ type: "chart", x: "time", y: ["count"] });
  expect(times(frame)).toEqual(["00:00", "00:05", "00:10"].map((t) => Date.parse(`2026-11-12T${t}:00Z`)));
  expect(frame?.rows.find((row) => row.time === "2026-11-12T00:05:00.000Z")?.count).toMatchObject({ coefficient: 0n });
});

it("tables an aggregation with the rows the query returned", async () => {
  const frame = await render({ type: "table" });
  expect(frame?.rows).toHaveLength(2);
});
