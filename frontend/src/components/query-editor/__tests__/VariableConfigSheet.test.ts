import { afterEach, expect, it, vi } from 'vitest'
import { createApp, nextTick, type App } from 'vue'
import { createPinia } from 'pinia'
import { i18n } from '@/i18n'
import { useVariableStore, type VariableState } from '@/stores/variables'
import VariableConfigSheet from '../VariableConfigSheet.vue'

let app: App | undefined
afterEach(() => { app?.unmount(); document.body.innerHTML = '' })

async function mountVariable(type: VariableState['type'], defaultValue: VariableState['defaultValue']) {
  const pinia = createPinia()
  const store = useVariableStore(pinia)
  store.setAllVariable([{ name: 'd', label: 'Day', type, value: '2026-11-12', defaultValue, inputType: 'datepicker' }])
  const host = document.createElement('div')
  document.body.appendChild(host)
  app = createApp(VariableConfigSheet, { open: true })
  app.use(pinia).use(i18n).mount(host)
  await nextTick(); await nextTick()
  return store
}
function typeSelect() {
  const select = document.querySelector<HTMLButtonElement>('[role=combobox]')
  if (!select) throw new Error('type select missing')
  return select
}
async function chooseType(label: string) {
  typeSelect().dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  await vi.waitFor(() => expect(document.querySelector('[role=option]')).not.toBeNull())
  const option = [...document.querySelectorAll<HTMLElement>('[role=option]')].find(el => el.textContent?.trim() === label)
  if (!option) throw new Error(`option ${label} missing`)
  option.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await nextTick(); await nextTick()
}
it('labels day as Date, renders a date-only picker, and switches to Date and time', async () => {
  const store = await mountVariable('day', '2026-11-12')
  expect(typeSelect().textContent?.trim()).toBe('Date')
  const picker = document.querySelector<HTMLButtonElement>('button[aria-haspopup=dialog]')
  if (!picker) throw new Error('day picker missing')
  picker.click()
  await nextTick(); await nextTick()
  expect(document.querySelector('input[placeholder="YYYY-MM-DD"]')).not.toBeNull()
  expect(document.querySelector('input[type=time]')).toBeNull()
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  await nextTick(); await nextTick()
  await chooseType('Date and time')
  expect(store.allVariables[0]?.type).toBe('date')
  expect(store.allVariables[0]?.defaultValue).toBe('2026-11-12')
  expect(typeSelect().textContent?.trim()).toBe('Date and time')
})
it.each([
  ['2026-11-12 12:34:56', '2026-11-12'],
  ['2026-11-12T12:34:56Z', '2026-11-12'],
  ['2024-02-29', '2024-02-29'],
  ['2026-02-30 12:00:00', ''],
  ['invalid', ''],
  [42, ''],
] satisfies [VariableState['defaultValue'], string][])('normalizes datetime default %s when switching to day', async (value, expected) => {
  const store = await mountVariable('date', value)
  expect(typeSelect().textContent?.trim()).toBe('Date and time')
  await chooseType('Date')
  expect(store.allVariables[0]?.type).toBe('day')
  expect(store.allVariables[0]?.defaultValue).toBe(expected)
  expect(typeSelect().textContent?.trim()).toBe('Date')
})
