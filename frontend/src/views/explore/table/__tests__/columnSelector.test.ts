import { afterEach, describe, expect, it } from 'vitest'
import { createApp, h, nextTick, type App } from 'vue'
import { useTable } from '@tanstack/vue-table'
import { i18n } from '@/i18n'
import ColumnSelector from '../data-table-column-selector.vue'
import { logTableFeatures, type Table } from '../tableFeatures'

let app: App | undefined

afterEach(() => {
  app?.unmount()
  app = undefined
  document.body.innerHTML = ''
})

async function mountSelector() {
  const host = document.createElement('div')
  document.body.appendChild(host)
  let table!: Table<Record<string, any>>
  app = createApp({
    setup() {
      table = useTable({
        features: logTableFeatures,
        data: [] as Record<string, any>[],
        columns: [
          { id: 'timestamp', enableHiding: false },
          { id: 'service.name' },
          { id: 'service.version' },
          { id: 'trace_id' },
        ],
        initialState: { columnVisibility: { 'service.version': false, trace_id: false } },
      })
      return () => h(ColumnSelector, { table })
    },
  })
  app.use(i18n)
  app.mount(host)
  const trigger = host.querySelector<HTMLButtonElement>('button')!
  const toggle = async () => {
    trigger.click()
    await nextTick()
    await nextTick()
  }
  await toggle()
  const search = async (text: string) => {
    const input = document.body.querySelector<HTMLInputElement>('input[type="search"]')!
    input.value = text
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
  }
  return { table, trigger, toggle, search }
}

function checkbox(id: string) {
  const element = document.getElementById(id)
  expect(element).not.toBeNull()
  return element as HTMLButtonElement
}

function listedFields() {
  return [...document.body.querySelectorAll('[role="checkbox"]')]
    .map(element => element.id).filter(id => id !== 'select-all')
}

describe('table column selector search', () => {
  it('filters hideable fields by a trimmed, case-insensitive substring', async () => {
    const { search } = await mountSelector()
    expect(listedFields()).toEqual(['service.name', 'service.version', 'trace_id'])
    await search('  SERVICE.  ')
    expect(listedFields()).toEqual(['service.name', 'service.version'])
    await search('VERSION')
    expect(listedFields()).toEqual(['service.version'])
    await search('   ')
    expect(listedFields()).toHaveLength(3)
  })

  it('preserves selected fields when searching for another field and reopening', async () => {
    const { search, table, toggle, trigger } = await mountSelector()
    await search('trace')
    checkbox('trace_id').click()
    await nextTick()
    await search('version')
    checkbox('service.version').click()
    await nextTick()
    expect(table.getColumn('trace_id')!.getIsVisible()).toBe(true)
    expect(trigger.textContent).toContain('(3)')
    await toggle()
    await toggle()
    expect(document.body.querySelector<HTMLInputElement>('input[type="search"]')!.value).toBe('')
    expect(listedFields()).toHaveLength(3)
    expect(checkbox('trace_id').getAttribute('aria-checked')).toBe('true')
    expect(checkbox('service.version').getAttribute('aria-checked')).toBe('true')
  })

  it('selects and clears only matching fields, preserving fields outside the search', async () => {
    const { search, table } = await mountSelector()
    await search('trace')
    checkbox('trace_id').click()
    await nextTick()
    await search('service')
    expect(checkbox('select-all').getAttribute('aria-checked')).toBe('mixed')
    checkbox('select-all').click()
    await nextTick()
    expect(checkbox('service.name').getAttribute('aria-checked')).toBe('true')
    expect(checkbox('service.version').getAttribute('aria-checked')).toBe('true')
    expect(checkbox('select-all').getAttribute('aria-checked')).toBe('true')
    checkbox('select-all').click()
    await nextTick()
    expect(table.getColumn('service.name')!.getIsVisible()).toBe(false)
    expect(table.getColumn('service.version')!.getIsVisible()).toBe(false)
    expect(table.getColumn('trace_id')!.getIsVisible()).toBe(true)
    expect(table.getColumn('timestamp')!.getIsVisible()).toBe(true)
  })

  it('shows an empty result without changing selection and restores the list on clear', async () => {
    const { search, table } = await mountSelector()
    await search('missing')
    expect(listedFields()).toEqual([])
    expect(document.getElementById('select-all')).toBeNull()
    expect(document.body.textContent).toContain('No results')
    expect(table.getColumn('service.name')!.getIsVisible()).toBe(true)
    await search('')
    expect(listedFields()).toHaveLength(3)
    checkbox('select-all').click()
    await nextTick()
    expect(listedFields().every(id => table.getColumn(id)!.getIsVisible())).toBe(true)
  })
})
