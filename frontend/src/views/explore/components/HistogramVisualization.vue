<script setup lang="ts">
import { computed } from 'vue'
import { useExploreStore } from '@/stores/explore'
import LogHistogram from '@/components/visualizations/LogHistogram.vue'

interface TimeRangeEvent {
  start: Date;
  end: Date;
}

const emit = defineEmits<{
  (e: 'zoom-time-range', range: TimeRangeEvent): void
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
    />
  </div>
  
  <!-- Show message when histogram is not available -->
  <div v-else class="histogram-unavailable-message">
    <div class="flex items-center justify-center h-16 text-sm text-muted-foreground">
      <span>Histogram is not available for this query mode</span>
    </div>
  </div>
</template>
