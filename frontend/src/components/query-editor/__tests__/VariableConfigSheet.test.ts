import { afterEach, expect, it, vi } from 'vitest'
import { createApp, nextTick, type App } from 'vue'
import { createPinia } from 'pinia'
import { i18n } from '@/i18n'
import { useVariableStore } from '@/stores/variables'
import VariableConfigSheet from '../VariableConfigSheet.vue'

let app: App | undefined
afterEach(() => { app?.unmount(); document.body.innerHTML = '' })
it('offers separate Date and Date and time types and keeps the day picker date-only', async () => {
  const pinia = createPinia()
  const store = useVariableStore(pinia)
  store.setAllVariable([{ name: 'd', label: 'Day', type: 'day', value: '2026-11-12', inputType: 'datepicker' }])
  const host = document.createElement('div')
  document.body.appendChild(host)
  app = createApp(VariableConfigSheet, { open: true })
  app.use(pinia).use(i18n).mount(host)
  await nextTick(); await nextTick()
  const select = document.querySelector<HTMLButtonElement>('[role=combobox]')
  if (!select) throw new Error('type select missing')
  expect(select.textContent).toContain('Date')
  select.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  await nextTick(); await nextTick()
  await vi.waitFor(() => {
    const options = [...document.querySelectorAll('[role=option]')].map(el => el.textContent?.trim())
    expect(options).toContain('Date')
    expect(options).toContain('Date and time')
  })
})
