<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, watch, computed, reactive } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import DateRangeFilter from '@/components/DateRangeFilter.vue'
import { api } from '@/services/api'
import Tag from 'primevue/tag'
import Skeleton from 'primevue/skeleton'
import { useToast } from 'primevue/usetoast'
import Toast from 'primevue/toast'
import Drawer from 'primevue/drawer'
import type { Log, LogResponse } from '@/types/logs'
import type { Source } from '@/types/source'
import Select from 'primevue/select'
import FloatLabel from 'primevue/floatlabel'
import Button from 'primevue/button'
import LogSchemaSidebar from '@/components/LogSchemaSidebar.vue'
import LogFieldValue from '@/components/LogFieldValue.vue'
import MultiSelect from 'primevue/multiselect'
import { useDebounceFn } from '@vueuse/core'
import QueryEditor from '@/components/QueryEditor.vue'

// Add proper interface for table state at the top of the file
interface TableState {
  rows: number
  totalRecords: number
  loading: boolean
  logs: ProcessedLog[]
  hasMore: boolean
  first: number
  sortField?: string
  sortOrder?: number
}

// Add at the top with other refs, after the imports
const intersectionObserver = ref<IntersectionObserver | null>(null)

// Move state declarations to the top
const sourcesLoading = ref(true)
const error = ref<string | null>(null)
const route = useRoute()
const router = useRouter()
const sources = ref<Source[]>([])
const rowsPerPageOptions = [50, 100, 500, 1000]
const searchQuery = ref('')
const severityText = ref('')

// Props and other refs
const props = defineProps<{
  sourceId?: string
  initialStartTime?: string
  initialEndTime?: string
  initialSearchQuery?: string
  initialSeverity?: string
}>()

const sourceId = ref<string | undefined>(props.sourceId)
const startDate = ref(
  props.initialStartTime
    ? new Date(props.initialStartTime)
    : new Date(Date.now() - 60 * 60 * 1000)
)
const endDate = ref(
  props.initialEndTime
    ? new Date(props.initialEndTime)
    : new Date()
)

// Add loading state refs
const initialLoad = ref(true) // Track first load to show skeleton

// Add new ref for query mode
const queryMode = ref<'basic' | 'logchefql' | 'sql'>('basic')
const queryString = ref('')

// Now define loadLogs and resetLogs
// Unified data loading function
const loadLogs = async (isInitialLoad = false) => {
  await fetchLogsPage({
    first: tableState.first,
    rows: tableState.rows
  })

  if (isInitialLoad) {
    initialLoad.value = false
  }
}

const resetLogs = async () => {
  tableState.logs = []
  error.value = null
  await fetchLogsPage({
    first: 0,
    rows: tableState.rows
  })
}

// Create computed property for URL parameters
const queryParams = computed(() => ({
  source: sourceId.value,
  start_time: startDate.value?.toISOString(),
  end_time: endDate.value?.toISOString()
}))

// Update URL when parameters change
watch([sourceId, startDate, endDate], () => {
  router.replace({
    query: {
      ...route.query,
      ...queryParams.value
    }
  })
}, { deep: true })

const toast = useToast()

// Type for the selected log
const selectedLog = ref<Log | null>(null)
const drawerVisible = ref(false)

const showLogDetails = (event: { data: Log }) => {
  selectedLog.value = event.data
  drawerVisible.value = true
}

const copyToClipboard = async (data: Log | null) => {
  if (!data) return
  try {
    await navigator.clipboard.writeText(JSON.stringify(data, null, 2))
    toast.add({
      severity: 'success',
      summary: 'Copied',
      detail: 'Log data copied to clipboard',
      life: 2000
    })
  } catch (err) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to copy to clipboard',
      life: 3000
    })
  }
}

const isProgressiveLoading = ref(false)
const progressPercentage = ref(0)

// Initial fetch of sources to get the first sourceId
async function fetchSources() {
  try {
    sourcesLoading.value = true
    const sourcesData = await api.fetchSources()
    if (sourcesData && sourcesData.length > 0) {
      sources.value = sourcesData
      sourceId.value = sourcesData[0].ID
    } else {
      error.value = 'No sources available'
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to fetch sources'
  } finally {
    sourcesLoading.value = false
  }
}

const getSeverityType = (severityText) => {
  switch (severityText?.toLowerCase()) {
    case 'error': return 'danger'
    case 'warn': return 'warning'
    case 'info': return 'secondary'
    case 'debug': return 'success'
    default: return null
  }
}

// Add back the schema ref
const schema = ref<any[]>([])

// Function to fetch both logs and schema concurrently
const fetchLogsAndSchema = async () => {
  if (!sourceId.value) {
    console.warn('⚠️ fetchLogsAndSchema: No sourceId')
    return
  }

  try {
    tableState.loading = true
    const controller = new AbortController()

    // Fetch both schema and logs concurrently
    const [schemaResponse, logsResponse] = await Promise.all([
      api.getLogSchema(sourceId.value, {
        start_time: startDate.value?.toISOString(),
        end_time: endDate.value?.toISOString()
      }, controller.signal),
      api.getLogs(sourceId.value, {
        start_time: startDate.value?.toISOString(),
        end_time: endDate.value?.toISOString(),
        offset: 0,
        limit: tableState.rows,
        search_query: searchQuery.value || '',
        severity_text: severityText.value || ''
      }, controller.signal)
    ])

    if (!controller.signal.aborted) {
      schema.value = schemaResponse || []
      tableState.logs = logsResponse.logs || []
      tableState.totalRecords = logsResponse.total_count
      tableState.hasMore = logsResponse.has_more || false

      // Set up intersection observer after logs are loaded
      nextTick(() => {
        setupIntersectionObserver()
      })
    }

  } catch (err) {
    if (err.name !== 'AbortError') {
      console.error('❌ Error in fetchLogsAndSchema:', err)
      error.value = err instanceof Error ? err.message : 'Failed to fetch data'
      schema.value = []
      tableState.logs = []
    }
  } finally {
    tableState.loading = false
    initialLoad.value = false
  }
}

// Add a new function to set up the intersection observer
const setupIntersectionObserver = () => {
  // Disconnect existing observer if any
  intersectionObserver.value?.disconnect()

  // Create new observer
  intersectionObserver.value = new IntersectionObserver(
    (entries) => {
      const entry = entries[0]
      if (entry.isIntersecting && !tableState.loading && tableState.hasMore) {
        console.log('🔄 Intersection triggered, fetching more logs')
        fetchMoreLogs()
      }
    },
    { threshold: 0.1 }
  )

  // Observe the trigger element
  const trigger = document.querySelector('#load-trigger')
  if (trigger && intersectionObserver.value) {
    intersectionObserver.value.observe(trigger)
    console.log('👀 Observer set up on load-trigger')
  } else {
    console.warn('⚠️ Could not find load-trigger element')
  }
}

// Update onMounted to remove the observer setup (it's now handled in fetchLogsAndSchema)
onMounted(async () => {
  console.log('🎬 Component mounted')

  try {
    await fetchSources()
    console.log('📡 Sources fetched:', {
      sourcesCount: sources.value.length,
      currentSourceId: sourceId.value
    })

    // Use URL parameters or fallback to default sourceId
    if (route.query.source) {
      sourceId.value = route.query.source as string
    } else if (!sourceId.value && sources.value.length > 0) {
      sourceId.value = sources.value[0].ID
    }
    console.log('🎯 Set sourceId:', sourceId.value)

    if (sourceId.value) {
      // Use URL parameters for date range if available
      startDate.value = route.query.start_time
        ? new Date(route.query.start_time as string)
        : props.initialStartTime
          ? new Date(props.initialStartTime)
          : new Date(Date.now() - 60 * 60 * 1000)

      endDate.value = route.query.end_time
        ? new Date(route.query.end_time as string)
        : props.initialEndTime
          ? new Date(props.initialEndTime)
          : new Date()

      console.log('📅 Date range set:', {
        startDate: startDate.value,
        endDate: endDate.value
      })

      // Fetch both schema and logs (this will also set up the observer)
      await fetchLogsAndSchema()
    }
  } catch (err) {
    console.error('Error during component initialization:', err)
    error.value = err instanceof Error ? err.message : 'Failed to initialize'
  }
})

// Move all cleanup to the top-level onUnmounted
onUnmounted(() => {
  // Clean up resize handlers if any
  resizeCleanup.value?.()

  // Clean up intersection observer
  intersectionObserver.value?.disconnect()
})

// Add a method to get shareable URL
const getShareableURL = () => {
  const url = new URL(window.location.href)
  url.search = new URLSearchParams(queryParams.value as Record<string, string>).toString()
  return url.toString()
}

// Add copy URL button
const copyShareableURL = async () => {
  try {
    await navigator.clipboard.writeText(getShareableURL())
    toast.add({
      severity: 'success',
      summary: 'URL Copied',
      detail: 'Shareable URL has been copied to clipboard',
      life: 2000
    })
  } catch (err) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to copy URL',
      life: 3000
    })
  }
}

const columns = computed(() => {
  const result = [
    { field: 'timestamp', header: 'Timestamp', required: true },
    { field: 'severity_text', header: 'Severity', required: false },
    { field: 'body', header: 'Message', required: false }
  ]

  // Add other fields from the first log
  if (tableState.logs.length > 0) {
    const firstLog = tableState.logs[0]
    Object.keys(firstLog).forEach(key => {
      // Skip default fields we already added
      if (!['timestamp', 'severity_text', 'body'].includes(key) &&
        !result.find(col => col.field === key)) {
        result.push({
          field: key,
          header: key,
          required: false
        })
      }
    })

    // Add nested fields from log_attributes if they exist
    if (firstLog.log_attributes) {
      Object.keys(firstLog.log_attributes).forEach(key => {
        result.push({
          field: `log_attributes.${key}`,
          header: key,
          required: false
        })
      })
    }
  }

  return result
})

const selectedColumns = ref([
  { field: 'timestamp', header: 'Timestamp', required: true },
  { field: 'severity_text', header: 'Severity', required: false },
  { field: 'body', header: 'Message', required: false }
])

// Add a function to reprocess logs when columns change
const reprocessLogs = (logs: Log[]) => {
  return logs.map(log => ({
    ...log,
    formatted_timestamp: formatTimestamp(log.timestamp),
    severity_type: getSeverityType(log.severity_text),
    nested_values: selectedColumns.value.reduce((acc, col) => {
      acc[col.field] = getNestedValue(log, col.field)
      return acc
    }, {} as Record<string, any>)
  }))
}

// Update onToggleColumns to reprocess logs with new columns
const onToggleColumns = (val) => {
  // Always include required columns (like timestamp)
  const requiredColumns = columns.value.filter(col => col.required)
  selectedColumns.value = [
    ...requiredColumns,
    ...val.filter(col => !col.required)
  ]

  // Reprocess existing logs with new column selection
  if (tableState.logs.length) {
    tableState.logs = reprocessLogs(tableState.logs as Log[])
  }
}

// Update fetchMoreLogs to re-setup the observer after loading more logs
const fetchMoreLogs = async () => {
  if (tableState.loading || !tableState.hasMore) return

  try {
    tableState.loading = true
    const response = await api.getLogs(sourceId.value!, {
      start_time: startDate.value?.toISOString(),
      end_time: endDate.value?.toISOString(),
      offset: tableState.logs.length,
      limit: tableState.rows,
      search_query: searchQuery.value || '',
      severity_text: severityText.value || ''
    })

    if (response.logs?.length) {
      // Process new logs before adding them
      const processedNewLogs = reprocessLogs(response.logs)
      tableState.logs = [...tableState.logs, ...processedNewLogs]
      tableState.hasMore = response.has_more || false
      tableState.totalRecords = response.total_count

      // Re-setup observer after adding new logs
      nextTick(() => {
        setupIntersectionObserver()
      })
    } else {
      tableState.hasMore = false
    }
  } catch (err) {
    console.error('Error fetching more logs:', err)
    error.value = err instanceof Error ? err.message : 'Failed to fetch logs'
  } finally {
    tableState.loading = false
  }
}

// Update updateVisibleColumns to use reprocessing
const updateVisibleColumns = (fields: string[]) => {
  selectedColumns.value = columns.value.filter(col =>
    col.required || fields.includes(col.field)
  )

  // Reprocess logs with new column selection
  if (tableState.logs.length) {
    tableState.logs = reprocessLogs(tableState.logs as Log[])
  }
}

// Update sortedVisibleColumns computed property
const sortedVisibleColumns = computed(() =>
  selectedColumns.value.map(col => col.field)
)

const getNestedValue = (obj: any, path: string) => {
  if (!obj) return '-'

  // Special handling for nested attributes
  if (path.startsWith('log_attributes.')) {
    const [_, ...attributePath] = path.split('.')
    const value = obj.log_attributes?.[attributePath.join('.')]
    return value ?? '-'
  }

  // For regular fields
  const value = path.split('.').reduce((acc, part) => {
    if (acc === undefined || acc === null) return undefined
    return acc[part]
  }, obj)

  return value ?? '-'
}

const formatColumnHeader = (field: string) => {
  // Remove duplicate log_attributes prefix if present
  return field.replace('log_attributes.log_attributes.', 'log_attributes.')
}

// Add these functions for column width persistence
const getColumnWidthKey = (sourceId: string) => `log-column-widths-${sourceId}`

// Function to save column widths
const saveColumnWidths = () => {
  if (!sourceId.value) return

  const widths: Record<string, string> = {}
  selectedColumns.value.forEach(col => {
    const th = document.querySelector(`th[data-field="${col.field}"]`)
    if (th) {
      widths[col.field] = th.style.width
    }
  })

  localStorage.setItem(getColumnWidthKey(sourceId.value), JSON.stringify(widths))
}

// Function to load saved column widths
const loadColumnWidths = () => {
  if (!sourceId.value) return

  const savedWidths = localStorage.getItem(getColumnWidthKey(sourceId.value))
  if (savedWidths) {
    return JSON.parse(savedWidths)
  }
  return null
}

// Update getColumnStyle to use saved widths
const getColumnStyle = (field: string) => {
  const savedWidths = loadColumnWidths()

  if (savedWidths && savedWidths[field]) {
    return {
      width: savedWidths[field],
      minWidth: savedWidths[field]
    }
  }

  // Fallback to default widths
  switch (field) {
    case 'timestamp':
      return {
        width: '200px',
        minWidth: '200px'
      }
    case 'severity_text':
      return {
        width: '100px',
        minWidth: '100px'
      }
    case 'body':
      return {
        minWidth: '300px'
      }
    default:
      return {
        minWidth: '150px'
      }
  }
}

// Add cleanup state for resize handlers
const resizeCleanup = ref<(() => void) | null>(null)

// Update the startResize function to store cleanup
const startResize = (event: MouseEvent, field: string) => {
  const th = event.target as HTMLElement
  const startX = event.pageX
  const startWidth = th.closest('th')?.offsetWidth || 0

  const handleMouseMove = (e: MouseEvent) => {
    const width = startWidth + (e.pageX - startX)
    if (th.closest('th')) {
      th.closest('th')!.style.width = `${width}px`
    }
  }

  const handleMouseUp = () => {
    document.removeEventListener('mousemove', handleMouseMove)
    document.removeEventListener('mouseup', handleMouseUp)
    // Save column widths after resize
    saveColumnWidths()
    resizeCleanup.value = null
  }

  // Clean up any existing handlers
  resizeCleanup.value?.()

  // Set up new handlers
  document.addEventListener('mousemove', handleMouseMove)
  document.addEventListener('mouseup', handleMouseUp)

  // Store cleanup function
  resizeCleanup.value = () => {
    document.removeEventListener('mousemove', handleMouseMove)
    document.removeEventListener('mouseup', handleMouseUp)
  }
}

// Format timestamp for developer-friendly display
const formatTimestamp = (timestamp: string) => {
  try {
    const date = new Date(timestamp)
    if (isNaN(date.getTime())) {
      throw new Error('Invalid date')
    }

    // Format without fractionalSecondDigits
    const formatter = new Intl.DateTimeFormat('en-US', {
      month: 'short',
      day: '2-digit',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      timeZone: 'UTC',
      hour12: false
    })

    // Manually add milliseconds
    const formattedDate = formatter.format(date)
    const milliseconds = date.getUTCMilliseconds().toString().padStart(3, '0')
    return `${formattedDate}.${milliseconds} UTC`
  } catch (err) {
    console.error('Error formatting timestamp:', timestamp, err)
    return timestamp
  }
}

// Add export function that handles dynamic columns
const exportLogs = () => {
  if (!dt.value || !tableState.logs.length) return

  // Get all selected columns
  const columnsToExport = selectedColumns.value.map(col => ({
    field: col.field,
    header: formatColumnHeader(col.field)
  }))

  // Prepare CSV data
  const csvData = tableState.logs.map(log => {
    const row = {}
    columnsToExport.forEach(col => {
      row[col.header] = getNestedValue(log, col.field)
    })
    return row
  })

  // Convert to CSV
  const headers = columnsToExport.map(col => col.header)
  const csvContent = [
    headers.join(','),
    ...csvData.map(row => headers.map(header => {
      const value = row[header]
      // Handle values that might contain commas or quotes
      const cellValue = String(value).replace(/"/g, '""')
      return `"${cellValue}"`
    }).join(','))
  ].join('\n')

  // Create and trigger download
  const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' })
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.setAttribute('download', `logs_export_${new Date().toISOString()}.csv`)
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

// Add DataTable ref
const dt = ref<any>(null)

// Add debounced filter handler
const debouncedSearch = useDebounceFn(() => {
  tableState.first = 0
  fetchLogsPage({ first: 0, rows: tableState.rows })
}, 300)

// Add sort handler
const onSort = async (event: { field: string; order: number }) => {
  tableState.sortField = event.field
  tableState.sortOrder = event.order
  tableState.first = 0 // Reset to first page when sorting
  await onLazyLoad({ first: 0, rows: tableState.rows })
}

// Optimized prefetch with request cancellation
let prefetchController: AbortController | null = null

const prefetchMoreLogs = async () => {
  if (tableState.loading || !tableState.hasMore) return

  // Cancel previous prefetch if exists
  prefetchController?.abort()
  prefetchController = new AbortController()

  try {
    const response = await api.getLogs(sourceId.value, {
      start_time: startDate.value?.toISOString(),
      end_time: endDate.value?.toISOString(),
      offset: tableState.logs.length,
      limit: tableState.rows,
      search_query: searchQuery.value || '',
      severity_text: severityText.value || ''
    }, prefetchController.signal)

    if (response.logs?.length) {
      // Process new logs before adding them
      const processedLogs = reprocessLogs(response.logs)
      tableState.logs = [...tableState.logs, ...processedLogs]
      tableState.hasMore = response.has_more || false
    }
  } catch (err) {
    if (err.name !== 'AbortError') {
      console.error('Error prefetching:', err)
    }
  }
}

// Add interface for preprocessed log
interface ProcessedLog extends Log {
  formatted_timestamp: string
  severity_type: string
  nested_values: Record<string, any>
}

// Update the fetchLogsPage function to preprocess logs
const fetchLogsPage = async (event: { first: number; rows: number }) => {
  if (!sourceId.value || tableState.loading) return

  try {
    tableState.loading = true
    const response = await api.getLogs(sourceId.value, {
      start_time: startDate.value?.toISOString(),
      end_time: endDate.value?.toISOString(),
      offset: event.first,
      limit: event.rows,
      search_query: searchQuery.value || '',
      severity_text: severityText.value || ''
    })

    // Preprocess logs once when we receive them
    const processedLogs = (response.logs || []).map(log => ({
      ...log,
      // Pre-format timestamp
      formatted_timestamp: formatTimestamp(log.timestamp),
      // Pre-compute severity type
      severity_type: getSeverityType(log.severity_text),
      // Pre-resolve nested values for selected columns
      nested_values: selectedColumns.value.reduce((acc, col) => {
        acc[col.field] = getNestedValue(log, col.field)
        return acc
      }, {} as Record<string, any>)
    }))

    tableState.logs = processedLogs
    tableState.totalRecords = response.total_count
    tableState.hasMore = response.has_more || false
  } catch (err) {
    console.error('❌ Error fetching logs:', err)
    error.value = err instanceof Error ? err.message : 'Failed to fetch logs'
  } finally {
    tableState.loading = false
  }
}

// Add a watcher for date changes
watch([startDate, endDate], () => {
  // Reset pagination and fetch new logs when date range changes
  tableState.first = 0
  fetchLogsPage({ first: 0, rows: tableState.rows })
}, { deep: true })

// Add proper type for onLazyLoad
const onLazyLoad = fetchLogsPage

// Add a safe getter for nested values
const getProcessedValue = (log: ProcessedLog | Log, field: string) => {
  if ('nested_values' in log) {
    return log.nested_values[field]
  }
  return getNestedValue(log, field)
}

const tableState = reactive<TableState>({
  rows: 100,
  totalRecords: 0,
  loading: false,
  logs: [],
  hasMore: true,
  first: 0,
  sortField: undefined,
  sortOrder: undefined
})

const handleSearch = async () => {
  tableState.first = 0
  await fetchLogsPage({ first: 0, rows: tableState.rows })
}
</script>

<template>
  <div class="flex flex-col min-h-screen">
    <!-- Fixed Header -->
    <div class="flex-none bg-white border-b border-gray-200">
      <!-- Primary Controls -->
      <div class="p-4 border-b border-gray-100">
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-4 flex-1">
            <!-- Left side: Source and Columns -->
            <div class="flex items-center gap-3 flex-1">
              <FloatLabel class="w-[250px]" variant="on">
                <Select v-model="sourceId" :options="sources" optionLabel="Name" optionValue="ID" class="w-full"
                  @change="resetLogs" />
                <label>Choose Log Source</label>
              </FloatLabel>

              <FloatLabel class="w-[350px]" variant="on">
                <MultiSelect v-model="selectedColumns" :options="columns" optionLabel="header" display="chip"
                  class="w-full" @update:modelValue="onToggleColumns" :disabled="tableState.loading" />
                <label>Select Columns</label>
              </FloatLabel>
            </div>

            <!-- Right side: Time Range -->
            <div class="flex items-center gap-3">
              <FloatLabel class="min-w-[400px]" variant="on">
                <DateRangeFilter v-model:startDate="startDate" v-model:endDate="endDate" @fetch="fetchLogsAndSchema" />
                <label>Time Range</label>
              </FloatLabel>
              <div class="h-6 w-px bg-gray-200 mx-2"></div>

              <!-- Replace the batch size selector -->
              <FloatLabel class="w-24" variant="on">
                <Select v-model="tableState.rows" :options="[100, 200, 500, 1000]" class="w-full" :pt="{
                  input: 'text-sm'
                }" />
                <label>Batch Size</label>
              </FloatLabel>

              <Button icon="pi pi-share-alt" severity="secondary" text v-tooltip.bottom="'Share Query'"
                @click="copyShareableURL" />
              <Button icon="pi pi-download" severity="secondary" text @click="exportLogs"
                v-tooltip.bottom="'Export as CSV'" :disabled="!tableState.logs.length" />
            </div>
          </div>
        </div>
      </div>

      <!-- Query Editor Section -->
      <div class="p-4">
        <QueryEditor v-model="queryString" :loading="tableState.loading" @search="handleSearch" />
      </div>
    </div>

    <!-- Content area -->
    <div class="flex-1">
      <!-- Loading skeleton -->
      <div v-if="initialLoad" class="p-4 space-y-4">
        <!-- Header skeleton -->
        <div class="flex space-x-4 mb-6">
          <Skeleton width="160px" height="2rem" />
          <Skeleton width="90px" height="2rem" />
          <Skeleton width="200px" height="2rem" />
        </div>

        <!-- Rows skeleton -->
        <div v-for="i in 10" :key="i" class="flex space-x-4">
          <Skeleton width="160px" height="2.5rem" />
          <Skeleton width="90px" height="2.5rem" />
          <Skeleton height="2.5rem" />
        </div>
      </div>

      <!-- Error state -->
      <div v-else-if="error" class="p-8 text-center">
        <div class="text-red-500 mb-2">{{ error }}</div>
        <Button label="Retry" severity="secondary" @click="() => loadLogs(true)" />
      </div>

      <!-- Empty state -->
      <div v-else-if="!tableState.logs.length" class="p-8 text-center">
        <div class="text-gray-500 mb-2">No logs found</div>
        <Button label="Change Filters" severity="secondary" @click="resetLogs" />
      </div>

      <!-- Table section -->
      <div v-else>
        <table class="w-full border-collapse">
          <thead class="sticky top-0 bg-white shadow-sm z-10">
            <tr>
              <th v-for="col in selectedColumns" :key="col.field" :data-field="col.field"
                :style="getColumnStyle(col.field)"
                class="text-left p-2 font-semibold text-gray-700 relative border-b border-gray-200">
                {{ formatColumnHeader(col.field) }}
                <div class="absolute right-0 top-0 bottom-0 w-1 cursor-col-resize hover:bg-gray-300"
                  @mousedown="startResize($event, col.field)"></div>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="log in tableState.logs" :key="log.id" class="hover:bg-gray-50 cursor-pointer"
              @click="showLogDetails({ data: log })">
              <td v-for="col in selectedColumns" :key="col.field" :style="getColumnStyle(col.field)"
                class="p-2 border-b border-gray-200">
                <LogFieldValue :field="col.field" :value="getProcessedValue(log, col.field)" />
              </td>
            </tr>
          </tbody>
        </table>

        <!-- Load More Trigger -->
        <div id="load-trigger" class="h-20 w-full flex items-center justify-center" v-show="tableState.hasMore">
          <div v-if="tableState.loading" class="text-gray-500">
            <i class="pi pi-spinner animate-spin mr-2"></i>
            Loading more logs...
          </div>
        </div>
      </div>
    </div>
  </div>

  <!-- Simplified Log Details Drawer -->
  <Drawer v-model:visible="drawerVisible" position="right" :modal="true" :dismissable="true" :closable="true"
    :style="{ width: 'min(85vw, 960px)' }" class="drawer-wide" :pt="{
      root: { class: 'border-l border-gray-200', style: { width: 'min(85vw, 960px)' } },
      header: { class: 'bg-gray-50 px-6 py-4 border-b border-gray-200' },
      content: { class: 'p-6 overflow-y-auto' }
    }">
    <template #header>
      <div class="flex justify-between items-center">
        <h3 class="text-lg font-semibold text-gray-900">Log Details</h3>
        <button @click="copyToClipboard(selectedLog)"
          class="text-gray-400 hover:text-gray-600 p-2 rounded-md hover:bg-gray-100" title="Copy to clipboard">
          <i class="pi pi-copy"></i>
        </button>
      </div>
    </template>

    <pre v-if="selectedLog" class="text-sm font-mono bg-gray-50 p-4 rounded-md overflow-x-auto whitespace-pre-wrap">{{
      JSON.stringify(selectedLog, null, 2) }}</pre>
  </Drawer>
</template>

<style>
/* Add these styles */
th {
  position: relative;
  background: white;
  user-select: none;
}

th::after {
  content: '';
  position: absolute;
  right: 0;
  top: 0;
  bottom: 0;
  width: 1px;
  background: #e5e7eb;
}

/* Make resize handle easier to grab */
th .cursor-col-resize {
  width: 8px;
  margin-right: -4px;
  z-index: 1;
}

/* Ensure table cells don't wrap */
td,
th {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  height: 36px;
  line-height: 1.2;
  font-size: 0.875rem;
}

/* Add transition for hover effects */
tr {
  transition: background-color 0.15s ease;
  border-bottom: 1px solid #e5e7eb;
}

tr:hover {
  background-color: rgb(249, 250, 251);
}

/* Keep existing styles */
table {
  border-collapse: collapse;
  table-layout: fixed;
  width: 100%;
}

thead {
  position: sticky;
  top: 0;
  z-index: 10;
  background: white;
  contain: style layout;
}

/* Update border color for better contrast */
tr {
  transition: background-color 0.15s ease;
  border-bottom: 1px solid #e5e7eb;
}

/* Optional: Add subtle border between columns */
td,
th {
  border-right: 1px solid #f3f4f6;
}

/* Add Query Editor styles */
.query-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px;
  background: var(--surface-ground);
  border-radius: 6px;
}

:deep(.query-editor-container) {
  margin: 0;
  box-shadow: var(--card-shadow);
}
</style>