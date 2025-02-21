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
import { exploreApi } from '@/api/explore'
import type { QueryStats } from '@/api/explore'
import { getErrorMessage } from '@/api/types'
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
import { useSqlGenerator } from '@/composables/useSqlGenerator'
import SqlPreview from '@/components/sql-preview/SqlPreview.vue'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from '@/components/ui/collapsible'
import { ChevronRight } from 'lucide-vue-next'

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

// Add a computed for source validity
const isValidSource = computed(() => {
  return selectedSourceDetails.value.database && selectedSourceDetails.value.table
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

// Query state
const queryMode = ref<'filters' | 'raw_sql'>('filters')
const sqlQuery = ref('')
const filterConditions = ref<FilterCondition[]>([])
const previewOpen = ref(false)

// Initialize SQL generator
const sqlGenerator = ref(useSqlGenerator({
  database: '',
  table: '',
  limit: queryLimit.value,
  start_timestamp: getTimestamps().start,
  end_timestamp: getTimestamps().end,
}))

// Function to initialize or update SQL generator
function updateSqlGenerator() {
  const timestamps = getTimestamps()
  sqlGenerator.value.updateOptions({
    database: selectedSourceDetails.value.database,
    table: selectedSourceDetails.value.table,
    limit: queryLimit.value,
    start_timestamp: timestamps.start,
    end_timestamp: timestamps.end,
  })

  // Regenerate SQL preview if in filter mode
  if (queryMode.value === 'filters') {
    sqlGenerator.value.generatePreviewSql(filterConditions.value)
  }
}

// Watch for source changes
watch(
  [selectedSourceDetails, queryLimit, internalDateRange],
  () => {
    if (isValidSource.value) {
      updateSqlGenerator()
    }
  },
  { deep: true }
)

// Watch filter conditions
watch(filterConditions, (newFilters) => {
  if (queryMode.value === 'filters' && isValidSource.value) {
    sqlGenerator.value.generatePreviewSql(newFilters)
  }
}, { deep: true })

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

    // Initialize SQL generator if we have a valid source
    if (isValidSource.value) {
      updateSqlGenerator()
    }

    // Only execute initial query if we have a valid source
    if (selectedSource.value && isValidSource.value) {
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

// Update the executeQuery function to handle empty SQL case
async function executeQuery() {
  if (!selectedSource.value || !isValidSource.value) {
    toast({
      title: 'Error',
      description: 'Please select a valid source before executing query',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
    return
  }

  isLoading.value = true
  logs.value = []
  queryStats.value = {
    execution_time_ms: 0,
    rows_read: 0,
    bytes_read: 0
  }

  try {
    const timestamps = getTimestamps()
    let rawSql: string

    if (queryMode.value === 'filters') {
      // Use immediate SQL generation for actual query
      const queryState = sqlGenerator.value.generateQuerySql(filterConditions.value)
      if (!queryState.isValid) {
        throw new Error(queryState.error || 'Invalid SQL query')
      }
      rawSql = queryState.sql
    } else {
      // For raw SQL mode, ensure we have a valid query with timestamp conditions
      const baseQuery = sqlQuery.value || `SELECT * FROM ${selectedSourceDetails.value.database}.${selectedSourceDetails.value.table}`
      rawSql = buildBaseQuery(baseQuery)
    }

    // Ensure we have a valid query with timestamps
    if (!rawSql) {
      rawSql = buildBaseQuery(`SELECT * FROM ${selectedSourceDetails.value.database}.${selectedSourceDetails.value.table}`)
    }

    const params = {
      limit: queryLimit.value,
      start_timestamp: timestamps.start,
      end_timestamp: timestamps.end,
      mode: 'raw_sql' as const,
      raw_sql: rawSql
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

// Helper function to build base query with timestamps
function buildBaseQuery(baseQuery: string): string {
  const timestamps = getTimestamps()
  const timeConditions = `timestamp >= fromUnixTimestamp64Milli(${timestamps.start}) AND timestamp <= fromUnixTimestamp64Milli(${timestamps.end})`

  // If query already has WHERE clause, append with AND
  if (baseQuery.toUpperCase().includes('WHERE')) {
    return baseQuery.replace(/WHERE/i, `WHERE ${timeConditions} AND`)
  }

  // If no WHERE clause, add one with time conditions
  return `${baseQuery} WHERE ${timeConditions}`
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
  <div v-else class="flex flex-col h-full min-w-0 gap-3">
    <!-- Query Controls -->
    <div class="space-y-3">
      <!-- Core Query Controls Group -->
      <div class="flex gap-3 items-start bg-muted/20 rounded-md p-2">
        <!-- Source + Time Range Group -->
        <div class="flex gap-3 flex-1">
          <div class="w-[180px]">
            <Label class="text-xs text-muted-foreground">Source</Label>
            <Select v-model="selectedSource">
              <SelectTrigger class="h-9">
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

          <div class="w-[380px]">
            <Label class="text-xs text-muted-foreground">Time Range</Label>
            <DateTimePicker v-model="dateRange" class="h-9" />
          </div>
        </div>

        <!-- Execution Controls Group -->
        <div class="flex items-end gap-2">
          <NumberField v-model="queryLimit" :min="10" :max="10000" :step="10"
            class="w-[90px] [&_input[type='number']::-webkit-inner-spin-button]:appearance-none [&_input[type='number']::-webkit-outer-spin-button]:appearance-none">
            <NumberFieldContent>
              <div class="flex items-center h-9">
                <NumberFieldInput placeholder="Limit" class="text-sm" :step="10" />
                <NumberFieldDecrement :step="10" />
                <NumberFieldIncrement :step="10" />
              </div>
            </NumberFieldContent>
          </NumberField>

          <Button @click="executeQuery" :disabled="isLoading || !isValidSource" class="w-[90px] h-9">
            <Search class="mr-2 h-3.5 w-3.5" />
            Search
          </Button>
        </div>
      </div>

      <!-- Query Builder Tabs -->
      <div class="rounded-md border bg-card">
        <Tabs v-model="queryMode" class="w-full" default-value="filters">
          <TabsList class="w-full h-9 bg-muted/30">
            <TabsTrigger value="filters" class="flex-1 text-sm">Filter Builder</TabsTrigger>
            <TabsTrigger value="raw_sql" class="flex-1 text-sm">SQL Query</TabsTrigger>
          </TabsList>

          <div class="p-3">
            <TabsContent value="filters" class="mt-0 space-y-3">
              <DataTableFilters :columns="columns" @update:filters="handleFiltersUpdate"
                class="border rounded-md bg-muted/20 p-2" />

              <!-- Collapsible SQL Preview -->
              <Collapsible>
                <CollapsibleTrigger
                  class="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground">
                  <ChevronRight class="h-3.5 w-3.5" :class="{ 'rotate-90': previewOpen }" />
                  Preview SQL
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <div class="mt-2 border rounded-md bg-muted/20 p-2">
                    <SqlPreview :sql="sqlGenerator.value.previewSql.value.sql"
                      :is-valid="sqlGenerator.value.previewSql.value.isValid"
                      :error="sqlGenerator.value.previewSql.value.error || undefined" />
                  </div>
                </CollapsibleContent>
              </Collapsible>
            </TabsContent>

            <TabsContent value="raw_sql" class="mt-0">
              <div class="border rounded-md bg-muted/20 p-2">
                <SQLEditor v-model="sqlQuery" :source-database="selectedSourceDetails.database"
                  :source-table="selectedSourceDetails.table" :start-timestamp="getTimestamps().start"
                  :end-timestamp="getTimestamps().end" @execute="executeQuery" />
              </div>
            </TabsContent>
          </div>
        </Tabs>
      </div>
    </div>

    <!-- Results Area -->
    <div class="flex-1 min-h-0">
      <!-- Show Skeleton when loading -->
      <div v-if="isLoading" class="border rounded-md bg-card p-3 space-y-3">
        <!-- Simulate Table Header Skeleton -->
        <div class="flex gap-3">
          <Skeleton class="h-4 w-1/4" />
          <Skeleton class="h-4 w-1/4" />
          <Skeleton class="h-4 w-1/4" />
          <Skeleton class="h-4 w-1/4" />
        </div>
        <!-- Simulate Table Rows Skeleton -->
        <div class="space-y-2">
          <div v-for="n in 5" :key="n" class="flex gap-3">
            <Skeleton class="h-4 w-1/4" />
            <Skeleton class="h-4 w-1/4" />
            <Skeleton class="h-4 w-1/4" />
            <Skeleton class="h-4 w-1/4" />
          </div>
        </div>
      </div>
      <!-- Show Results when data is loaded -->
      <div v-else-if="logs?.length > 0" class="border rounded-md bg-card">
        <DataTable :data="logs" :columns="tableColumns" :stats="queryStats" />
      </div>
      <EmptyState v-else class="border rounded-md bg-card" />
    </div>
  </div>
</template>

<style scoped>
.required::after {
  content: " *";
  color: hsl(var(--destructive));
}
</style>
