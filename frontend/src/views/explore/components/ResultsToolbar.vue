<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed } from 'vue'
import { Button } from '@/components/ui/button'
import { ChevronUp, ChevronDown, Rows4, TerminalSquare, Download, Braces, Zap } from 'lucide-vue-next'
import { useExploreRuns } from '@/stores/exploreRuns'
import GroupBySelector from './GroupBySelector.vue'
import StreamPicker from './StreamPicker.vue'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

const { t } = useI18n();

interface FieldInfo {
  name: string
  type: string
  isTimestamp?: boolean
  isSeverity?: boolean
}

interface Props {
  isHistogramVisible: boolean
  availableFields: FieldInfo[]
  displayMode: 'table' | 'compact' | 'json'
  logsCount: number
  queryTimeMs?: number
  isLoading?: boolean
  isExporting?: boolean
  canExport?: boolean
  fastFilterFields?: string[]
  canAddFastFilter?: boolean
}

withDefaults(defineProps<Props>(), { fastFilterFields: () => [], canAddFastFilter: false })

const emit = defineEmits<{
  (e: 'toggle-histogram'): void
  (e: 'update:displayMode', mode: 'table' | 'compact' | 'json'): void
  (e: 'export'): void
  (e: 'add-fast-filter', field: string): void
}>()

const runs = useExploreRuns()

const queryStats = computed(() => runs.results?.stats)
const isHistogramEligible = computed(() => runs.isHistogramEligible)
const queryWarnings = computed(() => runs.results?.warnings ?? [])

const formattedQueryTime = computed(() => {
  const executionTimeMs = queryStats.value?.execution_time_ms
  if (!executionTimeMs) return null
  
  // execution_time_ms is in milliseconds
  if (executionTimeMs < 1000) {
    return `${Math.round(executionTimeMs)}ms`
  }
  return `${(executionTimeMs / 1000).toFixed(2)}s`
})

const warningText = computed(() => {
  if (queryStats.value?.truncated) {
    return queryStats.value.truncated_reason === 'byte_limit'
      ? t('ui.responseSizeCapped')
      : t('ui.resultCapped')
  }
  return queryWarnings.value?.[0]?.message || null
})
</script>

<template>
  <div class="flex items-center justify-between h-8 px-3 bg-muted/30 border-b text-xs">
    <!-- Left: Histogram toggle + Stats -->
    <div class="flex items-center gap-3">
      <StreamPicker />
      <!-- Histogram Toggle -->
      <button
        v-if="isHistogramEligible"
        class="flex items-center gap-1 text-muted-foreground hover:text-foreground transition-colors"
        @click="emit('toggle-histogram')"
        :title="isHistogramVisible ? t('ui.hideHistogram') : t('ui.showHistogram')"
      >
        <ChevronUp v-if="isHistogramVisible" class="h-3.5 w-3.5" />
        <ChevronDown v-else class="h-3.5 w-3.5" />
        <span class='font-medium'>{{ t('ui.histogram') }}</span>
      </button>

      <div v-if="isHistogramEligible" class="h-4 w-px bg-border" />

      <!-- Stats -->
      <div class="flex items-center gap-2 text-muted-foreground">
        <span v-if="isLoading" class='animate-pulse'>{{ t('ui.loading') }}</span>
        <template v-else>
          <span class="font-medium text-foreground">{{ logsCount.toLocaleString() }}</span>
          <span>{{ t('ui.logs') }}</span>
          <template v-if="formattedQueryTime">
            <span class="text-muted-foreground/50">•</span>
            <span :title="t('pages.queryTimeTooltip', { duration: formattedQueryTime })">{{ formattedQueryTime }}</span>
          </template>
          <template v-if="warningText">
            <span class="text-muted-foreground/50">•</span>
            <span class="text-amber-600 dark:text-amber-400">{{ warningText }}</span>
          </template>
          <template v-if="fastFilterFields.length">
            <span class="text-muted-foreground/50">•</span>
            <span class="inline-flex items-center gap-1 text-muted-foreground" :title="t('explore.fastFilterTooltip')">
              <Zap class="h-3 w-3 text-amber-500" aria-hidden="true" />
              <span>{{ t('explore.fastFilterLead') }}</span>
              <template v-for="(field, idx) in fastFilterFields" :key="field">
                <button
                  v-if="canAddFastFilter"
                  type="button"
                  class="rounded bg-muted px-1 font-mono text-foreground/80 hover:bg-primary/10 hover:text-primary"
                  :aria-label="t('explore.addFastFilter', { field })"
                  @click="emit('add-fast-filter', field)"
                >{{ field }}</button>
                <code v-else class="rounded bg-muted px-1 text-foreground/80">{{ field }}</code>
                <span v-if="idx < fastFilterFields.length - 1">,</span>
              </template>
            </span>
          </template>
        </template>
      </div>
    </div>

    <!-- Center: Group By -->
    <div v-if="isHistogramEligible" class="flex items-center">
      <GroupBySelector v-if="!runs.windowed" :available-fields="availableFields" />
    </div>
    <div v-else />

    <!-- Right: Export + View Toggles -->
    <div class="flex items-center gap-1">
      <!-- Download Button (only for sources advertising the exports capability) -->
      <TooltipProvider v-if="canExport">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              class="h-7 w-7 p-0"
              :aria-label="isExporting ? t('ui.preparingDownload') : t('ui.downloadResults')"
              @click="emit('export')"
              :disabled="logsCount === 0 || isLoading || isExporting"
            >
              <Download class="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            <p class='text-xs'>{{ isExporting ? t('ui.preparingDownload') : t('ui.downloadResults') }}</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>

      <div class="h-4 w-px bg-border mx-1" />

      <!-- View Mode Toggles -->
      <div class="flex items-center bg-muted rounded-md p-0.5">
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                class="h-6 w-6 rounded flex items-center justify-center transition-colors"
                :aria-label="t('ui.compactView')"
                :class="displayMode === 'compact' ? 'bg-background shadow-sm' : 'hover:bg-background/50'"
                @click="emit('update:displayMode', 'compact')"
              >
                <TerminalSquare class="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p class='text-xs'>{{ t('ui.compactView') }}</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                class="h-6 w-6 rounded flex items-center justify-center transition-colors"
                :aria-label="t('explore.jsonView')"
                :class="displayMode === 'json' ? 'bg-background shadow-sm' : 'hover:bg-background/50'"
                @click="emit('update:displayMode', 'json')"
              >
                <Braces class="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p class="text-xs">{{ t('explore.jsonView') }}</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                class="h-6 w-6 rounded flex items-center justify-center transition-colors"
                :aria-label="t('ui.tableView')"
                :class="displayMode === 'table' ? 'bg-background shadow-sm' : 'hover:bg-background/50'"
                @click="emit('update:displayMode', 'table')"
              >
                <Rows4 class="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p class='text-xs'>{{ t('ui.tableView') }}</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>
    </div>
  </div>
</template>
