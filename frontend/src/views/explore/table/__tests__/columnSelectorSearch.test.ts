import { createApp, nextTick, reactive } from 'vue'
import { i18n } from '@/i18n'
import { afterEach, expect, it } from 'vitest'
import ColumnSelector from '../data-table-column-selector.vue'

function fakeTable(names: string[], hidden: string[] = []) {
  const visibility = reactive<Record<string, boolean>>(Object.fromEntries(names.map(name => [name, !hidden.includes(name)])))
  const columns = names.map(id => ({
    id,
    getCanHide: () => true,
    getIsVisible: () => visibility[id],
    toggleVisibility: (value?: boolean) => { visibility[id] = value ?? !visibility[id] },
  }))
  return { table: { getAllColumns: () => columns } as any, visibility }
}

let cleanup: (() => void) | undefined
afterEach(() => { cleanup?.(); cleanup = undefined })

async function mountAndOpen(table: unknown) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(ColumnSelector, { table })
  app.use(i18n)
  app.mount(host)
  cleanup = () => { app.unmount(); host.remove() }
  host.querySelector<HTMLButtonElement>('button')!.click()
  await nextTick()
  await nextTick()
  const search = document.querySelector<HTMLInputElement>('input[type="search"]')
  if (!search) throw new Error('Search input was not rendered')
  return search
}

async function type(input: HTMLInputElement, value: string) {
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
}

function listedColumns() {
  return [...document.querySelectorAll<HTMLLabelElement>('label')]
    .filter(label => label.htmlFor !== 'select-all')
    .map(label => label.textContent?.trim())
}

it('filters columns by name, case-insensitively', async () => {
  const { table } = fakeTable(['timestamp', 'service_name', 'http.status', 'Service_Version'])
  const search = await mountAndOpen(table)
  expect(listedColumns()).toEqual(['timestamp', 'service_name', 'http.status', 'Service_Version'])

  await type(search, '  SERVICE ')
  expect(listedColumns()).toEqual(['service_name', 'Service_Version'])
  expect(document.querySelector('label[for="select-all"]')?.textContent).toContain('2')

  await type(search, 'nothing-matches')
  expect(listedColumns()).toEqual([])
  expect(document.querySelector('#select-all')).toBeNull()
  expect(document.body.textContent).toContain('nothing-matches')
})

it('select all only toggles the columns that match the search', async () => {
  const { table, visibility } = fakeTable(['timestamp', 'service_name', 'service_version', 'msg'], ['service_version'])
  const search = await mountAndOpen(table)
  await type(search, 'service')

  const selectAll = document.querySelector<HTMLButtonElement>('#select-all')!
  expect(selectAll.dataset.state).toBe('indeterminate')

  selectAll.click()
  await nextTick()
  expect(visibility).toEqual({ timestamp: true, service_name: true, service_version: true, msg: true })
  expect(selectAll.dataset.state).toBe('checked')

  selectAll.click()
  await nextTick()
  expect(visibility).toEqual({ timestamp: true, service_name: false, service_version: false, msg: true })
  expect(selectAll.dataset.state).toBe('unchecked')
})
