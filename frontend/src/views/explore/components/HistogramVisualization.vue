<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed } from 'vue'
import { useExploreStore } from '@/stores/explore'
import LogHistogram from '@/components/visualizations/LogHistogram.vue'
import { useExploreWindowedStore } from '@/stores/exploreWindowed'
import { cn } from '@/lib/utils'

const { t } = useI18n();

interface TimeRangeEvent {
  start: Date;
  end: Date;
}

const emit = defineEmits<{
  (e: 'zoom-time-range', range: TimeRangeEvent): void
}>()

const exploreStore = useExploreStore()
const windowed = useExploreWindowedStore()

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
      :is-loading="isExecutingQuery && !windowed.active"
      :group-by="groupByField"
      @zoom-time-range="handleZoomTimeRange"
    />
    <div v-if="windowed.active" class="flex flex-col gap-1 pt-1">
      <div class="flex h-2 gap-px" :aria-label="t('sources.windowedCountCoverage')">
        <button v-for="window in [...windowed.coverage].reverse()" :key="window.index" type="button"
          :class="cn('rounded-sm', window.count === 'complete' ? 'bg-primary' : window.count === 'failed' ? 'bg-destructive' : 'bg-muted')"
          :style="{ flexGrow: Date.parse(window.end) - Date.parse(window.start) }"
          :title="`${window.start} to ${window.end}: ${window.count}`"
          :aria-label="`${window.start} to ${window.end}: ${window.count}`"
          :disabled="window.count !== 'failed' || windowed.isCounting" @click="windowed.retryCount(window.index)" />
      </div>
      <p class="text-xs text-muted-foreground">{{ t('sources.windowedCountCoverageLegend') }}</p>
    </div>
  </div>
  
  <!-- Show message when histogram is not available -->
  <div v-else class="histogram-unavailable-message">
    <div class="flex items-center justify-center h-16 text-sm text-muted-foreground">
      <span>{{ t('ui.histogramIsNotAvailableForThisQueryMode') }}</span>
    </div>
  </div>
</template>
