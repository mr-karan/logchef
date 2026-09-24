import { describe, expect, it } from 'vitest'
import { getVisibleColumnRange } from '../columnVirtualization'

const widths = Array.from({ length: 50 }, () => 100)

describe('getVisibleColumnRange', () => {
  it('bounds the initial render before the viewport is measured', () => {
    expect(getVisibleColumnRange(widths, 0, 0)).toEqual({ start: 0, end: 14 })
    expect(getVisibleColumnRange(widths, 1200, 0)).toEqual({ start: 9, end: 26 })
    expect(getVisibleColumnRange([], 0, 0)).toEqual({ start: 0, end: 0 })
  })

  it('renders the columns under the viewport plus overscan', () => {
    // Viewport covers 1000..1500px: columns 10..14, overscan 3 each side.
    expect(getVisibleColumnRange(widths, 1000, 500, 0, 3)).toEqual({ start: 7, end: 18 })
  })

  it('clamps at both edges', () => {
    expect(getVisibleColumnRange(widths, 0, 500)).toEqual({ start: 0, end: 8 })
    expect(getVisibleColumnRange(widths, 4800, 500)).toEqual({ start: 45, end: 50 })
  })

  it('accounts for fixed cells before the first column', () => {
    // 24px expander: scrollLeft 24 puts column 0 exactly at the viewport edge.
    expect(getVisibleColumnRange(widths, 124, 200, 24, 0)).toEqual({ start: 1, end: 3 })
  })

  it('keeps the last column when scrolled past the end', () => {
    expect(getVisibleColumnRange(widths, 9000, 500, 0, 0)).toEqual({ start: 49, end: 50 })
  })

  it('handles variable widths', () => {
    expect(getVisibleColumnRange([300, 50, 400, 120], 320, 200, 0, 0)).toEqual({ start: 1, end: 3 })
  })
})
