<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Play, RefreshCw, Share2, Keyboard, Eraser, AlertCircle, Clock, X } from 'lucide-vue-next'
import { useToast } from '@/components/ui/toast'
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

const props = withDefaults(defineProps<Props>(), {
  showExecuteControls: true
})

const emit = defineEmits<{
  (e: 'execute', key: string): void
  (e: 'clear'): void
}>()

const router = useRouter()
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
  get: () => {
    const timeout = exploreStore.queryTimeout || 30;
    console.log('QueryControls: Getting timeout value:', timeout, 'returning as string:', timeout.toString());
    return timeout.toString();
  },
  set: (value: string) => {
    const numValue = parseInt(value, 10);
    console.log('QueryControls: Setting timeout from string:', value, 'to number:', numValue);
    exploreStore.setQueryTimeout(numValue);
  }
})

// Enhanced tooltip content based on why the query is dirty
// Track when last query was executed and when parameters were last changed
const lastQueryTime = ref(exploreStore.lastExecutionTimestamp || 0);
const lastParamChangeTime = ref(0);

// Update lastQueryTime when execution timestamp changes
watch(() => exploreStore.lastExecutionTimestamp, (newVal, oldVal) => {
  if (newVal && newVal !== oldVal) {
    console.log("QueryControls: lastExecutionTimestamp changed, updating lastQueryTime from:", 
                lastQueryTime.value, "to:", newVal);
                
    // Update the timestamp of when the query was last executed
    // This should match the time when the query results were returned
    lastQueryTime.value = newVal;
  }
});

// Watch for time range changes to update lastParamChangeTime
watch(() => exploreStore.timeRange, () => {
  lastParamChangeTime.value = Date.now();
  console.log("QueryControls: Time range changed, updating lastParamChangeTime to:", lastParamChangeTime.value);
}, { deep: true });

// Watch for limit changes to update lastParamChangeTime
watch(() => exploreStore.limit, () => {
  lastParamChangeTime.value = Date.now();
  console.log("QueryControls: Limit changed, updating lastParamChangeTime to:", lastParamChangeTime.value);
});

// Calculate dirty state based on parameter changes
const forceDirty = computed(() => {
  // Only calculate dirty state if we already have executed a query
  if (!exploreStore.lastExecutedState || !exploreStore.lastExecutionTimestamp) {
    return false;
  }
  
  // Check current time range and last executed time range
  const currentTimeRange = JSON.stringify(exploreStore.timeRange || {});
  const lastTimeRange = exploreStore.lastExecutedState.timeRange || '';
  
  // Check if time range changed
  const timeRangeChanged = currentTimeRange !== lastTimeRange;
  
  // Check if limit changed  
  const limitChanged = exploreStore.limit !== exploreStore.lastExecutedState.limit;
  
  // Check if parameters were changed *after* the most recent query execution
  const paramChangesAfterExecution = lastParamChangeTime.value > lastQueryTime.value;
  
  // Log the calculation
  console.log("QueryControls: Manual dirty calculation - ",
              "timeRangeChanged:", timeRangeChanged,
              "limitChanged:", limitChanged,
              "lastQueryTime:", lastQueryTime.value,
              "lastParamChangeTime:", lastParamChangeTime.value,
              "paramChangesAfterExecution:", paramChangesAfterExecution);
  
  // Return dirty state: true if parameters changed AND those changes happened after the last execution
  return (timeRangeChanged || limitChanged || isDirty.value) && paramChangesAfterExecution;
});

const dirtyTooltipContent = computed(() => {
  // Use forceDirty instead of isDirty for the check
  if (!forceDirty.value) return 'Execute query'
  
  const reasons = []
  
  // Check time range manually
  const timeRangeJSON = JSON.stringify(exploreStore.timeRange);
  const lastTimeRangeJSON = exploreStore.lastExecutedState?.timeRange;
  if (lastTimeRangeJSON && (timeRangeJSON !== lastTimeRangeJSON)) {
    reasons.push('Time range has changed')
  }
  
  // Check limit manually
  if (exploreStore.lastExecutedState && 
      (exploreStore.limit !== exploreStore.lastExecutedState.limit)) {
    reasons.push('Result limit has changed')
  }
  
  // Check other reasons from dirtyReason
  if (dirtyReason.value?.queryChanged) reasons.push('Query content has changed')
  if (dirtyReason.value?.modeChanged) reasons.push('Query mode has changed')
  
  return reasons.length > 0 
    ? `Results may be outdated: ${reasons.join(', ')}`
    : 'Query parameters have changed, results may be outdated'
})

// Function to copy current URL to clipboard
const copyUrlToClipboard = () => {
  try {
    navigator.clipboard.writeText(window.location.href)
    toast({
      title: "URL Copied",
      description: "The shareable link has been copied to your clipboard.",
      duration: TOAST_DURATION.INFO,
      variant: "success"
    })
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
  // Update the lastQueryTime immediately to mark this as "executed"
  const currentTime = Date.now();
  lastQueryTime.value = currentTime;
  
  // Reset the parameter change time to be before the query execution time
  // This ensures we don't show the dirty state immediately after execution
  lastParamChangeTime.value = 0;

  // Add debugging info for both dirty states
  console.log("QueryControls: Executing query - isDirty:", isDirty.value, 
              "forceDirty:", forceDirty.value,
              "dirtyReason:", dirtyReason.value, 
              "timeRange:", JSON.stringify(exploreStore.timeRange).substring(0, 50) + "...",
              "lastExecutedState:", exploreStore.lastExecutedState ? 
                "exists with timeRange: " + exploreStore.lastExecutedState.timeRange.substring(0, 50) + "..." : "null",
              "setting lastQueryTime to:", lastQueryTime.value,
              "resetting lastParamChangeTime");
  
  emit('execute', 'manual-execution')
}

// Clear editor content
const clearEditor = () => {
  emit('clear')
}

// Cancel query
const cancelQuery = () => {
  exploreStore.cancelQuery()
  toast({
    title: "Query Cancelled",
    description: "The running query has been cancelled.",
    duration: TOAST_DURATION.INFO,
    variant: "default"
  })
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