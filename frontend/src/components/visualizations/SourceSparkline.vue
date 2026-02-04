<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { VisXYContainer, VisLine, VisArea, VisTooltip } from '@unovis/vue'
import { CurveType } from '@unovis/ts'

interface SparklinePoint {
  bucket: string
  rows: number
}

interface Props {
  data: SparklinePoint[]
  height?: number
  showArea?: boolean
  color?: string
}

const props = withDefaults(defineProps<Props>(), {
  height: 40,
  showArea: true,
  color: '#3b82f6', // Default blue
})

// Generate a unique ID for the gradient to avoid conflicts
const gradientId = ref(`sparkline-gradient-${Math.random().toString(36).slice(2, 9)}`)

// Normalize a date to start of hour and return a consistent key string
const toHourKey = (date: Date): string => {
  const d = new Date(date)
  d.setMinutes(0, 0, 0)
  return d.toISOString().slice(0, 13)
}

// Fill in missing hourly buckets with 0 values to create a continuous time series
const normalizedData = computed(() => {
  if (props.data.length === 0) return []

  const dataMap = new Map<string, number>()
  props.data.forEach((point) => {
    const date = new Date(point.bucket)
    dataMap.set(toHourKey(date), point.rows)
  })

  const now = new Date()
  now.setMinutes(0, 0, 0)
  const buckets: { bucket: Date; rows: number }[] = []

  for (let i = 23; i >= 0; i--) {
    const bucketTime = new Date(now.getTime() - i * 60 * 60 * 1000)
    const key = toHourKey(bucketTime)
    buckets.push({
      bucket: bucketTime,
      rows: dataMap.get(key) || 0,
    })
  }

  return buckets
})

const showTime = computed(() =>
  normalizedData.value.some((point) =>
    point.bucket.getHours() !== 0 || point.bucket.getMinutes() !== 0
  )
)

const x = (d: { bucket: Date }) => d.bucket
const y = (d: { rows: number }) => d.rows

const tooltipContainer = typeof document !== 'undefined' ? document.body : undefined

const formatBucket = (date: Date) => {
  if (showTime.value) {
    return new Intl.DateTimeFormat(undefined, {
      month: 'short',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    }).format(date)
  }
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: '2-digit',
    year: 'numeric',
  }).format(date)
}

const renderTooltip = (datum: { bucket: Date; rows: number }) => {
  return `
    <div class="sparkline-tooltip">
      <span class="sparkline-tooltip__label">${formatBucket(datum.bucket)}</span>
      <span class="sparkline-tooltip__value">${datum.rows.toLocaleString()} rows</span>
    </div>
  `
}

const tooltipTriggers = computed(() => ({
  '.sparkline-line': renderTooltip,
  '.sparkline-area': renderTooltip,
}))

// Inject gradient into SVG after mount
const containerRef = ref<InstanceType<typeof VisXYContainer> | null>(null)

onMounted(() => {
  // Find the SVG and inject our gradient definition
  const container = containerRef.value?.$el
  if (container) {
    const svg = container.querySelector('svg')
    if (svg) {
      // Create defs element with gradient
      const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs')
      defs.innerHTML = `
        <linearGradient id="${gradientId.value}" x1="0%" y1="0%" x2="0%" y2="100%">
          <stop offset="0%" stop-color="${props.color}" stop-opacity="0.3" />
          <stop offset="100%" stop-color="${props.color}" stop-opacity="0.02" />
        </linearGradient>
      `
      svg.insertBefore(defs, svg.firstChild)
    }
  }
})

const areaColor = computed(() => `url(#${gradientId.value})`)
</script>

<template>
  <VisXYContainer
    ref="containerRef"
    :data="normalizedData"
    :height="height"
    :margin="{ top: 2, right: 0, bottom: 2, left: 0 }"
    :padding="{ top: 4, right: 0, bottom: 0, left: 0 }"
    class="sparkline-container"
  >
    <VisTooltip
      :triggers="tooltipTriggers"
      :container="tooltipContainer"
    />
    <VisArea
      v-if="showArea"
      :x="x"
      :y="y"
      :curve-type="CurveType.MonotoneX"
      :color="areaColor"
      :opacity="1"
    />
    <VisLine
      :x="x"
      :y="y"
      :curve-type="CurveType.MonotoneX"
      :line-width="1.5"
      :color="color"
    />
  </VisXYContainer>
</template>

<style>
/* Tooltip styles - not scoped so they work in portal */
.sparkline-tooltip {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 6px 10px;
  border-radius: 6px;
  background: hsl(var(--popover));
  color: hsl(var(--popover-foreground));
  font-size: 11px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  border: 1px solid hsl(var(--border));
}

.sparkline-tooltip__label {
  color: hsl(var(--muted-foreground));
  font-size: 10px;
}

.sparkline-tooltip__value {
  font-weight: 600;
  font-size: 12px;
}
</style>

<style scoped>
.sparkline-container {
  opacity: 0.9;
  transition: opacity 0.2s ease;
}

.sparkline-container:hover {
  opacity: 1;
}
</style>
