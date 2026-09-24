export interface ColumnRange {
  /** Index of the first rendered column. */
  start: number
  /** Index one past the last rendered column. */
  end: number
}

/**
 * Picks the columns whose horizontal extent intersects the viewport, plus
 * `overscan` extra columns on each side. `leadingOffset` is the width of any
 * fixed cells before the first column. Before layout is measured, use a
 * bounded provisional viewport. Rendering every column on the initial mount
 * can exhaust memory before the ResizeObserver ever gets a chance to run.
 */
export function getVisibleColumnRange(
  widths: readonly number[],
  scrollLeft: number,
  viewportWidth: number,
  leadingOffset = 0,
  overscan = 3,
): ColumnRange {
  if (widths.length === 0) {
    return { start: 0, end: 0 }
  }

  const effectiveWidth = viewportWidth > 0 ? viewportWidth : 1024
  const viewportLeft = Math.max(0, scrollLeft - leadingOffset)
  const viewportRight = viewportLeft + effectiveWidth

  let start = widths.length
  let end = 0
  let offset = 0
  for (let index = 0; index < widths.length; index++) {
    const right = offset + widths[index]
    if (right > viewportLeft && start === widths.length) {
      start = index
    }
    if (offset < viewportRight) {
      end = index + 1
    }
    offset = right
  }

  if (start === widths.length) {
    // Scrolled past every column: keep the last one so the row is not empty.
    start = widths.length - 1
    end = widths.length
  }

  return {
    start: Math.max(0, start - overscan),
    end: Math.min(widths.length, end + overscan),
  }
}
