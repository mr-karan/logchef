<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SelectGroup } from '@/components/ui/select'
import {
  NumberField,
  NumberFieldContent,
  NumberFieldDecrement,
  NumberFieldIncrement,
  NumberFieldInput,
} from '@/components/ui/number-field'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useToast } from '@/components/ui/toast'
import { sourcesApi } from '@/api/sources'
import { exploreApi } from '@/api/explore'
import type { Source } from '@/api/sources'
import type { Log } from '@/api/explore'
import type { QueryStats } from '@/api/explore'
import { isErrorResponse, getErrorMessage } from '@/api/types'
import { TOAST_DURATION } from '@/lib/constants'
import { DateTimePicker } from '@/components/date-time-picker'
import type { DateRange } from 'radix-vue'
import { getLocalTimeZone, now } from '@internationalized/date'
import { cn } from '@/lib/utils'
import { Search, RefreshCcw, X, CalendarIcon, Play, ChevronsUpDown, LogOut } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import Results from './Results.vue'
import EmptyState from './EmptyState.vue'

const { toast } = useToast()

const sources = ref<Source[]>([])
const selectedSource = ref<string>('')
const sourceDetails = ref<Source | null>(null)
const isLoading = ref(false)
const queryInput = ref('')
const queryLimit = ref(1000)
const logs = ref<Record<string, any>[]>([])
const queryStats = ref<QueryStats>({
  execution_time_ms: 0
})

const df = new Intl.DateTimeFormat('en-US', {
  dateStyle: 'medium',
})

const timeFormatter = new Intl.DateTimeFormat('en-US', {
  hour: 'numeric',
  minute: 'numeric',
  second: 'numeric',
  hour12: false,
})

const currentTime = now(getLocalTimeZone())
const dateRange = ref<DateRange>({
  start: currentTime.subtract({ hours: 3 }),
  end: currentTime,
})

// Load sources on mount
onMounted(async () => {
  try {
    const response = await sourcesApi.listSources()
    if (isErrorResponse(response)) {
      toast({
        title: 'Error',
        description: response.data.error,
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      })
      return
    }
    sources.value = response.data.sources
    // If we have sources, select the first one
    if (sources.value.length > 0) {
      selectedSource.value = sources.value[0].id
      // Load source details and execute query
      const sourceResponse = await sourcesApi.getSource(selectedSource.value)
      if (!isErrorResponse(sourceResponse)) {
        sourceDetails.value = sourceResponse.data.source
        await executeQuery()
      }
    }
  } catch (error) {
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  } finally {
    isLoading.value = false
  }
})

// Watch for changes in source or date range to reload logs
watch([selectedSource, dateRange], async ([newSourceId]) => {
  if (newSourceId && sources.value.length > 0) {
    try {
      // First fetch source details to get schema
      const sourceResponse = await sourcesApi.getSource(newSourceId)
      if (isErrorResponse(sourceResponse)) {
        toast({
          title: 'Error',
          description: sourceResponse.data.error,
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        })
        return
      }
      sourceDetails.value = sourceResponse.data.source
      // Then execute the query
      await executeQuery()
    } catch (error) {
      toast({
        title: 'Error',
        description: getErrorMessage(error),
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      })
    }
  }
})

async function executeQuery() {
  if (!selectedSource.value) return

  isLoading.value = true
  try {
    const response = await exploreApi.getLogs(selectedSource.value, {
      query: queryInput.value.trim(),
      limit: queryLimit.value,
      start_timestamp: dateRange.value.start.toDate(getLocalTimeZone()).getTime(),
      end_timestamp: dateRange.value.end.toDate(getLocalTimeZone()).getTime(),
    })
    if (response?.data) {
      logs.value = response.data.logs ?? []
      queryStats.value = response.data.stats
    }
  } catch (error) {
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  } finally {
    isLoading.value = false
  }
}

async function loadSourceDetails(sourceId: string) {
  try {
    const response = await sourcesApi.getSource(sourceId)
    if (isErrorResponse(response)) {
      toast({
        title: 'Error',
        description: response.data.error,
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      })
      return
    }
    sourceDetails.value = response.data.source
  } catch (error) {
    console.error('Error loading source details:', error)
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  }
}
</script>

<template>
  <div class="flex flex-col h-full min-w-0">
    <!-- Query Controls -->
    <div class="mb-6 space-y-4">
      <!-- Top Controls Row -->
      <div class="grid grid-cols-[200px,400px,1fr,220px] gap-4 h-10">
        <!-- Source Selector -->
        <Select v-model="selectedSource">
          <SelectTrigger>
            <SelectValue placeholder="Select a source">
              {{ sources.find(s => s.id === selectedSource)?.connection?.database || 'Select a source' }}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem v-for="source in sources" :key="source.id" :value="source.id">
                {{ source.connection?.database || source.id }}
              </SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>

        <!-- Date Range Picker -->
        <DateTimePicker v-model="dateRange" />

        <!-- Spacer -->
        <div></div>

        <!-- Right Group -->
        <div class="flex items-center gap-2 ml-auto">
          <NumberField v-model="queryLimit" :min="100" :max="10000" :step="100"
            class="w-[100px] [&_input[type='number']::-webkit-inner-spin-button]:appearance-none [&_input[type='number']::-webkit-outer-spin-button]:appearance-none">
            <NumberFieldContent>
              <div class="flex items-center">
                <NumberFieldInput placeholder="Limit" class="text-sm" :step="100" />
                <NumberFieldDecrement :step="100" />
                <NumberFieldIncrement :step="100" />
              </div>
            </NumberFieldContent>
          </NumberField>

          <Button @click="executeQuery" :disabled="isLoading" class="w-[100px] h-10">
            <Search class="mr-2 h-4 w-4" />
            Search
          </Button>
        </div>
      </div>

      <!-- Query Input Row -->
      <div class="w-full">
        <Input v-model="queryInput" placeholder="Type your query here..." class="w-full font-mono text-sm h-10" />
      </div>
    </div>

    <!-- Results Area -->
    <div class="flex-1 min-h-0 min-w-0">
      <!-- Show Skeleton when loading -->
      <div v-if="isLoading" class="space-y-4 p-4 animate-pulse">
        <!-- Simulate Table Header Skeleton -->
        <div class="flex space-x-2">
          <Skeleton class="h-4 w-1/4" />
          <Skeleton class="h-4 w-1/4" />
          <Skeleton class="h-4 w-1/4" />
          <Skeleton class="h-4 w-1/4" />
        </div>
        <!-- Simulate Table Rows Skeleton -->
        <div class="space-y-2">
          <div v-for="n in 5" :key="n" class="flex space-x-2">
            <Skeleton class="h-4 w-1/4" />
            <Skeleton class="h-4 w-1/4" />
            <Skeleton class="h-4 w-1/4" />
            <Skeleton class="h-4 w-1/4" />
          </div>
        </div>
      </div>
      <!-- Show Results when data is loaded -->
      <Results 
        v-else-if="logs.length > 0" 
        :logs="logs" 
        :columns="sourceDetails?.columns || []" 
        :stats="queryStats" 
      />
      <EmptyState v-else />
    </div>
  </div>
</template>
