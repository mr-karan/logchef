import { createApp, h, nextTick } from 'vue'
import { expect, it } from 'vitest'
import CellWithActions from '../CellWithActions.vue'

it('creates actions on hover or focus and retains them while moving focus between buttons', async () => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  let clicks = 0
  const app = createApp({
    render: () => h(CellWithActions, {}, {
      default: () => 'Log value',
      actions: () => [
        h('button', { onClick: () => clicks++ }, 'Copy'),
        h('button', {}, 'Filter'),
      ],
    }),
  })
  try {
    app.mount(host)
    const cell = host.querySelector<HTMLElement>('[tabindex="0"]')
    if (!cell) throw new Error('Focusable cell was not rendered')
    expect(host.textContent).toBe('Log value')
    expect(host.querySelectorAll('button')).toHaveLength(0)
    cell.dispatchEvent(new MouseEvent('mouseenter'))
    await nextTick()
    expect(host.querySelectorAll('button')).toHaveLength(2)
    cell.dispatchEvent(new MouseEvent('mouseleave'))
    await nextTick()
    expect(host.querySelectorAll('button')).toHaveLength(0)
    cell.focus()
    await nextTick()
    const buttons = host.querySelectorAll('button')
    expect(buttons).toHaveLength(2)
    buttons[0]?.focus()
    buttons[0]?.click()
    buttons[1]?.focus()
    await nextTick()
    expect(clicks).toBe(1)
    expect(host.querySelectorAll('button')).toHaveLength(2)
    buttons[1]?.blur()
    await nextTick()
    expect(host.querySelectorAll('button')).toHaveLength(0)
  } finally {
    app.unmount()
    host.remove()
  }
})
