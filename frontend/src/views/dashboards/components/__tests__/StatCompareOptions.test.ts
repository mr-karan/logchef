import { createApp, h, nextTick, ref } from "vue";
import { expect, it } from "vitest";
import { i18n } from "@/i18n";
import type { DashboardViz } from "@/api/dashboards";
import { resultFrame } from "@/utils/panelFrame";
import PanelResultOptions from "../PanelResultOptions.vue";

it("edits comparison and sparkline with ordering and reducer warnings",async()=>{
 const viz=ref<DashboardViz>({type:"stat",value:"n",reduce:"sum"});
 const host=document.createElement('div');
 const frame=resultFrame({columns:[{name:"n",type:"UInt64"}],rows:[{n:1},{n:2}]});
 const app=createApp({render:()=>h(PanelResultOptions,{query:{mode:"result",language:"clickhouse-sql",text:"SELECT n"},viz:viz.value,frame,"onUpdate:viz":next=>{viz.value=next;}})}).use(i18n);
 try {
  app.mount(host);
  const select=host.querySelector<HTMLSelectElement>('#stat-compare');
  if(!select) throw new Error('comparison control missing');
  select.value='previous_row';select.dispatchEvent(new Event('change'));await nextTick();
  expect(viz.value.type==='stat' && viz.value.compare?.mode).toBe('previous_row');
  expect(host.textContent).toContain('not calendar periods');
  expect(host.textContent).toContain('first or last reducer');
  expect(host.textContent).toContain('Set an order column');
  const input=host.querySelector<HTMLInputElement>('input[type=checkbox]');
  if(!input) throw new Error('sparkline control missing');input.dispatchEvent(new Event('change'));await nextTick();
  expect(viz.value.type==='stat' && viz.value.sparkline).toBe(true);
 } finally {app.unmount();}
});
