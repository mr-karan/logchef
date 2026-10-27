import { isVNode, toRaw } from 'vue'
import type { ColumnInfo } from '@/api/explore'
import { getStandardSeverityClasses } from '@/lib/utils'
import { classifyCellValue } from './cellTypography'
import { getOutcomeTone } from './outcomeTone'
import { calendarDate, timestampCell, timestampPrecision } from './columns'

export const CONTENT_WIDTH_CAP = 300
const SAMPLE_ROWS = 50
const SAMPLE_CHARACTERS = 320

interface TextStyle {
  font: string
  padding: number
  minWidth: number
  transform: string
  letterSpacing: number
}

function readStyle(element: HTMLElement): TextStyle {
  const style = getComputedStyle(element)
  return {
    font: `${style.fontWeight} ${style.fontSize} ${style.fontFamily}`,
    padding: (parseFloat(style.paddingLeft) || 0) + (parseFloat(style.paddingRight) || 0)
      + (parseFloat(style.borderLeftWidth) || 0) + (parseFloat(style.borderRightWidth) || 0),
    minWidth: parseFloat(style.minWidth) || 0,
    transform: style.textTransform,
    letterSpacing: parseFloat(style.letterSpacing) || 0,
  }
}

function renderedText(value: unknown): string {
  if (typeof value === 'string' || typeof value === 'number') return String(value)
  if (Array.isArray(value)) return value.map(renderedText).join('')
  return isVNode(value) ? renderedText(value.children) : ''
}

const PROBE_CLASSES = {
  text: 'text-[13px]',
  mono: 'font-mono text-[13px]',
  timestamp: 'font-mono text-[13px] font-medium',
  header: 'text-sm font-medium',
  outcome: 'text-[13px] outcome-badge px-1',
  severity: `font-mono text-[13px] px-1.5 ${getStandardSeverityClasses('error')}`,
  status: 'status-code',
  method: 'http-method',
} as const

type ProbeKind = keyof typeof PROBE_CLASSES
let styleCache: Map<ProbeKind, TextStyle> | undefined

export function waitForContentFonts() {
  return document.fonts?.ready ?? Promise.resolve()
}

function probeStyles(root: HTMLElement) {
  if (styleCache) return styleCache
  const probes = document.createElement('div')
  probes.style.cssText = 'position:fixed;contain:strict;width:0;height:0;visibility:hidden;pointer-events:none'
  probes.style.font = getComputedStyle(root).font
  probes.dataset.logchefTable = ''
  const elements = new Map<ProbeKind, HTMLElement>()
  const kinds = Object.keys(PROBE_CLASSES).filter((name): name is ProbeKind => Object.hasOwn(PROBE_CLASSES, name))
  for (const kind of kinds) {
    const element = document.createElement('span')
    element.className = PROBE_CLASSES[kind]
    probes.appendChild(element)
    elements.set(kind, element)
  }
  document.body.appendChild(probes)
  const styles = new Map<ProbeKind, TextStyle>()
  elements.forEach((element, name) => styles.set(name, readStyle(element)))
  probes.remove()
  styleCache = styles
  return styles
}

export function createContentMeasurer(root: HTMLElement) {
  const context = document.createElement('canvas').getContext('2d')
  if (!context) return null
  const canvasContext = context
  const styles = probeStyles(root)
  const cell = root.querySelector<HTMLElement>('td[data-cell-id]')
  const cellPadding = cell ? readStyle(cell).padding : 25
  const cache = new Map<string, number>()
  const valueCaches = new Map<string, Map<unknown, number>>()

  function textWidth(text: string, kind: ProbeKind): number {
    const style = styles.get(kind)
    if (!style) return 0
    const displayed = style.transform === 'uppercase' ? text.toUpperCase() : text
    const key = kind + displayed
    const cached = cache.get(key)
    if (cached !== undefined) return cached
    canvasContext.font = style.font
    // Canvas ignores the tabular figures used by readable cells.
    const measuredText = kind === 'text' ? displayed.replace(/\d/g, '0') : displayed
    const width = Math.max(style.minWidth, canvasContext.measureText(measuredText).width + displayed.length * style.letterSpacing + style.padding)
    cache.set(key, width)
    return width
  }

  return (column: ColumnInfo, rows: readonly Record<string, unknown>[], columnType: string | undefined,
    timezone: 'local' | 'utc', severityField: string, mode: 'auto' | 'explicit' = 'auto'): number => {
    const headerWidth = Math.ceil(textWidth(column.name, 'header') + 32)
    const cap = Math.max(headerWidth, mode === 'auto' ? CONTENT_WIDTH_CAP : 800)
    let width = Math.max(50, headerWidth)
    const cacheKey = `${column.name}-${columnType}-${column.type}-${column.name === severityField}-${timezone}-${mode}`
    let valueWidths = valueCaches.get(cacheKey)
    if (!valueWidths) {
      valueWidths = new Map()
      valueCaches.set(cacheKey, valueWidths)
    }
    for (const row of (mode === 'auto' ? toRaw(rows).slice(0, SAMPLE_ROWS) : toRaw(rows))) {
      const value = row[column.name]
      let measured = valueWidths.get(value)
      if (measured === undefined) {
        if (value === null || value === undefined) {
          measured = textWidth('-', 'text')
        } else {
          const text = (typeof value === 'object' ? JSON.stringify(value) : String(value)).slice(0, mode === 'auto' ? SAMPLE_CHARACTERS : 1000)
          if (columnType === 'date') {
            measured = textWidth(calendarDate(value), 'mono')
          } else if (columnType === 'timestamp' || /^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?$/.test(text)) {
            measured = textWidth(renderedText(timestampCell(typeof value === 'number' ? value : text,
              timezone, timestampPrecision(column.type))), 'timestamp')
          } else if (column.name === severityField && typeof value === 'string'
            && (getStandardSeverityClasses(text) || !getOutcomeTone(text, column.name, severityField))) {
            measured = textWidth(text, 'severity')
          } else if (columnType === 'status' && /^\d{3}$/.test(text)) {
            measured = textWidth(text, 'status')
          } else {
            const kind = classifyCellValue(value)
            measured = textWidth(text, typeof value === 'object' || kind === 'identifier' || kind === 'structured' ? 'mono' : 'text')
            if (/\b(GET|POST|PUT|DELETE|HEAD|PATCH|OPTIONS)\b/i.test(text) && typeof value !== 'object') {
              for (const match of text.matchAll(/\b(GET|POST|PUT|DELETE|HEAD|PATCH|OPTIONS)\b/gi)) {
                measured += textWidth(match[0].toUpperCase(), 'method') - textWidth(match[0], 'text') + 4
              }
            } else if (typeof value !== 'object') {
              const tone = getOutcomeTone(text, column.name, severityField)
              if (tone === 'failure' || tone === 'pending') measured = textWidth(text, 'outcome')
            }
          }
        }
        valueWidths.set(value, measured)
      }
      width = Math.max(width, Math.ceil(measured + cellPadding))
      if (width >= cap) return cap
    }
    return width
  }
}
