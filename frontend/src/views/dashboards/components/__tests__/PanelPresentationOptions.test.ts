import { createApp, h, nextTick, ref } from "vue";
import { expect, it } from "vitest";
import { i18n } from "@/i18n";
import type { DashboardViz } from "@/api/dashboards";
import { resultFrame } from "@/utils/panelFrame";
import PanelPresentationOptions from "../PanelPresentationOptions.vue";

it("removes disabled presentation fields so their features can disappear", async () => {
 const viz=ref<DashboardViz>({type:"stat",color_mode:"background",thresholds:[{value:null,color:"red"}]});
 const host=document.createElement("div");
 const frame=resultFrame({columns:[{name:"ts",type:"DateTime"}],rows:[]});
 const app=createApp({render:()=>h(PanelPresentationOptions,{viz:viz.value,frame,"onUpdate:viz":next=>{viz.value=next;}})}).use(i18n);
 function button(text:string) {
  const button=[...host.querySelectorAll("button")].find(b=>b.textContent?.trim()===text);
  if (!button) throw new Error(`Missing button ${text}`);
  button.click();
 }
 try {
  app.mount(host);
  button("Value");await nextTick();expect(viz.value).not.toHaveProperty("color_mode");
  button("×");await nextTick();expect(viz.value).not.toHaveProperty("thresholds");
  viz.value={type:"table",rules:[{columns:["ts"],op:"empty",scope:"cell",color:"red"}],column_formats:{ts:{kind:"time",time:"seconds"}}};
  await nextTick();button("×");await nextTick();expect(viz.value).not.toHaveProperty("rules");
  const select=host.querySelector<HTMLSelectElement>('select[aria-label="Format"]');
  if (!select) throw new Error("Missing column format selector");
  select.value="";select.dispatchEvent(new Event("change"));await nextTick();
  expect(viz.value).not.toHaveProperty("column_formats");
 } finally { app.unmount(); }
});
