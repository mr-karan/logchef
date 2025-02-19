<script setup lang="ts">
import { ref, onMounted, watch, computed, type WritableComputedRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SelectGroup } from '@/components/ui/select'
import {
  NumberField,
  NumberFieldContent,
  NumberFieldDecrement,
  NumberFieldIncrement,
  NumberFieldInput,
} from '@/components/ui/number-field'
import { useToast } from '@/components/ui/toast'
import { sourcesApi } from '@/api/sources'
import { exploreApi } from '@/api/explore'
import type { Source } from '@/api/sources'
import type { QueryStats } from '@/api/explore'
import { isErrorResponse, getErrorMessage } from '@/api/types'
import { TOAST_DURATION } from '@/lib/constants'
import { DateTimePicker } from '@/components/date-time-picker'
import { getLocalTimeZone, now, CalendarDateTime, type DateValue } from '@internationalized/date'
import type { DateRange } from 'radix-vue'
import { Search, Plus } from 'lucide-vue-next'
import { createColumns } from './table/columns'
import DataTable from './table/data-table.vue'
import EmptyState from './EmptyState.vue'
import type { ColumnDef } from '@tanstack/vue-table'
import type { ColumnInfo } from '@/api/explore'
import { Switch } from '@/components/ui/switch'
import DataTableFilters from './table/data-table-filters.vue'
import type { FilterCondition } from '@/api/explore'
import SQLEditor from '@/components/sql-editor/SQLEditor.vue'
import { formatSourceName } from '@/utils/format'
import { useSourcesStore } from '@/stores/sources'

const router = useRouter()
const sourcesStore = useSourcesStore()
const { toast } = useToast()
const route = useRoute()

const selectedSource = ref<string>('')
const isLoading = ref(false)
const queryInput = ref('')
const queryLimit = ref(100)
const logs = ref<Record<string, any>[]>([])
const columns = ref<ColumnInfo[]>([])
const queryStats = ref<QueryStats>({
  execution_time_ms: 0,
  rows_read: 0,
  bytes_read: 0
})

// Flag to prevent unnecessary URL updates on initial load
const isInitialLoad = ref(true)

// Date state
const currentTime = now(getLocalTimeZone())
const internalDateRange = ref<{ start: DateValue; end: DateValue }>({
  start: currentTime.subtract({ hours: 3 }),
  end: currentTime
})

// Computed DateRange for v-model binding
const dateRange = computed({
  get: () => ({
    start: internalDateRange.value.start,
    end: internalDateRange.value.end
  }),
  set: (newValue: DateRange | null) => {
    if (newValue?.start && newValue?.end) {
      internalDateRange.value = {
        start: newValue.start as DateValue,
        end: newValue.end as DateValue
      }
    }
  }
}) as unknown as WritableComputedRef<DateRange>

// Add loading state handling
const showLoadingState = computed(() => {
  return sourcesStore.isLoading
})

// Fix the empty state computed to handle null sources from API
const showEmptyState = computed(() => {
  return !sourcesStore.isLoading && (!sourcesStore.sources || sourcesStore.sources.length === 0)
})

// Fix the selected source name computed to handle null sources
const selectedSourceName = computed(() => {
  if (!sourcesStore.sources) return 'Loading...'
  const source = sourcesStore.sources.find(s => s.id === selectedSource.value)
  return source ? formatSourceName(source) : 'Select a source'
})

// Fix the selected source details computed to handle null sources
const selectedSourceDetails = computed(() => {
  if (!sourcesStore.sources) return { database: '', table: '' }
  const source = sourcesStore.sources.find(s => s.id === selectedSource.value)
  return source ? {
    database: source.connection.database,
    table: source.connection.table_name
  } : {
    database: '',
    table: ''
  }
})

// Function to get timestamps from dateRange
const getTimestamps = () => {
  if (!internalDateRange.value?.start || !internalDateRange.value?.end) {
    return {
      start: 0,
      end: 0
    }
  }

  const startDate = new Date(
    (internalDateRange.value.start as DateValue).year,
    (internalDateRange.value.start as DateValue).month - 1,
    (internalDateRange.value.start as DateValue).day,
    'hour' in internalDateRange.value.start ? internalDateRange.value.start.hour : 0,
    'minute' in internalDateRange.value.start ? internalDateRange.value.start.minute : 0,
    'second' in internalDateRange.value.start ? internalDateRange.value.start.second : 0
  )

  const endDate = new Date(
    (internalDateRange.value.end as DateValue).year,
    (internalDateRange.value.end as DateValue).month - 1,
    (internalDateRange.value.end as DateValue).day,
    'hour' in internalDateRange.value.end ? internalDateRange.value.end.hour : 0,
    'minute' in internalDateRange.value.end ? internalDateRange.value.end.minute : 0,
    'second' in internalDateRange.value.end ? internalDateRange.value.end.second : 0
  )

  return {
    start: startDate.getTime(),
    end: endDate.getTime()
  }
}

// Initialize state from URL parameters
function initializeFromURL() {
  const { source, limit, start_time, end_time, query } = route.query

  // Parse source ID
  if (source && typeof source === 'string') {
    selectedSource.value = source
  }

  // Parse limit
  if (limit) {
    const parsedLimit = parseInt(limit as string)
    if (!isNaN(parsedLimit) && parsedLimit > 0 && parsedLimit <= 10000) {
      queryLimit.value = parsedLimit
    }
  }

  // Parse timestamps
  if (start_time && end_time) {
    try {
      const startTs = parseInt(start_time as string)
      const endTs = parseInt(end_time as string)
      if (!isNaN(startTs) && !isNaN(endTs)) {
        // Create JavaScript Date objects first
        const startDate = new Date(startTs)
        const endDate = new Date(endTs)

        // Only update if dates are valid
        if (startDate.getTime() > 0 && endDate.getTime() > 0) {
          const start = new CalendarDateTime(
            startDate.getFullYear(),
            startDate.getMonth() + 1,
            startDate.getDate(),
            startDate.getHours(),
            startDate.getMinutes(),
            startDate.getSeconds()
          )
          const end = new CalendarDateTime(
            endDate.getFullYear(),
            endDate.getMonth() + 1,
            endDate.getDate(),
            endDate.getHours(),
            endDate.getMinutes(),
            endDate.getSeconds()
          )
          internalDateRange.value = {
            start: start as DateValue,
            end: end as DateValue
          }
        }
      }
    } catch (e) {
      console.error('Error parsing timestamps:', e)
      // On error, set to last 3 hours
      const newCurrentTime = now(getLocalTimeZone())
      internalDateRange.value = {
        start: newCurrentTime.subtract({ hours: 3 }),
        end: newCurrentTime
      }
    }
  }

  // Parse query
  if (query && typeof query === 'string') {
    queryInput.value = decodeURIComponent(query)
  }
}

// Update URL with current state
function updateURL() {
  if (isInitialLoad.value) {
    isInitialLoad.value = false
    return
  }

  const timestamps = getTimestamps()
  const query: Record<string, string> = {
    source: selectedSource.value,
    limit: queryLimit.value.toString(),
    start_time: timestamps.start.toString(),
    end_time: timestamps.end.toString()
  }

  if (queryInput.value.trim()) {
    query.query = encodeURIComponent(queryInput.value.trim())
  }

  router.replace({ query })
}

// Update onMounted
onMounted(async () => {
  try {
    // First initialize from URL
    initializeFromURL()

    // Then load sources
    await sourcesStore.loadSources()

    // If source wasn't set from URL and we have sources, select the first one
    if (!selectedSource.value && sourcesStore.sources.length > 0) {
      selectedSource.value = sourcesStore.sources[0].id
    }

    // Validate selected source exists
    if (selectedSource.value && !sourcesStore.sources.find(s => s.id === selectedSource.value)) {
      toast({
        title: 'Warning',
        description: 'Selected source not found',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      })
      selectedSource.value = sourcesStore.sources[0]?.id || ''
    }

    // Execute initial query if we have a valid source
    if (selectedSource.value) {
      await executeQuery()
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

// Query state
const queryMode = ref<'filters' | 'raw_sql'>('filters')
const sqlQuery = ref('')
const filterConditions = ref<FilterCondition[]>([])

async function executeQuery() {
  if (!selectedSource.value) return

  isLoading.value = true
  logs.value = []
  queryStats.value = {
    execution_time_ms: 0,
    rows_read: 0,
    bytes_read: 0
  }

  try {
    const timestamps = getTimestamps()
    const baseParams = {
      limit: queryLimit.value,
      start_timestamp: timestamps.start,
      end_timestamp: timestamps.end,
    }

    // Type-safe params based on mode
    const params = queryMode.value === 'filters'
      ? {
        ...baseParams,
        mode: 'filters' as const,
        conditions: filterConditions.value
      }
      : {
        ...baseParams,
        mode: 'raw_sql' as const,
        raw_sql: sqlQuery.value
      }

    const response = await exploreApi.getLogs(selectedSource.value, params)

    if ('error' in response.data) {
      toast({
        title: 'Error',
        description: response.data.error,
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      })
      return
    }

    logs.value = response.data.logs
    queryStats.value = response.data.stats
    columns.value = response.data.columns

    // Update URL only after successful search
    if (!isInitialLoad.value) {
      updateURL()
    }
    isInitialLoad.value = false
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

const tableColumns = ref<ColumnDef<Record<string, any>>[]>([])

watch(
  () => columns.value,
  (newColumns) => {
    if (newColumns) {
      tableColumns.value = createColumns(newColumns)
    }
  },
  { immediate: true }
)

// Handle filter updates
const handleFiltersUpdate = (filters: FilterCondition[]) => {
  filterConditions.value = filters
}

// Toggle query mode
const toggleQueryMode = (checked: boolean) => {
  queryMode.value = checked ? 'raw_sql' : 'filters'
  // Clear the other mode's input when switching
  if (checked) {
    filterConditions.value = []
  } else {
    sqlQuery.value = ''
  }
}
</script>

<template>
  <!-- Show loading state -->
  <div v-if="showLoadingState" class="flex flex-col items-center justify-center h-[calc(100vh-12rem)] gap-4">
    <div class="space-y-4 p-4 animate-pulse">
      <div class="flex space-x-2">
        <Skeleton class="h-4 w-32" />
      </div>
      <div class="space-y-2">
        <Skeleton class="h-4 w-48" />
        <Skeleton class="h-4 w-40" />
      </div>
    </div>
  </div>

  <!-- Show empty state when no sources are available -->
  <div v-else-if="showEmptyState" class="flex flex-col items-center justify-center h-[calc(100vh-12rem)] gap-4">
    <div class="text-center space-y-2">
      <h2 class="text-2xl font-semibold tracking-tight">No Sources Found</h2>
      <p class="text-muted-foreground">You need to configure a log source before you can explore logs.</p>
    </div>
    <Button @click="router.push({ name: 'NewSource' })">
      <Plus class="mr-2 h-4 w-4" />
      Add Your First Source
    </Button>
  </div>

  <!-- Main explorer UI when sources are available -->
  <div v-else class="flex flex-col h-full min-w-0">
    <!-- Query Controls -->
    <div class="mb-6 space-y-6">
      <!-- Top Controls Row -->
      <div class="grid grid-cols-[200px,400px,1fr,220px] gap-6">
        <!-- Source Selector -->
        <div class="flex flex-col gap-1.5">
          <Label class="text-sm font-medium">Source</Label>
          <Select v-model="selectedSource">
            <SelectTrigger>
              <SelectValue placeholder="Select a source">
                {{ selectedSourceName }}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem v-for="source in sourcesStore.sources" :key="source.id" :value="source.id">
                  {{ formatSourceName(source) }}
                </SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>

        <!-- Date Range Picker -->
        <div class="flex flex-col gap-1.5">
          <Label class="text-sm font-medium">Time Range</Label>
          <DateTimePicker v-model="dateRange" />
        </div>

        <!-- Query Mode Switch -->
        <div class="flex flex-col gap-1.5">
          <Label class="text-sm font-medium">Query Mode</Label>
          <div class="flex items-center gap-2 h-10">
            <div class="flex items-center gap-2">
              <Switch :checked="queryMode === 'raw_sql'" @update:checked="toggleQueryMode" />
              <Label class="text-sm">{{ queryMode === 'raw_sql' ? 'SQL Query' : 'Filter Builder' }}</Label>
            </div>
          </div>
        </div>

        <!-- Right Group -->
        <div class="flex flex-col gap-1.5">
          <Label class="text-sm font-medium">Results Limit</Label>
          <div class="flex items-center gap-2">
            <NumberField v-model="queryLimit" :min="10" :max="10000" :step="10"
              class="w-[100px] [&_input[type='number']::-webkit-inner-spin-button]:appearance-none [&_input[type='number']::-webkit-outer-spin-button]:appearance-none">
              <NumberFieldContent>
                <div class="flex items-center">
                  <NumberFieldInput placeholder="Limit" class="text-sm" :step="10" />
                  <NumberFieldDecrement :step="10" />
                  <NumberFieldIncrement :step="10" />
                </div>
              </NumberFieldContent>
            </NumberField>

            <Button @click="executeQuery" :disabled="isLoading" class="w-[100px] h-10">
              <Search class="mr-2 h-4 w-4" />
              Search
            </Button>
          </div>
        </div>
      </div>

      <!-- Query Input Row -->
      <div class="w-full">
        <div class="flex flex-col gap-1.5">
          <Label class="text-sm font-medium">{{ queryMode === 'raw_sql' ? 'SQL Query' : 'Filter Conditions' }}</Label>
          <div class="bg-muted/30 rounded-lg p-4">
            <template v-if="queryMode === 'raw_sql'">
              <SQLEditor v-model="sqlQuery" :source-database="selectedSourceDetails.database"
                :source-table="selectedSourceDetails.table" :start-timestamp="getTimestamps().start"
                :end-timestamp="getTimestamps().end" @execute="executeQuery" />
            </template>
            <template v-else>
              <DataTableFilters :columns="columns" @update:filters="handleFiltersUpdate" />
            </template>
          </div>
        </div>
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
      <div v-else-if="logs?.length > 0">
        <DataTable :data="logs" :columns="tableColumns" :stats="queryStats" />
      </div>
      <EmptyState v-else />
    </div>
  </div>
</template>

<style scoped>
.required::after {
  content: " *";
  color: hsl(var(--destructive));
}
</style>
