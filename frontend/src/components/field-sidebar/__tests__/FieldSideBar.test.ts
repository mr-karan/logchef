import { createApp, h, reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import { sourcesApi } from '@/api/sources'
import { useExploreStore } from '@/stores/explore'
import FieldSideBar from '../FieldSideBar.vue'

afterEach(() => {
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

// Mounting hundreds of sidebar rows takes about 3 s under coverage on CI.
const WIDE_MOUNT_TIMEOUT_MS = 15_000

const flush = async () => {
  for (let i = 0; i < 10; i++) await new Promise(resolve => setTimeout(resolve, 0))
}

it('renders a wide schema in bounded steps and loads values only for rendered fields', async () => {
  const getFieldValues = vi.spyOn(sourcesApi, 'getFieldValues').mockImplementation(async (_team, _source, fieldName, fieldType) => ({
    status: 'success',
    data: { field_name: fieldName, field_type: fieldType, is_low_cardinality: false, values: [{ value: 'a', count: 1 }], total_distinct: 20 },
  }))
  const pinia = createPinia()
  setActivePinia(pinia)
  const exploreStore = useExploreStore()
  exploreStore.setRelativeTimeRange('15m')

  const state = reactive({
    // Every tenth field auto-loads (Int64); the rest are click-to-load strings.
    fields: Array.from({ length: 1000 }, (_, i) => ({ name: `field_${String(i).padStart(4, '0')}`, type: i % 10 === 0 ? 'Int64' : 'String' })),
  })
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp({ render: () => h(FieldSideBar, { fields: state.fields, expanded: true, sourceId: 1, teamId: 1 }) })
  app.use(pinia)
  app.use(i18n)
  try {
    app.mount(host)
    await flush()
    const collapsibles = () => host.querySelectorAll('[data-slot="collapsible"]')
    const requestedFields = () => new Set(getFieldValues.mock.calls.map(call => call[2]))

    expect(collapsibles()).toHaveLength(100)
    expect(host.querySelectorAll('[data-slot="collapsible-content"]')).toHaveLength(0)
    expect(host.querySelector('[data-field-render-hint]')?.textContent).toContain('Showing 100 of 1,000 fields')
    expect(requestedFields().size).toBe(10)

    const showMore = [...host.querySelectorAll('button')].find(button => button.textContent?.trim() === 'Show more')
    if (!showMore) throw new Error('Show more button was not rendered')
    showMore.click()
    await flush()
    expect(collapsibles()).toHaveLength(200)
    expect(requestedFields().size).toBe(20)
    // Growing the list must not refetch fields that already loaded.
    expect(getFieldValues).toHaveBeenCalledTimes(20)

    const input = host.querySelector('input')
    if (!input) throw new Error('Search input was not rendered')
    input.value = 'field_099'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flush()
    expect(collapsibles()).toHaveLength(10)
    expect(host.textContent).toContain('field_0999')
    expect(host.querySelector('[data-field-render-hint]')).toBeNull()
    expect(getFieldValues).toHaveBeenCalledTimes(21)

    // Typing more narrows the list without refetching anything.
    input.value = 'field_0990'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flush()
    expect(collapsibles()).toHaveLength(1)
    expect(getFieldValues).toHaveBeenCalledTimes(21)

    // A new query invalidates every loaded value and reloads the rendered set.
    await new Promise(resolve => setTimeout(resolve, 2))
    exploreStore.resetQueryToDefaults()
    await flush()
    expect(getFieldValues).toHaveBeenCalledTimes(22)
    expect(getFieldValues.mock.calls.at(-1)?.[2]).toBe('field_0990')
  } finally {
    app.unmount()
  }
}, WIDE_MOUNT_TIMEOUT_MS)

it('auto-expands a low-cardinality field once and keeps it collapsed after the user closes it', async () => {
  vi.spyOn(sourcesApi, 'getFieldValues').mockImplementation(async (_team, _source, fieldName, fieldType) => ({
    status: 'success',
    data: {
      field_name: fieldName, field_type: fieldType, is_low_cardinality: false,
      values: [{ value: `${fieldName}-a`, count: 2 }],
      total_distinct: fieldName === 'level' ? 3 : 50,
    },
  }))
  const pinia = createPinia()
  setActivePinia(pinia)
  useExploreStore().setRelativeTimeRange('15m')
  const fields = [{ name: 'level', type: 'Int64' }, { name: 'message', type: 'String' }]
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp({ render: () => h(FieldSideBar, { fields, expanded: true, sourceId: 1, teamId: 1 }) })
  app.use(pinia)
  app.use(i18n)
  const trigger = (name: string) => [...host.querySelectorAll<HTMLButtonElement>('[data-slot="collapsible-trigger"]')]
    .find(button => button.textContent?.includes(name))
  try {
    app.mount(host)
    await flush()
    expect(host.textContent).toContain('level-a')

    trigger('level')?.click()
    await flush()
    expect(host.textContent).not.toContain('level-a')

    // Loading another field must not reopen the one the user closed.
    trigger('message')?.click()
    await flush()
    expect(host.textContent).toContain('message-a')
    expect(host.textContent).not.toContain('level-a')
  } finally {
    app.unmount()
  }
})
