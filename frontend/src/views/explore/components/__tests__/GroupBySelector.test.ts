import { createApp, nextTick } from 'vue'
import { expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import GroupBySelector from '../GroupBySelector.vue'

const setGroupByField = vi.hoisted(() => vi.fn())
vi.mock('@/stores/explore', () => ({ useExploreStore: () => ({ groupByField: '', setGroupByField }) }))
vi.mock('@/stores/sources', () => ({ useSourcesStore: () => ({
  currentSourceDetails: { _meta_ts_field: 'timestamp', _meta_severity_field: 'level', sort_keys: ['service'] },
}) }))

it('does not mount a large schema while closed and bounds options when opened', async () => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
  HTMLElement.prototype.scrollIntoView = vi.fn()
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(GroupBySelector, {
    availableFields: [
      ...['timestamp', 'level', 'service'].map(name => ({ name, type: 'String' })),
      ...Array.from({ length: 10_000 }, (_, i) => ({ name: `field_${String(i).padStart(5, '0')}`, type: 'String' })),
    ],
  })
  app.use(i18n)
  const items = () => document.querySelectorAll('[data-item]')
  try {
    app.mount(host)
    await nextTick()
    expect(items()).toHaveLength(0)

    host.querySelector<HTMLButtonElement>('[role="combobox"]')?.click()
    await vi.waitFor(() => expect(items()).toHaveLength(100))
    // No grouping first, then recommended fields; the timestamp is never offered.
    expect(items()[0].textContent).toContain('No Grouping')
    expect(items()[1].textContent).toContain('level')
    expect(items()[2].textContent).toContain('service')
    expect(document.body.textContent).not.toMatch(/\btimestamp\b/)
    expect(document.querySelector('[data-truncation-hint]')?.textContent).toContain('Showing the first 100 of 10,003')

    const input = document.querySelector<HTMLInputElement>('[data-slot="popover-content"] input')
    if (!input) throw new Error('Search input was not rendered')
    input.value = 'field_09999'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    expect(items()).toHaveLength(1)
    expect(document.querySelector('[data-truncation-hint]')).toBeNull()

    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await vi.waitFor(() => expect(setGroupByField).toHaveBeenCalledWith('field_09999'))
  } finally {
    app.unmount()
    host.remove()
    HTMLElement.prototype.scrollIntoView = originalScrollIntoView
    vi.unstubAllGlobals()
  }
})
