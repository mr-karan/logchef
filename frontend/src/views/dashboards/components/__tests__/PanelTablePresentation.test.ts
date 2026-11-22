import { createApp, h, nextTick } from 'vue';
import { createPinia } from 'pinia';
import { expect, it } from 'vitest';
import { i18n } from '@/i18n';
import { resultFrame } from '@/utils/panelFrame';
import PanelTable from '../PanelTable.vue';
it('highlights the first matching row and keeps unformatted timestamp precision', async () => {
 const frame=resultFrame({columns:[{name:'details',type:'String'},{name:'ts',type:'DateTime64(6)'},{name:'n',type:'Decimal(10,2)'}],rows:[{details:'retry failed',ts:'2026-11-12T23:05:09.123456Z',n:'1.50'}]});
 const host=document.createElement('div');
 const app=createApp({render:()=>h(PanelTable,{frame,rules:[{columns:['details'],op:'contains',value:'retry',scope:'row',color:'red'},{columns:['n'],op:'contains',value:'50',scope:'cell',color:'blue'}]})}).use(createPinia()).use(i18n);
 try {app.mount(host);await nextTick();expect(host.querySelector<HTMLElement>('tbody tr')?.style.backgroundColor).toBe('var(--panel-red-background)');expect(host.textContent).toContain('.123456');expect(host.querySelector<HTMLElement>('tbody td:last-child')?.style.backgroundColor).toBe('var(--panel-blue-background)');} finally {app.unmount();}
});

it('matches a time rule against the timestamp the table shows, and a number rule against its digits', async () => {
 const frame=resultFrame({columns:[{name:'ts',type:'DateTime64(6)'},{name:'n',type:'UInt64'}],rows:[{ts:'2026-11-12T23:05:09.123456Z',n:'1500'}]});
 const host=document.createElement('div');
 localStorage.setItem('logchef_user_preferences', JSON.stringify({ timezone: 'utc' }));
 // A medium date format shows the month name ("Nov"), which the raw ISO text never contains.
 const app=createApp({render:()=>h(PanelTable,{frame,columnFormats:{ts:{kind:'time',date:'medium'},n:{kind:'number',decimals:0}},rules:[{columns:['ts'],op:'contains',value:'Nov',scope:'row',color:'red'},{columns:['n'],op:'contains',value:'50',scope:'cell',color:'blue'}]})}).use(createPinia()).use(i18n);
 try {
  app.mount(host);await nextTick();
  expect(host.textContent).toContain('Nov');
  expect(host.querySelector<HTMLElement>('tbody tr')?.style.backgroundColor).toBe('var(--panel-red-background)');
  expect(host.querySelector<HTMLElement>('tbody td:last-child')?.style.backgroundColor).toBe('var(--panel-blue-background)');
 } finally {app.unmount();}
});

// Tables side by side on one dashboard: VictoriaLogs LogsQL (no column types,
// trailing zeros dropped) and Loki LogchefQL (_time typed DateTime, nanosecond
// values). All show _time the same way, in the viewer's zone.
it('shows time columns of every backend in one format and in the viewer zone', async () => {
 const victoria=resultFrame({columns:[{name:'_time',type:'DateTime'},{name:'_msg',type:'String'}],rows:[{_time:'2026-11-22T11:50:41.138Z',_msg:'a'}],untyped:true});
 const loki=resultFrame({columns:[{name:'_time',type:'DateTime'},{name:'_msg',type:'String'}],rows:[{_time:'2026-11-22T11:51:33.544385024Z',_msg:'b'}]});
 const trimmed=resultFrame({columns:[{name:'_time',type:'DateTime'}],rows:[{_time:'2026-11-22T11:52:00.1Z'}],untyped:true});
 localStorage.setItem('logchef_user_preferences', JSON.stringify({ timezone: 'utc' }));
 const host=document.createElement('div');
 const app=createApp({render:()=>[h(PanelTable,{frame:victoria}),h(PanelTable,{frame:loki}),h(PanelTable,{frame:trimmed})]}).use(createPinia()).use(i18n);
 try {
  app.mount(host);await nextTick();
  const times=[...host.querySelectorAll('tbody tr td:first-child')].map(td=>td.textContent?.trim());
  expect(times).toEqual(['2026-11-22 11:50:41.138','2026-11-22 11:51:33.544','2026-11-22 11:52:00.100']);
 } finally {app.unmount();localStorage.removeItem('logchef_user_preferences');}
});
