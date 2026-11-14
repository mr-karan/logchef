import { readFileSync } from "node:fs";
import { Line } from "@unovis/ts";
import { ResizeObserver as PolyfillResizeObserver } from "@juggle/resize-observer";
import { createApp, h, markRaw, nextTick, ref } from "vue";
import { i18n } from "@/i18n";
import { createPinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DashboardQueryPanel } from "@/api/dashboards";
import { resultFrame } from "@/utils/panelFrame";
import PanelStat from "../PanelStat.vue";

markRaw(PolyfillResizeObserver.prototype);

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

it.each(['green', 'amber', 'red', 'blue', 'purple', 'gray'] as const)('uses the %s tile foreground for all stat text and sparkline', async color => {
 const panel = ref<DashboardQueryPanel>({ id:'s',kind:'query',title:'',team_id:1,source_id:1,query:{mode:'result',language:'clickhouse-sql',text:'SELECT day,n'},viz:{type:'stat',value:'n',order_by:'day',compare:{mode:'previous_row',good:'up'},sparkline:true,thresholds:[{value:null,color}],color_mode:'background'} });
 const frame = resultFrame({columns:[{name:'day',type:'Date'},{name:'n',type:'UInt64'}],rows:[{day:'2026-11-11',n:100},{day:'2026-11-12',n:150}]});
 const host=document.createElement('div');
 document.body.append(host);
 const widthSpy=vi.spyOn(HTMLElement.prototype,'clientWidth','get').mockReturnValue(400);
 const boundsSpy=vi.spyOn(Element.prototype,'getBoundingClientRect').mockReturnValue(new DOMRect(0,0,400,160));
 const app=createApp({render:()=>h(PanelStat,{frame,panel:panel.value})}).use(createPinia()).use(i18n);
 try {
  app.mount(host);await nextTick();
  const token=`var(--panel-${color})`;
  expect(host.querySelector<HTMLElement>('.panel-stat')?.style.backgroundColor).toBe(`var(--panel-${color}-background)`);
  for (const selector of ['.panel-stat__value','.panel-stat__comparison','.panel-stat__comparison .block','.panel-stat > .text-muted-foreground']) {
   const text=host.querySelector<HTMLElement>(selector);
   expect(text,selector).toBeTruthy();expect(text?.style.color,selector).toBe(token);
   const css=readFileSync('src/assets/index.css','utf8');
   for (const theme of [css.split('.dark {')[0],css.split('.dark {')[1]]) {
    const luminance=(value: string) => {
     const name=value.slice(4,-1);
     const hex=theme.match(new RegExp(`${name}: #([0-9a-f]{6})`))?.[1];
     if (!hex) throw new Error(`Missing palette token ${name}`);
     const channels=hex.match(/../g)!.map(c=>parseInt(c,16)/255).map(c=>c<=0.04045?c/12.92:((c+0.055)/1.055)**2.4);
     return channels[0]*0.2126+channels[1]*0.7152+channels[2]*0.0722;
    };
    const fg=luminance(text!.style.color);
    const bg=luminance(host.querySelector<HTMLElement>('.panel-stat')!.style.backgroundColor);
    expect((Math.max(fg,bg)+0.05)/(Math.min(fg,bg)+0.05),selector).toBeGreaterThanOrEqual(4.5);
   }
  }
  expect(host.querySelector('.panel-stat__comparison')?.textContent).toContain('+50');
  await vi.waitFor(() => {
   const path=host.querySelector<SVGElement>(`.panel-stat__sparkline .${Line.selectors.linePath}`);
   expect(path?.getAttribute('stroke')).toBe(token);
  });
  if(panel.value.viz.type!=='stat') throw new Error('Expected stat');
  panel.value.viz.color_mode='value';await nextTick();
  expect(host.querySelector<HTMLElement>('.panel-stat__value')?.style.color).toBe(token);
  expect(host.querySelector<HTMLElement>('.panel-stat')?.style.backgroundColor).toBe('');
 } finally {app.unmount();host.remove();widthSpy.mockRestore();boundsSpy.mockRestore();}
});
