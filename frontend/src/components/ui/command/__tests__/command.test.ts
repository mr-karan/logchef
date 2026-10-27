import { afterEach, beforeAll, describe, expect, it } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { Command, CommandGroup, CommandInput, CommandItem, CommandList } from '..'

// jsdom has no layout, so reka's scroll-into-view on highlight needs a stand-in.
beforeAll(() => {
  if (!Element.prototype.scrollIntoView) Element.prototype.scrollIntoView = () => {}
})

let app: App | null = null
afterEach(() => {
  app?.unmount()
  app = null
  document.body.innerHTML = ''
})

const settle = async () => {
  await nextTick()
  await new Promise((resolve) => setTimeout(resolve, 5))
  await nextTick()
}

function mount(rows: string[]) {
  const text = ref('')
  const chosen: string[] = []
  const Harness = defineComponent(() => () =>
    h(Command, null, () => [
      h(CommandInput, { modelValue: text.value, 'onUpdate:modelValue': (value: string) => { text.value = value } }),
      h(CommandList, null, () => h(CommandGroup, { heading: 'Pages' }, () => rows.map((row) =>
        h(CommandItem, { key: row, value: row, onSelect: (event: Event) => { event.preventDefault(); chosen.push(row) } }, () => row)))),
    ]))
  const root = document.createElement('div')
  document.body.append(root)
  app = createApp(Harness)
  app.mount(root)
  const input = root.querySelector('input')
  if (!input) throw new Error('no input')
  return { root, input, text, chosen }
}

const key = (target: Element, name: string) => target.dispatchEvent(new KeyboardEvent('keydown', { key: name, bubbles: true, cancelable: true }))

describe('command primitives', () => {
  it('renders a labelled group of options and reports typed text', async () => {
    const { root, input, text } = mount(['Alerts', 'Sources'])
    await settle()
    expect(root.querySelector('[data-slot="command-group-heading"]')?.textContent?.trim()).toBe('Pages')
    expect([...root.querySelectorAll('[role="option"]')].map((option) => option.textContent)).toEqual(['Alerts', 'Sources'])
    input.value = 'ale'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    expect(text.value).toBe('ale')
  })

  it('moves the highlight with the arrows and chooses it with Enter, again and again', async () => {
    const { input, chosen } = mount(['Alerts', 'Sources'])
    await settle()
    key(input, 'ArrowDown')
    key(input, 'ArrowDown')
    await settle()
    key(input, 'Enter')
    key(input, 'Enter')
    await settle()
    expect(chosen).toEqual(['Sources', 'Sources'])
  })

  it('leaves Ctrl+Enter to the caller', async () => {
    const { input, chosen } = mount(['Alerts'])
    await settle()
    key(input, 'ArrowDown')
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }))
    await settle()
    expect(chosen).toEqual([])
  })
})
