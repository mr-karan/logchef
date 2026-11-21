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
  exploreStore.setWindow({ kind: "relative", spec: "15m" })

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

    exploreStore.resetQueryToDefaults()
    await flush()
    expect(getFieldValues).toHaveBeenCalledTimes(21)

    const refresh = host.querySelector<HTMLButtonElement>(`button[aria-label="${i18n.global.t('ui.refreshFieldValues')}"]`)
    if (!refresh) throw new Error('Refresh button was not rendered')
    refresh.click()
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
  useExploreStore().setWindow({ kind: "relative", spec: "15m" })
  const fields = [{ name: 'level', type: 'Int64' }, { name: 'request', type: 'String' }]
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
    trigger('request')?.click()
    await flush()
    expect(host.textContent).toContain('request-a')
    expect(host.textContent).not.toContain('level-a')
  } finally {
    app.unmount()
  }
})

it('says a field has too many distinct values instead of listing fragments', async () => {
  const getFieldValues = vi.spyOn(sourcesApi, 'getFieldValues').mockImplementation(async (_team, _source, fieldName, fieldType) => ({
    status: 'success',
    data: {
      field_name: fieldName, field_type: fieldType, is_low_cardinality: false,
      values: [{ value: `${fieldName}-fragment`, count: 1 }],
      total_distinct: 212695,
    },
  }))
  const pinia = createPinia()
  setActivePinia(pinia)
  useExploreStore().setWindow({ kind: "relative", spec: "15m" })
  const fields = [{ name: 'msg', type: 'String' }, { name: 'request_id', type: 'String' }]
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
    expect(trigger('msg')?.textContent).toContain('String')
    expect(trigger('msg')?.textContent).not.toContain('click')

    trigger('msg')?.click()
    await flush()
    expect(getFieldValues).not.toHaveBeenCalled()
    expect(host.querySelector('[data-too-many-values]')?.textContent).toContain('Too many distinct values')

    trigger('request_id')?.click()
    await flush()
    expect(getFieldValues).toHaveBeenCalledOnce()
    expect(host.textContent).not.toContain('request_id-fragment')
    expect(host.textContent).toContain(`${(212695).toLocaleString()} distinct values: too many to list`)
  } finally {
    app.unmount()
  }
})

it('preloads Loki stream label values in one request and loads a label it skips on click', async () => {
  const labelValues = (field: string, values: string[]) => ({
    field_name: field, field_type: 'label', is_low_cardinality: true,
    values: values.map(value => ({ value, count: 0 })), total_distinct: values.length,
  })
  // The preload covers app and level only, as when pod is past its 30-label cap.
  const getAllFieldValues = vi.spyOn(sourcesApi, 'getAllFieldValues').mockResolvedValue({
    status: 'success',
    data: { app: labelValues('app', ['api', 'web']), level: labelValues('level', ['error', 'info']) },
  })
  const getFieldValues = vi.spyOn(sourcesApi, 'getFieldValues').mockImplementation(async (_team, _source, fieldName, fieldType) => ({
    status: 'success',
    data: { field_name: fieldName, field_type: fieldType, is_low_cardinality: true, values: [{ value: 'pod-a', count: 7 }], total_distinct: 1 },
  }))
  const pinia = createPinia()
  setActivePinia(pinia)
  useExploreStore().setWindow({ kind: "relative", spec: "15m" })
  const fields = [
    { name: 'app', type: 'label' }, { name: 'level', type: 'label' },
    { name: 'pod', type: 'label' }, { name: 'trace_id', type: 'String' },
  ]
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp({ render: () => h(FieldSideBar, { fields, expanded: true, sourceId: 1, teamId: 1 }) })
  app.use(pinia)
  app.use(i18n)
  const trigger = (name: string) => [...host.querySelectorAll<HTMLButtonElement>('[data-slot="collapsible-trigger"]')]
    .find(button => button.textContent?.includes(name))
  const valueButton = (value: string) => [...host.querySelectorAll<HTMLButtonElement>('button[title]')]
    .find(button => button.title.endsWith(`"${value}"`))
  try {
    app.mount(host)
    await flush()
    expect(getAllFieldValues).toHaveBeenCalledOnce()
    expect(getFieldValues).not.toHaveBeenCalled()
    // Index values have no counts, so a value row shows no count.
    expect(valueButton('api')?.textContent?.trim()).toBe('api')
    expect(valueButton('error')?.textContent?.trim()).toBe('error')

    trigger('pod')?.click()
    await flush()
    expect(getFieldValues).toHaveBeenCalledOnce()
    expect(getFieldValues.mock.calls[0][2]).toBe('pod')
    expect(valueButton('pod-a')?.textContent).toContain('7')
    expect(getAllFieldValues).toHaveBeenCalledOnce()
  } finally {
    app.unmount()
  }
})

it('lists query fields first and hides fields absent from the results until asked', async () => {
  vi.spyOn(sourcesApi, 'getFieldValues').mockImplementation(async (_team, _source, fieldName, fieldType) => ({
    status: 'success',
    data: { field_name: fieldName, field_type: fieldType, is_low_cardinality: false, values: [{ value: `${fieldName}-a`, count: 2 }], total_distinct: 50 },
  }))
  localStorage.removeItem('logchef.fieldsSidebar.onlyResultFields')
  const pinia = createPinia()
  setActivePinia(pinia)
  useExploreStore().setWindow({ kind: "relative", spec: "15m" })
  const fields = ['level', 'service', 'host', 'unused'].map(name => ({ name, type: 'String' }))
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp({ render: () => h(FieldSideBar, { fields, expanded: true, sourceId: 1, teamId: 1, queryFields: ['level'], resultFields: ['level', 'service', 'host'] }) })
  app.use(pinia)
  app.use(i18n)
  const groupFields = (id: string) => [...host.querySelectorAll(`[data-field-group="${id}"] [data-slot="collapsible-trigger"]`)].map(el => el.textContent?.replace(/String.*/, '').trim())
  try {
    app.mount(host)
    await flush()
    const groups = [...host.querySelectorAll('[data-field-group]')].map(el => el.getAttribute('data-field-group'))
    expect(groups[0]).toBe('query')
    expect(groupFields('query')).toEqual(['level'])
    expect(host.textContent).not.toContain('unused')
    expect(host.querySelector('[data-absent-fields-hint]')?.textContent).toContain('1 fields not in these results are hidden')

    host.querySelector<HTMLButtonElement>('[data-only-result-fields]')?.click()
    await flush()
    expect(groupFields('absent')).toEqual(['unused'])
    expect(host.querySelector('[data-absent-fields-hint]')).toBeNull()
    expect(localStorage.getItem('logchef.fieldsSidebar.onlyResultFields')).toBe('false')
  } finally {
    app.unmount()
    localStorage.removeItem('logchef.fieldsSidebar.onlyResultFields')
  }
})

it('offers include, exclude and copy on each value and add-as-column on the field', async () => {
  vi.spyOn(sourcesApi, 'getFieldValues').mockImplementation(async (_team, _source, fieldName, fieldType) => ({
    status: 'success',
    data: { field_name: fieldName, field_type: fieldType, is_low_cardinality: false, values: [{ value: 'error', count: 2 }], total_distinct: 1 },
  }))
  const pinia = createPinia()
  setActivePinia(pinia)
  useExploreStore().setWindow({ kind: "relative", spec: "15m" })
  const onAddFilter = vi.fn()
  const onAddColumn = vi.fn()
  const host = document.createElement('div')
  document.body.appendChild(host)
  const onRemoveColumn = vi.fn()
  const state = reactive({ shownColumns: [] as string[] })
  const app = createApp({ render: () => h(FieldSideBar, { fields: [{ name: 'level', type: 'Int64' }], expanded: true, sourceId: 1, teamId: 1, shownColumns: state.shownColumns, onAddFilter, onAddColumn, onRemoveColumn }) })
  app.use(pinia)
  app.use(i18n)
  try {
    app.mount(host)
    await flush()
    const action = (kind: string) => host.querySelector<HTMLButtonElement>(`[data-value-action="${kind}"]`)
    expect(action('copy')).not.toBeNull()
    action('include')?.click()
    action('exclude')?.click()
    expect(onAddFilter.mock.calls).toEqual([['level', 'error', '='], ['level', 'error', '!=']])
    host.querySelector<HTMLButtonElement>('button[aria-label^="Add as column"]')?.click()
    expect(onAddColumn).toHaveBeenCalledWith('level')

    // A field the table shows as a column offers to remove it instead.
    state.shownColumns = ['level']
    await flush()
    host.querySelector<HTMLButtonElement>('button[aria-label^="Remove column"]')?.click()
    expect(onRemoveColumn).toHaveBeenCalledWith('level')
    expect(onAddColumn).toHaveBeenCalledTimes(1)
  } finally {
    app.unmount()
  }
})
