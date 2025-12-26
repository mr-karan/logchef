<script setup lang="ts">
import { computed } from 'vue'
import { useExploreStore } from '@/stores/explore'
import LogHistogram from '@/components/visualizations/LogHistogram.vue'
import type { DateValue } from '@internationalized/date'

interface TimeRangeEvent {
  start: Date;
  end: Date;
}

interface DateValueRangeEvent {
  start: DateValue;
  end: DateValue;
}

const emit = defineEmits<{
  (e: 'zoom-time-range', range: TimeRangeEvent): void
  (e: 'update:timeRange', range: TimeRangeEvent): void
}>()

const exploreStore = useExploreStore()

// Reactive computed properties
const isExecutingQuery = computed(() => exploreStore.isLoadingOperation('executeQuery'))
const timeRange = computed(() => exploreStore.timeRange ?? undefined)
const groupByField = computed(() => exploreStore.groupByField === '__none__' ? undefined : exploreStore.groupByField)
const sourceId = computed(() => exploreStore.sourceId)
const isHistogramEligible = computed(() => exploreStore.isHistogramEligible)

// Event handlers for histogram interactions
const handleZoomTimeRange = (range: TimeRangeEvent) => {
  emit('zoom-time-range', range)
}

const handleTimeRangeUpdate = (range: DateValueRangeEvent) => {
  // Convert DateValue to Date for the parent component
  const start = new Date(range.start.year, range.start.month - 1, range.start.day)
  const end = new Date(range.end.year, range.end.month - 1, range.end.day)
  emit('update:timeRange', { start, end })
}
</script>

<template>
  <!-- Only show histogram if query is eligible -->
  <div v-if="isHistogramEligible" class="histogram-container">
    <LogHistogram
      :key="`histogram-${sourceId}`"
      :time-range="timeRange"
      :is-loading="isExecutingQuery"
      :group-by="groupByField"
      @zoom-time-range="handleZoomTimeRange"
      @update:timeRange="handleTimeRangeUpdate"
    />
  </div>
  
  <!-- Show message when histogram is not available -->
  <div v-else class="histogram-unavailable-message">
    <div class="flex items-center justify-center h-16 text-sm text-muted-foreground">
      <span>📊 Histogram is only available for LogchefQL queries</span>
    </div>
  </div>
</template>