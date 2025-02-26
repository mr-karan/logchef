<script setup lang="ts">
import { ref, onMounted, watch, computed } from 'vue'
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
import type { FilterCondition } from '@/api/explore'
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
import DataTableFilters from './table/data-table-filters.vue'
import SQLEditor from '@/components/sql-editor/SQLEditor.vue'
import { formatSourceName } from '@/utils/format'
import { useSourcesStore } from '@/stores/sources'
import { useExploreStore } from '@/stores/explore'
import { useSqlGenerator } from '@/composables/useSqlGenerator'
import SqlPreview from '@/components/sql-preview/SqlPreview.vue'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from '@/components/ui/collapsible'
import { ChevronRight } from 'lucide-vue-next'
import SmartFilterBar from '@/components/smart-filter/SmartFilterBar.vue'

const router = useRouter()
const sourcesStore = useSourcesStore()
const exploreStore = useExploreStore()
const { toast } = useToast()
const route = useRoute()

// Local UI state
const isLoading = computed(() => exploreStore.isLoading)
const previewOpen = ref(false)

// Flag to prevent unnecessary URL updates on initial load
const isInitialLoad = ref(true)

// Date state
const currentTime = now(getLocalTimeZone())

// Computed DateRange for v-model binding
const dateRange = computed({
  get: () => ({
    start: exploreStore.data.timeRange?.start || currentTime.subtract({ hours: 3 }),
    end: exploreStore.data.timeRange?.end || currentTime
  }),
  set: (newValue: DateRange | null) => {
    if (newValue?.start && newValue?.end) {
      const range = {
        start: newValue.start as unknown as DateValue,
        end: newValue.end as unknown as DateValue
      }
      exploreStore.setTimeRange(range)
      // Update SQL generator when time range changes
      updateSqlGenerator()
    }
  }
})

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
  const source = sourcesStore.sources.find(s => s.id === exploreStore.data.sourceId)
  return source ? formatSourceName(source) : 'Select a source'
})

// Fix the selected source details computed to handle null sources
const selectedSourceDetails = computed(() => {
  if (!sourcesStore.sources) return { database: '', table: '' }
  const source = sourcesStore.sources.find(s => s.id === exploreStore.data.sourceId)
  return source ? {
    database: source.connection.database,
    table: source.connection.table_name
  } : {
    database: '',
    table: ''
  }
})

// Function to get timestamps from store's time range
const getTimestamps = () => {
  const timeRange = exploreStore.data.timeRange
  if (!timeRange?.start || !timeRange?.end) {
    return {
      start: 0,
      end: 0
    }
  }

  const startDate = new Date(
    timeRange.start.year,
    timeRange.start.month - 1,
    timeRange.start.day,
    'hour' in timeRange.start ? timeRange.start.hour : 0,
    'minute' in timeRange.start ? timeRange.start.minute : 0,
    'second' in timeRange.start ? timeRange.start.second : 0
  )

  const endDate = new Date(
    timeRange.end.year,
    timeRange.end.month - 1,
    timeRange.end.day,
    'hour' in timeRange.end ? timeRange.end.hour : 0,
    'minute' in timeRange.end ? timeRange.end.minute : 0,
    'second' in timeRange.end ? timeRange.end.second : 0
  )

  return {
    start: startDate.getTime(),
    end: endDate.getTime()
  }
}

// Initialize SQL generator with options
const sqlGenerator = ref(useSqlGenerator({
  database: selectedSourceDetails.value.database,
  table: selectedSourceDetails.value.table,
  start_timestamp: getTimestamps().start,
  end_timestamp: getTimestamps().end,
  limit: exploreStore.data.limit,
}))

// Add computed for SQL preview state
const sqlPreviewState = computed(() => {
  if (!sqlGenerator.value?.previewSql) {
    return {
      sql: '',
      isValid: true,
      error: undefined
    }
  }
  const state = sqlGenerator.value.previewSql
  return {
    sql: state.sql ?? '',
    isValid: state.isValid ?? true,
    error: state.error
  }
})

// Function to initialize or update SQL generator
function updateSqlGenerator() {
  const timestamps = getTimestamps()
  sqlGenerator.value?.updateOptions({
    database: selectedSourceDetails.value.database,
    table: selectedSourceDetails.value.table,
    start_timestamp: timestamps.start,
    end_timestamp: timestamps.end,
    limit: exploreStore.data.limit,
  })

  // Always regenerate SQL preview to ensure it's visible
  sqlGenerator.value?.generatePreviewSql(exploreStore.data.filterConditions)
}

// Initialize state from URL parameters
function initializeFromURL() {
  const { source, limit, start_time, end_time, query } = route.query

  // Parse source ID
  if (source && typeof source === 'string') {
    exploreStore.setSource(source)
  }

  // Parse limit
  if (limit) {
    const parsedLimit = parseInt(limit as string)
    if (!isNaN(parsedLimit) && parsedLimit > 0 && parsedLimit <= 10000) {
      exploreStore.setLimit(parsedLimit)
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
          exploreStore.setTimeRange({
            start,
            end
          })
        }
      }
    } catch (e) {
      console.error('Error parsing timestamps:', e)
      // On error, set to last 3 hours
      const newCurrentTime = now(getLocalTimeZone())
      exploreStore.setTimeRange({
        start: newCurrentTime.subtract({ hours: 3 }),
        end: newCurrentTime
      })
    }
  }

  // Parse query
  if (query && typeof query === 'string') {
    exploreStore.setRawSql(decodeURIComponent(query))
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
    source: exploreStore.data.sourceId,
    limit: exploreStore.data.limit.toString(),
    start_time: timestamps.start.toString(),
    end_time: timestamps.end.toString()
  }

  if (exploreStore.data.rawSql.trim()) {
    query.query = encodeURIComponent(exploreStore.data.rawSql.trim())
  }

  router.replace({ query })
}

// Add activeTab ref for tab management
const activeTab = ref('filters')

// Update onMounted
onMounted(async () => {
  try {
    // First initialize from URL
    initializeFromURL()

    // Then load sources
    await sourcesStore.loadSources()

    // If source wasn't set from URL and we have sources, select the first one
    if (!exploreStore.data.sourceId && sourcesStore.sources.length > 0) {
      exploreStore.setSource(sourcesStore.sources[0].id)
    }

    // Validate selected source exists
    if (exploreStore.data.sourceId && !sourcesStore.sources.find(s => s.id === exploreStore.data.sourceId)) {
      toast({
        title: 'Warning',
        description: 'Selected source not found',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      })
      exploreStore.setSource(sourcesStore.sources[0]?.id || '')
    }

    // Set default time range if not set from URL
    if (!exploreStore.data.timeRange) {
      const newCurrentTime = now(getLocalTimeZone())
      exploreStore.setTimeRange({
        start: newCurrentTime.subtract({ hours: 3 }),
        end: newCurrentTime
      })
    }

    // Initialize SQL generator if we have a valid source
    if (selectedSourceDetails.value.database && selectedSourceDetails.value.table) {
      updateSqlGenerator()
    }

    // Only execute initial query if we have both a valid source and time range
    if (exploreStore.canExecuteQuery) {
      await executeQuery()
    }
  } catch (error) {
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  }
})

// Watch for changes that should trigger SQL updates
watch(
  () => ({
    sourceDetails: selectedSourceDetails.value,
    filterConditions: exploreStore.data.filterConditions,
    timeRange: exploreStore.data.timeRange,
    limit: exploreStore.data.limit
  }),
  () => {
    if (selectedSourceDetails.value.database && selectedSourceDetails.value.table) {
      updateSqlGenerator()
    }
  },
  { deep: true }
)

// Update query execution to always use SQL
async function executeQuery() {
  if (!exploreStore.data.sourceId || !selectedSourceDetails.value.database || !selectedSourceDetails.value.table) {
    toast({
      title: 'Error',
      description: 'Please select a valid source before executing query',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
    return
  }

  try {
    let rawSql: string
    if (activeTab.value === 'filters') {
      // Generate SQL from filters
      const queryState = sqlGenerator.value?.generateQuerySql(exploreStore.data.filterConditions)
      if (!queryState?.isValid) {
        throw new Error(queryState?.error || 'Invalid SQL query')
      }
      rawSql = queryState.sql
    } else {
      // Use raw SQL directly
      rawSql = exploreStore.data.rawSql
    }

    // Execute query through store
    await exploreStore.executeQuery(rawSql)

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
  }
}

const tableColumns = ref<ColumnDef<Record<string, any>>[]>([])

watch(
  () => exploreStore.data.columns,
  (newColumns) => {
    if (newColumns) {
      tableColumns.value = createColumns(newColumns)
    }
  },
  { immediate: true }
)

// Handle filter updates
const handleFiltersUpdate = (filters: FilterCondition[]) => {
  exploreStore.setFilterConditions(filters)
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
    <!-- Ultra-Compact Control Bar - Removed container styling -->
    <div class="flex items-center gap-2 h-9 mb-1">
      <!-- Source Selector -->
      <div class="w-[180px]">
        <Select v-model="exploreStore.data.sourceId" class="h-7">
          <SelectTrigger class="h-7 py-0 text-xs">
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

      <!-- Divider -->
      <div class="h-5 w-px bg-border self-center"></div>

      <!-- Time Range Picker - Increased width -->
      <div class="w-[340px]">
        <DateTimePicker v-model="dateRange" class="h-7" buttonClass="py-0 h-7 text-xs" />
      </div>

      <!-- Spacer -->
      <div class="flex-1"></div>

      <!-- Limit Control - Changed to dropdown -->
      <div class="flex items-center gap-1 mr-2">
        <span class="text-xs text-muted-foreground whitespace-nowrap">Limit:</span>
        <Select :model-value="exploreStore.data.limit.toString()"
          @update:model-value="val => exploreStore.setLimit(parseInt(val))" class="w-[80px]">
          <SelectTrigger class="h-7 py-0 text-xs">
            <SelectValue placeholder="Limit" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="100">100</SelectItem>
            <SelectItem value="500">500</SelectItem>
            <SelectItem value="1000">1,000</SelectItem>
            <SelectItem value="5000">5,000</SelectItem>
            <SelectItem value="10000">10,000</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <!-- Search Button -->
      <Button @click="executeQuery"
        :disabled="isLoading || !selectedSourceDetails.database || !selectedSourceDetails.table"
        class="h-7 px-3 py-0 text-xs">
        <Search class="mr-2 h-3 w-3" />
        Search
      </Button>
    </div>

    <!-- Query Builder Tabs -->
    <div class="rounded-md border bg-card">
      <Tabs v-model="activeTab" class="w-full" default-value="filters">
        <TabsList class="grid w-full grid-cols-2">
          <TabsTrigger value="filters">
            Filter Builder
          </TabsTrigger>
          <TabsTrigger value="raw_sql">
            SQL Query
          </TabsTrigger>
        </TabsList>

        <div class="p-3">
          <TabsContent value="filters" class="mt-0 space-y-3">
            <SmartFilterBar v-model="exploreStore.data.filterConditions" :columns="exploreStore.data.columns"
              @search="executeQuery" class="border rounded-md bg-muted/20 p-2" />

            <!-- SQL Preview -->
            <Collapsible v-model:open="previewOpen">
              <CollapsibleTrigger class="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground">
                <ChevronRight class="h-3.5 w-3.5" :class="{ 'rotate-90': previewOpen }" />
                Preview SQL
              </CollapsibleTrigger>
              <CollapsibleContent>
                <div class="mt-2 border rounded-md bg-muted/20 p-2">
                  <SqlPreview :sql="sqlPreviewState.sql" :is-valid="sqlPreviewState.isValid"
                    :error="sqlPreviewState.error || undefined" />
                </div>
              </CollapsibleContent>
            </Collapsible>
          </TabsContent>

          <TabsContent value="raw_sql" class="mt-0">
            <div class="border rounded-md bg-muted/20 p-2">
              <SQLEditor v-model="exploreStore.data.rawSql" :source-database="selectedSourceDetails.database"
                :source-table="selectedSourceDetails.table" :start-timestamp="getTimestamps().start"
                :end-timestamp="getTimestamps().end" @execute="executeQuery" />
            </div>
          </TabsContent>
        </div>
      </Tabs>
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
      <div v-else-if="exploreStore.data.logs?.length > 0" class="border rounded-md bg-card">
        <DataTable :columns="tableColumns" :data="exploreStore.data.logs" :stats="exploreStore.data.queryStats"
          :source-id="exploreStore.data.sourceId" />
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
