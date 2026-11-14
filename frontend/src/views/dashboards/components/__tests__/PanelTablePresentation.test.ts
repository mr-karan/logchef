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
