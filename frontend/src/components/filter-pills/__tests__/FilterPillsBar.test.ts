import { createApp, h, nextTick, type Component } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import FilterPillsBar from '../FilterPillsBar.vue'
import FilterEditor from '../FilterEditor.vue'
import type { QueryFilter } from '@/utils/queryFilters'

afterEach(() => {
  document.body.innerHTML = ''
})

const flush = async () => {
  for (let i = 0; i < 10; i++) await new Promise(resolve => setTimeout(resolve, 0))
}

function mountComponent(component: Component, props: Record<string, unknown>) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp({ render: () => h(component, props) })
  app.use(i18n)
  app.mount(host)
  return { host, unmount: () => app.unmount() }
}

const byTestId = (testId: string) => document.body.querySelector<HTMLElement>(`[data-testid="${testId}"]`)
const allByTestId = (testId: string) => [...document.body.querySelectorAll<HTMLElement>(`[data-testid="${testId}"]`)]

// jsdom lacks the pointer capture and scrolling that the select calls.
Object.assign(Element.prototype, { hasPointerCapture: () => false, releasePointerCapture: () => {}, scrollIntoView: () => {} })

async function chooseOperator(key: string) {
  const trigger = byTestId('filter-operator')!
  trigger.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, button: 0, pointerType: 'mouse' }))
  trigger.click()
  await flush()
  const option = byTestId(`filter-operator-${key}`)
  if (!option) throw new Error(`Missing operator ${key}`)
  option.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, button: 0, pointerType: 'mouse' }))
  option.click()
  await flush()
}

async function setValue(testId: string, value: string) {
  const element = byTestId(testId) as HTMLInputElement | HTMLSelectElement
  element.value = value
  element.dispatchEvent(new Event(element.tagName === 'SELECT' ? 'change' : 'input'))
  await nextTick()
}

const region: QueryFilter = { field: 'cloud.region', operator: 'is', values: ['us-central1'] }

function mountBar(filters: QueryFilter[]) {
  const update = vi.fn()
  const copy = vi.fn()
  const mounted = mountComponent(FilterPillsBar, {
    filters,
    fields: ['cloud.region', 'level'],
    language: 'logsql',
    'onUpdate:filters': update,
    onCopyToQuery: copy,
  })
  return { ...mounted, update, copy }
}

describe('FilterPillsBar', () => {
  it('shows each pill with its field and value', () => {
    const { unmount } = mountBar([region, { field: 'level', operator: 'is_one_of', values: ['info', 'warn'], negate: true }])
    const pills = allByTestId('filter-pill')
    expect(pills).toHaveLength(2)
    expect(pills[0].textContent).toContain('cloud.region: us-central1')
    expect(pills[1].textContent).toContain('NOT')
    expect(pills[1].textContent).toContain('level: is not one of info, warn')
    unmount()
  })

  it('shows a custom label in place of the filter', () => {
    const { unmount } = mountBar([{ ...region, label: 'Central' }])
    expect(byTestId('filter-pill-label')?.textContent?.trim()).toBe('Central')
    unmount()
  })

  it('removes a pill with its close button', () => {
    const { update, unmount } = mountBar([region])
    byTestId('filter-pill-remove')?.click()
    expect(update).toHaveBeenLastCalledWith([])
    unmount()
  })

  it('excludes, disables and copies a pill from its menu', async () => {
    const { update, copy, unmount } = mountBar([region])
    const open = async () => {
      byTestId('filter-pill-label')?.click()
      await flush()
    }
    await open()
    byTestId('filter-negate')?.click()
    expect(update).toHaveBeenLastCalledWith([{ ...region, negate: true }])

    await open()
    byTestId('filter-disable')?.click()
    expect(update).toHaveBeenLastCalledWith([{ ...region, disabled: true }])

    await open()
    byTestId('filter-copy')?.click()
    expect(copy).toHaveBeenLastCalledWith([region])
    unmount()
  })
})

describe('FilterEditor', () => {
  it('edits a pill with a preview and a custom label', async () => {
    const save = vi.fn()
    const { unmount } = mountComponent(FilterEditor, { filter: region, fields: ['cloud.region'], language: 'logsql', onSave: save })
    expect(byTestId('filter-preview')?.textContent).toBe('cloud.region:="us-central1"')
    await chooseOperator('is_one_of')
    await setValue('filter-value', 'europe-west1')
    byTestId('filter-value')?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await nextTick()
    await setValue('filter-label', 'Europe')
    expect(byTestId('filter-preview')?.textContent).toBe('cloud.region:in("us-central1","europe-west1")')
    byTestId('filter-editor')?.dispatchEvent(new Event('submit'))
    expect(save).toHaveBeenLastCalledWith({ field: 'cloud.region', operator: 'is_one_of', values: ['us-central1', 'europe-west1'], label: 'Europe' })
    unmount()
  })

  it('needs a value unless the operator is exists', async () => {
    const { unmount } = mountComponent(FilterEditor, { fields: [], language: 'logchefql' })
    await setValue('filter-field', 'user')
    expect((byTestId('filter-save') as HTMLButtonElement).disabled).toBe(true)
    await chooseOperator('not_exists')
    expect((byTestId('filter-save') as HTMLButtonElement).disabled).toBe(false)
    expect(byTestId('filter-preview')?.textContent).toBe('user=""')
    unmount()
  })

  it('suggests values for the field and adds a picked one', async () => {
    const save = vi.fn()
    const loadValues = vi.fn(async () => ['us-central1', 'europe-west1', 'asia-east1'])
    const { unmount } = mountComponent(FilterEditor, { filter: region, fields: ['cloud.region'], language: 'logsql', loadValues, onSave: save })
    await new Promise(resolve => setTimeout(resolve, 300))
    await flush()
    expect(loadValues).toHaveBeenCalledWith('cloud.region')
    await chooseOperator('is_one_of')
    await new Promise(resolve => setTimeout(resolve, 300))
    await flush()
    byTestId('filter-value')?.dispatchEvent(new Event('focus'))
    await setValue('filter-value', 'europe')
    const options = [...document.body.querySelectorAll<HTMLElement>('[data-testid="filter-value-suggestions"] [role="option"]')]
    expect(options.map(option => option.textContent?.trim())).toEqual(['europe-west1'])
    options[0].dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await nextTick()
    byTestId('filter-editor')?.dispatchEvent(new Event('submit'))
    expect(save).toHaveBeenLastCalledWith({ field: 'cloud.region', operator: 'is_one_of', values: ['us-central1', 'europe-west1'] })
    unmount()
  })
})
