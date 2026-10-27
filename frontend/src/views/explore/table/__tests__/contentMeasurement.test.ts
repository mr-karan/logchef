import { afterEach, beforeEach, expect, it, vi } from 'vitest'

const canvasDescriptor = Object.getOwnPropertyDescriptor(HTMLCanvasElement.prototype, 'getContext')

beforeEach(() => {
  vi.resetModules()
  Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
    configurable: true,
    value: () => ({ font: '', measureText: (text: string) => ({ width: text.length * 8 }) }),
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  document.body.replaceChildren()
  document.documentElement.classList.remove('dark')
  if (canvasDescriptor) Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', canvasDescriptor)
})

function root() {
  const table = document.createElement('div')
  table.dataset.logchefTable = ''
  table.innerHTML = '<table><tbody><tr><td data-cell-id="0_s" style="padding: 0 12px; border-right: 1px solid"></td></tr></tbody></table>'
  document.body.appendChild(table)
  return table
}

it('measures non-string severity with its rendered typography', async () => {
  const css = document.createElement('style')
  css.textContent = '.px-1\\.5 { padding: 0 6px; }'
  document.body.appendChild(css)
  const { createContentMeasurer } = await import('../contentSizing')
  const measure = createContentMeasurer(root())
  for (const value of [{ detail: 'retained JSON' }, 123456789]) {
    const rows = [{ s: value }]
    expect(measure?.({ name: 's', type: 'String' }, rows, 'severity', 'utc', 's'))
      .toBe(measure?.({ name: 's', type: 'String' }, rows, 'default', 'utc', 'other'))
  }
})

it('measures tabular digits as zeros without changing header text', async () => {
  const measureText = vi.fn((text: string) => ({ width: text.length * 8 }))
  Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
    configurable: true,
    value: () => ({ font: '', measureText }),
  })
  const { createContentMeasurer } = await import('../contentSizing')
  const measure = createContentMeasurer(root())
  for (const value of ['192.168.1.100', '2024-01-15', '/api/v1/items/123', '2001:db8::1']) {
    measure?.({ name: 'field1', type: 'String' }, [{ field1: value }], 'default', 'utc', 'level')
    expect(measureText).toHaveBeenCalledWith(value.replace(/\d/g, '0'))
  }
  expect(measureText).toHaveBeenCalledWith('field1')
})

it('includes badge letter spacing in the measured width', async () => {
  const css = document.createElement('style')
  css.textContent = '.outcome-badge { letter-spacing: 2px; padding: 0 4px; }'
  document.body.appendChild(css)
  const { createContentMeasurer } = await import('../contentSizing')
  const measure = createContentMeasurer(root())
  expect(measure?.({ name: 's', type: 'String' }, [{ s: 'FAILED' }], 'severity', 'utc', 's')).toBeGreaterThanOrEqual(93)
})

it('keeps measured widths identical across theme changes', async () => {
  const css = document.createElement('style')
  css.textContent = '.outcome-badge { letter-spacing: 1px; padding: 0 4px; }'
  document.body.appendChild(css)
  const { createContentMeasurer } = await import('../contentSizing')
  const host = root()
  const column = { name: 'status', type: 'String' }
  const rows = [{ status: 'FAILED' }]
  const light = createContentMeasurer(host)?.(column, rows, 'default', 'utc', 'level')
  document.documentElement.classList.add('dark')
  const dark = createContentMeasurer(host)?.(column, rows, 'default', 'utc', 'level')
  expect(light).toBeGreaterThan(50)
  expect(dark).toBe(light)
})

it('waits for fonts loaded after an earlier measurement', async () => {
  const descriptor = Object.getOwnPropertyDescriptor(document, 'fonts')
  const fonts = { ready: Promise.resolve('first fonts') }
  Object.defineProperty(document, 'fonts', { configurable: true, value: fonts })
  try {
    const { waitForContentFonts } = await import('../contentSizing')
    await expect(waitForContentFonts()).resolves.toBe('first fonts')
    fonts.ready = Promise.resolve('new fonts')
    await expect(waitForContentFonts()).resolves.toBe('new fonts')
  } finally {
    if (descriptor) Object.defineProperty(document, 'fonts', descriptor)
    else Reflect.deleteProperty(document, 'fonts')
  }
})
