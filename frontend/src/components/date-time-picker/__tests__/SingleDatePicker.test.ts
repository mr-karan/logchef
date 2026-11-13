import { createPinia } from 'pinia'
import { usePreferencesStore } from '@/stores/preferences'
import { afterEach, expect, it, vi } from 'vitest'
import { createApp, nextTick, type App } from 'vue'
import { i18n } from '@/i18n'
import SingleDatePicker from '../SingleDatePicker.vue'

process.env.TZ = 'America/Los_Angeles'
let app: App | undefined
afterEach(() => { app?.unmount(); document.body.innerHTML = ''; vi.useRealTimers() })

async function open(includeTime: boolean, modelValue: string) {
  const emitted: (string | null)[] = []
  const host = document.createElement('div')
  document.body.appendChild(host)
  app = createApp(SingleDatePicker, { includeTime, modelValue, 'onUpdate:modelValue': (value: string | null) => emitted.push(value) })
  const pinia = createPinia()
  usePreferencesStore(pinia).preferences.timezone = 'utc'
  app.use(pinia).use(i18n).mount(host)
  host.querySelector('button')?.click()
  await nextTick(); await nextTick()
  return emitted
}
function apply() {
  const button = [...document.querySelectorAll('button')].find(b => b.textContent?.trim() === 'Apply')
  if (!button) throw new Error('Apply missing')
  button.click()
}
it('round-trips a day without a time control or a browser-zone shift', async () => {
  const emitted = await open(false, '2026-11-12')
  expect(document.querySelector('input[type=time]')).toBeNull()
  expect(document.querySelector<HTMLInputElement>('input')?.value).toBe('2026-11-12')
  apply()
  expect(emitted).toEqual(['2026-11-12'])
})
it('rejects impossible days instead of rolling into the next month', async () => {
  const emitted = await open(false, '2026-11-12')
  const input = document.querySelector('input')
  if (!input) throw new Error('date input missing')
  input.value = '2026-02-30'
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  apply()
  expect(emitted).toEqual([])
})
it('keeps the datetime control and timestamp output', async () => {
  const emitted = await open(true, '2026-11-12 12:34:56')
  expect(document.querySelector('input[type=time]')).not.toBeNull()
  apply()
  expect(emitted).toEqual(['2026-11-12 12:34:56'])
})

it('trims date-only drafts and labels the shortcut Today', async () => {
  const emitted = await open(false, '2026-11-12')
  expect([...document.querySelectorAll('button')].some(b => b.textContent?.trim() === 'Today')).toBe(true)
  const input = document.querySelector('input')
  if (!input) throw new Error('date input missing')
  input.value = '  2024-02-29  '
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  apply()
  expect(emitted).toEqual(['2024-02-29'])
})

it('Today uses UTC display time near midnight in a non-UTC browser zone', async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-11-13T00:30:00Z'))
  expect(new Date().getDate()).toBe(12)
  const emitted = await open(false, '2026-11-10')
  const today = [...document.querySelectorAll('button')].find(b => b.textContent?.trim() === 'Today')
  if (!today) throw new Error('Today missing')
  today.click()
  expect(emitted).toEqual(['2026-11-13'])
})

it('Now uses the display timezone for datetime variables', async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-11-13T00:30:00Z'))
  const emitted = await open(true, '2026-11-10 12:00:00')
  const button = [...document.querySelectorAll('button')].find(b => b.textContent?.trim() === 'Now')
  if (!button) throw new Error('Now missing')
  button.click()
  expect(emitted).toEqual(['2026-11-13 00:30:00'])
})

it('calendar starts in the display month and highlights the display day', async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-12-01T00:30:00Z'))
  await open(false, '')
  const calendarButton = document.querySelector<HTMLButtonElement>('input + button')
  if (!calendarButton) throw new Error('calendar button missing')
  calendarButton.click()
  await nextTick(); await nextTick()
  expect(document.querySelector('[data-slot=calendar]')?.textContent).toContain('December')
  const marked = document.querySelectorAll('[data-slot=calendar-cell-trigger][data-today]')
  expect(marked.length).toBe(1)
  expect(marked[0]?.getAttribute('data-value')).toContain('2026-12-01')
})
