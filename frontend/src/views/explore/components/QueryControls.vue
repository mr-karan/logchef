<script setup lang="ts">
import { computed } from 'vue'
import { Button } from '@/components/ui/button'
import { Play, RefreshCw, Share2, Keyboard, Eraser, AlertCircle, Clock, X } from 'lucide-vue-next'
import { useToast } from '@/composables/useToast'
import { TOAST_DURATION } from '@/lib/constants'
import { useExploreStore } from '@/stores/explore'
import { useQuery } from '@/composables/useQuery'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

interface Props {
  showExecuteControls?: boolean
}

withDefaults(defineProps<Props>(), {
  showExecuteControls: true
})

const emit = defineEmits<{
  (e: 'execute', key: string): void
  (e: 'clear'): void
}>()
const { toast } = useToast()
const exploreStore = useExploreStore()

const {
  isDirty,
  isExecutingQuery,
  canExecuteQuery,
  dirtyReason
} = useQuery()

// Add cancel query capabilities
const canCancelQuery = computed(() => exploreStore.canCancelQuery)
const isCancellingQuery = computed(() => exploreStore.isCancellingQuery)

// Query timeout options in seconds
const timeoutOptions = [
  { label: '10s', value: 10 },
  { label: '30s', value: 30 },
  { label: '1m', value: 60 },
  { label: '2m', value: 120 },
  { label: '5m', value: 300 },
  { label: '10m', value: 600 },
  { label: '15m', value: 900 },
  { label: '30m', value: 1800 }
]

// Get current timeout or default to 30 seconds - handle as string for Select component
const selectedTimeout = computed({
  get: () => (exploreStore.queryTimeout || 30).toString(),
  set: (value: string) => {
    exploreStore.setQueryTimeout(parseInt(value, 10))
  }
})

const hasExecutedQuery = computed(() => Boolean(exploreStore.lastExecutedState))

const forceDirty = computed(() => {
  return hasExecutedQuery.value && isDirty.value
})

const dirtyTooltipContent = computed(() => {
  if (!forceDirty.value) return 'Execute query'

  const reasons: string[] = []

  if (dirtyReason.value?.timeRangeChanged) {
    reasons.push('Time range has changed')
  }

  if (dirtyReason.value?.limitChanged) {
    reasons.push('Result limit has changed')
  }

  if (dirtyReason.value?.queryChanged) {
    reasons.push('Query content has changed')
  }

  if (dirtyReason.value?.modeChanged) {
    reasons.push('Query mode has changed')
  }

  return reasons.length > 0
    ? `Results may be outdated: ${reasons.join(', ')}`
    : 'Query parameters have changed, results may be outdated'
})

// Function to copy current URL to clipboard
const copyUrlToClipboard = () => {
  try {
    navigator.clipboard.writeText(window.location.href)
  } catch (error) {
    console.error("Failed to copy URL: ", error)
    toast({
      title: "Copy Failed",
      description: "Failed to copy URL to clipboard.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR
    })
  }
}

// Execute query with a dedicated key to prevent duplicates
const executeQuery = () => {
  emit('execute', 'manual-execution')
}

// Clear editor content
const clearEditor = () => {
  emit('clear')
}

// Cancel query
const cancelQuery = () => {
  exploreStore.cancelQuery()
}
</script>

<template>
  <div class="flex items-center justify-between w-full">
    <!-- Left side controls with better grouping -->
    <div class="flex items-center gap-3">
      <!-- Primary action buttons group -->
      <div class="flex items-center gap-2">
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button v-if="showExecuteControls" variant="default" class="h-9 px-4 flex items-center gap-2 shadow-sm" 
                :class="{
                  'bg-orange-600 hover:bg-orange-700 dark:bg-orange-600 dark:hover:bg-orange-700 text-white font-semibold border-2 border-orange-700': forceDirty && !isExecutingQuery,
                  'bg-primary hover:bg-primary/90 text-primary-foreground': !forceDirty && !isExecutingQuery,
                  'bg-primary/80 hover:bg-primary/90 text-primary-foreground': isExecutingQuery
                }" 
                :disabled="isExecutingQuery || !canExecuteQuery"
                @click="executeQuery">
                <Play v-if="!isExecutingQuery" class="h-4 w-4" />
                <RefreshCw v-else class="h-4 w-4 animate-spin" />
                <span :class="{ 'font-bold': forceDirty }">Run Query</span>
                <AlertCircle class="h-3.5 w-3.5 ml-1" 
                  :class="{ 'opacity-0': !forceDirty, 'text-white': forceDirty }" />
                <div class="flex flex-col items-start ml-1 border-l border-current/20 pl-2 text-xs text-current">
                  <div class="flex items-center gap-1">
                    <Keyboard class="h-3 w-3" />
                    <span>Ctrl+Enter</span>
                  </div>
                </div>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom" class="max-w-xs">
              <p>{{ dirtyTooltipContent }}</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- Cancel Button -->
        <TooltipProvider v-if="isExecutingQuery && canCancelQuery">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="destructive" size="sm" class="h-9 px-3 flex items-center gap-1.5"
                @click="cancelQuery" :disabled="isCancellingQuery" aria-label="Cancel running query">
                <X class="h-3.5 w-3.5" />
                <span>{{ isCancellingQuery ? 'Cancelling...' : 'Cancel' }}</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p>Cancel running query</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- Clear Button -->
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="sm" class="h-9 px-3 flex items-center gap-1.5"
                @click="clearEditor" :disabled="isExecutingQuery" aria-label="Clear query editor">
                <Eraser class="h-3.5 w-3.5" />
                <span>Clear</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p>Clear Query</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>

      <!-- Separator -->
      <div class="h-6 w-px bg-border"></div>

      <!-- Settings group -->
      <div class="flex items-center gap-2">
        <!-- Query Timeout Selector -->
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <div class="flex items-center">
                <Select v-model="selectedTimeout" :disabled="isExecutingQuery">
                  <SelectTrigger class="h-9 w-[80px] text-xs">
                    <div class="flex items-center gap-1.5">
                      <Clock class="h-3.5 w-3.5" />
                      <SelectValue />
                    </div>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="option in timeoutOptions" :key="option.value" :value="option.value.toString()">
                      {{ option.label }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p>Query timeout duration</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
        
        <slot name="extraControls"></slot>
      </div>

      <!-- Middle slot for additional controls -->
      <slot name="rightControls"></slot>
    </div>

    <!-- Share Button - positioned at extreme right -->
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="outline" size="sm" class="h-8" @click="copyUrlToClipboard">
            <Share2 class="h-4 w-4 mr-1.5" />
            Share
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          <p>Copy shareable link</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  </div>
</template>
