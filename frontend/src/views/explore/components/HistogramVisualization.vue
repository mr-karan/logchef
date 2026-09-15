<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed } from 'vue'
import { useExploreStore } from '@/stores/explore'
import LogHistogram from '@/components/visualizations/LogHistogram.vue'

const { t } = useI18n();

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
      <span>{{ t('ui.histogramIsNotAvailableForThisQueryMode') }}</span>
    </div>
  </div>
</template>
