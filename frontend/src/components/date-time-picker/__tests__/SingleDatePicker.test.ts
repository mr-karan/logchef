import { afterEach, expect, it } from 'vitest'
import { createApp, nextTick, type App } from 'vue'
import { i18n } from '@/i18n'
import SingleDatePicker from '../SingleDatePicker.vue'

process.env.TZ = 'America/Los_Angeles'
let app: App | undefined
afterEach(() => { app?.unmount(); document.body.innerHTML = '' })

async function open(includeTime: boolean, modelValue: string) {
  const emitted: (string | null)[] = []
  const host = document.createElement('div')
  document.body.appendChild(host)
  app = createApp(SingleDatePicker, { includeTime, modelValue, 'onUpdate:modelValue': (value: string | null) => emitted.push(value) })
  app.use(i18n).mount(host)
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
