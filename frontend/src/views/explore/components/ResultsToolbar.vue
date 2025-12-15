<script setup lang="ts">
import { computed } from 'vue'
import { Button } from '@/components/ui/button'
import { ChevronUp, ChevronDown, Rows4, TerminalSquare, Download } from 'lucide-vue-next'
import { useExploreStore } from '@/stores/explore'
import GroupBySelector from './GroupBySelector.vue'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

interface FieldInfo {
  name: string
  type: string
  isTimestamp?: boolean
  isSeverity?: boolean
}

interface Props {
  isHistogramVisible: boolean
  availableFields: FieldInfo[]
  displayMode: 'table' | 'compact'
  logsCount: number
  queryTimeMs?: number
  isLoading?: boolean
}

defineProps<Props>()

const emit = defineEmits<{
  (e: 'toggle-histogram'): void
  (e: 'update:displayMode', mode: 'table' | 'compact'): void
  (e: 'export'): void
}>()

const exploreStore = useExploreStore()

const queryStats = computed(() => exploreStore.queryStats)

const formattedQueryTime = computed(() => {
  const elapsed = queryStats.value?.elapsed
  if (!elapsed) return null
  
  // elapsed is in seconds
  if (elapsed < 1) {
    return `${Math.round(elapsed * 1000)}ms`
  }
  return `${elapsed.toFixed(2)}s`
})
</script>

<template>
  <div class="flex items-center justify-between h-9 px-3 bg-muted/30 border-b text-xs">
    <!-- Left: Histogram toggle + Stats -->
    <div class="flex items-center gap-3">
      <!-- Histogram Toggle -->
      <button 
        class="flex items-center gap-1 text-muted-foreground hover:text-foreground transition-colors"
        @click="emit('toggle-histogram')"
        :title="isHistogramVisible ? 'Hide histogram' : 'Show histogram'"
      >
        <ChevronUp v-if="isHistogramVisible" class="h-3.5 w-3.5" />
        <ChevronDown v-else class="h-3.5 w-3.5" />
        <span class="font-medium">Histogram</span>
      </button>

      <div class="h-4 w-px bg-border" />

      <!-- Stats -->
      <div class="flex items-center gap-2 text-muted-foreground">
        <span v-if="isLoading" class="animate-pulse">Loading...</span>
        <template v-else>
          <span class="font-medium text-foreground">{{ logsCount.toLocaleString() }}</span>
          <span>logs</span>
          <template v-if="formattedQueryTime">
            <span class="text-muted-foreground/50">•</span>
            <span>{{ formattedQueryTime }}</span>
          </template>
        </template>
      </div>
    </div>

    <!-- Center: Group By -->
    <div class="flex items-center">
      <GroupBySelector :available-fields="availableFields" />
    </div>

    <!-- Right: View Toggles + Export -->
    <div class="flex items-center gap-1">
      <!-- Export Button -->
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button 
              variant="ghost" 
              size="sm" 
              class="h-7 w-7 p-0"
              @click="emit('export')"
              :disabled="logsCount === 0"
            >
              <Download class="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            <p class="text-xs">Export results</p>
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
                :class="displayMode === 'compact' ? 'bg-background shadow-sm' : 'hover:bg-background/50'"
                @click="emit('update:displayMode', 'compact')"
              >
                <TerminalSquare class="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p class="text-xs">Compact view</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                class="h-6 w-6 rounded flex items-center justify-center transition-colors"
                :class="displayMode === 'table' ? 'bg-background shadow-sm' : 'hover:bg-background/50'"
                @click="emit('update:displayMode', 'table')"
              >
                <Rows4 class="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p class="text-xs">Table view</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>
    </div>
  </div>
</template>

