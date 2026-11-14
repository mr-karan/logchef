import { createApp, h, nextTick, ref } from "vue";
import { i18n } from "@/i18n";
import { createPinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DashboardQueryPanel } from "@/api/dashboards";
import { resultFrame } from "@/utils/panelFrame";
import PanelStat from "../PanelStat.vue";

describe("stat frame rendering", () => {
  const NumberFormat = Intl.NumberFormat;
  beforeEach(() => {
    vi.spyOn(Intl, "NumberFormat").mockImplementation(function (locale, options) {
      return new NumberFormat(locale ?? "en-US", options);
    });
  });
  afterEach(() => { vi.restoreAllMocks(); });
  it("shows exact digits in the value and tooltip, then scales without another query", async () => {
    const panel = ref<DashboardQueryPanel>({ id: "s", kind: "query", title: "", team_id: 1, source_id: 1, query: { mode: "result", language: "clickhouse-sql", text: "SELECT n" }, viz: { type: "stat", value: "n" } });
    const host = document.createElement("div");
    const frame = resultFrame({ columns: [{ name: "n", type: "UInt64" }], rows: [{ n: "9007199254740993" }] });
    const app = createApp({ render: () => h(PanelStat, { frame, panel: panel.value }) }).use(createPinia()).use(i18n);
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
    const app = createApp(PanelStat, { frame, panel }).use(createPinia()).use(i18n);
    try {
      app.mount(host);
      await nextTick();
      expect(host.querySelector(".panel-stat__value")?.textContent).toBe("NULL");
      expect(host.textContent).toContain("2026-11-12");
      expect(host.textContent).toContain("2 rows share");
    } finally { app.unmount(); }
  });
});

it("renders scaled exact deltas, both dates and improvement colors", async () => {
 const panel = ref<DashboardQueryPanel>({id:"s",kind:"query",title:"",team_id:1,source_id:1,query:{mode:"result",language:"clickhouse-sql",text:"SELECT day, n FROM events"},viz:{type:"stat",value:"n",order_by:"day",compare:{mode:"previous_row",good:"up"},format:{scale:0.1,decimals:2,suffix:" M"}}});
 const frame = resultFrame({columns:[{name:"day",type:"Date"},{name:"n",type:"Decimal(30,2)"}],rows:[{day:"2026-11-12",n:"9007199254740994.02"},{day:"2026-11-09",n:"9007199254740993.01"}]});
 const host = document.createElement("div");
 const app = createApp({render:()=>h(PanelStat,{frame,panel:panel.value})}).use(createPinia()).use(i18n);
 try {
  app.mount(host); await nextTick();
  const delta=host.querySelector('.panel-stat__comparison');
  expect(delta?.textContent).toContain('+0.10 M');
  expect(delta?.textContent).toContain('2026-11-09'); expect(host.textContent).toContain('2026-11-12');
  expect(delta?.className).toContain('text-green-600');
  if(panel.value.viz.type!=="stat") throw new Error("expected stat");
  panel.value.viz.compare={mode:"previous_row",good:"down"}; await nextTick();
  expect(delta?.className).toContain('text-red-600');
  panel.value.viz.compare={mode:"previous_row",good:"neutral"}; await nextTick();
  expect(delta?.className).toContain('text-muted-foreground');
 } finally {app.unmount();}
});

it("keeps an exact value visible when sparkline coordinates overflow",async()=>{
 const host=document.createElement('div');
 const panel:DashboardQueryPanel={id:"s",kind:"query",title:"",team_id:1,source_id:1,query:{mode:"result",language:"logsql",text:"*"},viz:{type:"stat",value:"n",order_by:"day",sparkline:true,columns:{n:{kind:"number",confidence:"confirmed"},day:{kind:"date",confidence:"confirmed"}}}};
 const frame=resultFrame({columns:[{name:"day",type:"String"},{name:"n",type:"String"}],rows:[{day:"2026-11-12",n:"1e400"}]});
 const app=createApp(PanelStat,{frame,panel}).use(createPinia()).use(i18n);
 try {app.mount(host);await nextTick();expect(host.querySelector('.panel-stat__value')?.textContent).not.toBe('No data');const alert=host.querySelector('[role=alert]');expect(alert?.textContent).toContain('outside the chart coordinate range');expect(alert?.closest('[role=img]')).toBeNull();expect(host.querySelector('[role=img]')).toBeNull();expect(host.querySelector('[tabindex="0"]')).toBeNull();}finally{app.unmount();}
});

it("keeps the approximate sparkline note outside the chart image",async()=>{
 const host=document.createElement('div');
 const panel:DashboardQueryPanel={id:"s",kind:"query",title:"",team_id:1,source_id:1,query:{mode:"result",language:"logsql",text:"*"},viz:{type:"stat",value:"n",order_by:"day",sparkline:true,columns:{n:{kind:"number",confidence:"confirmed"},day:{kind:"date",confidence:"confirmed"}}}};
 const frame=resultFrame({columns:[{name:"day",type:"String"},{name:"n",type:"UInt64"}],rows:[{day:"2026-11-11",n:"9007199254740993"},{day:"2026-11-12",n:"9007199254740995"}]});
 const app=createApp(PanelStat,{frame,panel}).use(createPinia()).use(i18n);
 try {
  app.mount(host);await nextTick();
  const note=[...host.querySelectorAll('span')].find(e=>e.textContent===i18n.global.t('dashboardBuilder.ApproximateSparkline'));
  expect(note).toBeTruthy();
  expect(note?.closest('[role=img]')).toBeNull();
  expect(host.querySelector('[role=img]')).not.toBeNull();
 } finally {app.unmount();}
});

it("shows a negative delta and percent, red for good up and green for good down", async () => {
  const panel = ref<DashboardQueryPanel>({ id: "s", kind: "query", title: "", team_id: 1, source_id: 1, query: { mode: "result", language: "clickhouse-sql", text: "SELECT day, n FROM events" }, viz: { type: "stat", value: "n", order_by: "day", compare: { mode: "previous_row", good: "up" } } });
  const frame = resultFrame({ columns: [{ name: "day", type: "Date" }, { name: "n", type: "UInt64" }], rows: [{ day: "2026-11-12", n: 50 }, { day: "2026-11-11", n: 100 }] });
  const host = document.createElement("div");
  const app = createApp({ render: () => h(PanelStat, { frame, panel: panel.value }) }).use(createPinia()).use(i18n);
  try {
    app.mount(host); await nextTick();
    const delta = host.querySelector(".panel-stat__comparison");
    expect(delta?.textContent?.trim().startsWith("-50 (-50.00%)")).toBe(true);
    expect(delta?.className).toContain("text-red-600");
    if (panel.value.viz.type !== "stat") throw new Error("expected stat");
    panel.value.viz.compare = { mode: "previous_row", good: "down" }; await nextTick();
    expect(delta?.className).toContain("text-green-600");
  } finally { app.unmount(); }
});
