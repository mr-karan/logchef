<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, watch, computed, reactive } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import DateRangeFilter from '@/components/DateRangeFilter.vue'
import { api } from '@/services/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Skeleton from 'primevue/skeleton'
import { useToast } from 'primevue/usetoast'
import Toast from 'primevue/toast'
import Drawer from 'primevue/drawer'
import Paginator from 'primevue/paginator'
import type { Log, LogResponse } from '@/types/logs'
import type { Source } from '@/types/source'
import Select from 'primevue/select'
import FloatLabel from 'primevue/floatlabel'
import Button from 'primevue/button'
import LogSchemaSidebar from '@/components/LogSchemaSidebar.vue'
import LogFieldValue from '@/components/LogFieldValue.vue'
import MultiSelect from 'primevue/multiselect'
import { useDebounceFn } from '@vueuse/core'

// Move state declarations to the top
const sourcesLoading = ref(true)
const error = ref<string | null>(null)
const route = useRoute()
const router = useRouter()
const sources = ref<Source[]>([])
const rowsPerPageOptions = [50, 100, 500, 1000]
const limit = ref(50)
const hasMore = ref(true)
const totalRecords = ref(0)
const searchQuery = ref('')
const severityText = ref('')
const lazyState = ref({
  first: 0,
  rows: limit.value
})
const schema = ref<any[]>([])

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
  // Reset all states
  tableState.first = 0
  tableState.logs = []
  error.value = null

  // Fetch fresh data
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

const offset = ref(0)
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

// Initialize data on mount
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

      // Fetch both schema and logs
      await fetchLogsAndSchema()
    }
  } catch (err) {
    console.error('Error during component initialization:', err)
    error.value = err instanceof Error ? err.message : 'Failed to initialize'
  }
})

onUnmounted(() => {
  // Save scroll position for restoration
  sessionStorage.setItem('logsScrollPosition', tableState.scrollPosition.toString())
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
  const result = []

  // Add default columns first
  result.push(
    { field: 'timestamp', header: 'Timestamp', required: true },
    { field: 'severity_text', header: 'Severity', required: false },
    { field: 'body', header: 'Message', required: false }
  )

  // Process schema fields
  schema.value?.forEach(field => {
    if (field.parent === 'log_attributes') {
      // For nested fields under log_attributes
      result.push({
        field: `log_attributes.${field.path[field.path.length - 1]}`,
        header: field.path[field.path.length - 1],
        required: false,
        type: field.type
      })
    } else if (field.children) {
      // For fields with children (nested structures)
      field.children.forEach(child => {
        result.push({
          field: child.path.join('.'),
          header: child.path[child.path.length - 1],
          required: false,
          type: child.type
        })
      })
    } else if (!['timestamp', 'severity_text', 'body'].includes(field.path[0])) {
      // For regular fields, excluding the ones we already added
      result.push({
        field: field.path.join('.'),
        header: field.path[0],
        required: false,
        type: field.type
      })
    }
  })

  return result
})

const selectedColumns = ref([
  { field: 'timestamp', header: 'Timestamp', required: true },
  { field: 'severity_text', header: 'Severity', required: false },
  { field: 'body', header: 'Message', required: false }
])

const onToggleColumns = (val) => {
  // Always include required columns (like timestamp)
  const requiredColumns = columns.value.filter(col => col.required)
  selectedColumns.value = [
    ...requiredColumns,
    ...val.filter(col => !col.required)
  ]
}

// Replace updateVisibleColumns with this
const updateVisibleColumns = (fields: string[]) => {
  selectedColumns.value = columns.value.filter(col =>
    col.required || fields.includes(col.field)
  )
}

// Update sortedVisibleColumns computed property
const sortedVisibleColumns = computed(() =>
  selectedColumns.value.map(col => col.field)
)

const getNestedValue = (obj: any, path: string) => {
    if (!obj) return undefined

    // Special handling for nested attributes
    if (path.startsWith('log_attributes.')) {
        const [_, ...attributePath] = path.split('.')
        const value = obj.log_attributes?.[attributePath.join('.')]
        return value === undefined ? '-' : value
    }

    // For regular fields
    const value = path.split('.').reduce((acc, part) => {
        if (acc === undefined) return undefined
        return acc[part]
    }, obj)

    return value === undefined ? '-' : value
}

const formatColumnHeader = (field: string) => {
    // Remove duplicate log_attributes prefix if present
    return field.replace('log_attributes.log_attributes.', 'log_attributes.')
}

// Add function to handle column styles
const getColumnStyle = (field: string) => {
    const isNested = field.includes('.')

    switch (field) {
        case 'timestamp':
            return {
                width: '160px',
                minWidth: '160px',
                backgroundColor: 'white'
            }
        case 'severity_text':
            return {
                width: '90px',
                minWidth: '90px',
                backgroundColor: 'white'
            }
        case 'body':
            return {
                minWidth: '300px',
                backgroundColor: 'white'
            }
        default:
            return {
                ...(isNested
                    ? { width: '140px', minWidth: '140px' }
                    : { minWidth: '120px' }
                ),
                backgroundColor: 'white'
            }
    }
}

// Format timestamp for developer-friendly display
const formatTimestamp = (timestamp: string) => {
  try {
    const date = new Date(timestamp)
    if (isNaN(date.getTime())) {
      throw new Error('Invalid date')
    }

    // Format: Nov 26 2024 11:30:34.000 UTC
    const formatter = new Intl.DateTimeFormat('en-US', {
      month: 'short',
      day: '2-digit',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      fractionalSecondDigits: 3,
      timeZone: 'UTC',
      hour12: false
    })

    return formatter.format(date) + ' UTC'
  } catch (err) {
    console.error('Error formatting timestamp:', timestamp, err)
    return timestamp
  }
}

// Update pagination methods
const prevPage = () => {
  if (lazyState.value.first > 0) {
    lazyState.value.first -= lazyState.value.rows
    loadLogs()
    const tableContainer = document.querySelector('.flex-1.overflow-y-auto')
    tableContainer?.scrollTo({ top: 0, behavior: 'smooth' })
  }
}

const nextPage = () => {
  if (hasMore.value) {
    lazyState.value.first += lazyState.value.rows
    loadLogs()
    const tableContainer = document.querySelector('.flex-1.overflow-y-auto')
    tableContainer?.scrollTo({ top: 0, behavior: 'smooth' })
  }
}

// Add a watcher for lazyState.rows changes
watch(() => lazyState.value.rows, (newLimit) => {
  // Reset to first page when limit changes
  lazyState.value.first = 0
  loadLogs()
}, { immediate: false })

const dt = ref()

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

const tableState = reactive({
  rows: 50,
  totalRecords: 0,
  loading: false,
  logs: [] as Log[],
  first: 0,
  scrollPosition: 0,
  sortField: null as string | null,
  sortOrder: null as number | null,
  hasMore: true
})

// Handle pagination events
const handlePageChange = async (event: { first: number; rows: number; page: number }) => {
  console.log('📄 Page Event:', event)
  if (tableState.loading) return

  try {
    tableState.loading = true
    const response = await api.getLogs(sourceId.value!, {
      start_time: startDate.value?.toISOString(),
      end_time: endDate.value?.toISOString(),
      offset: event.first,
      limit: event.rows,
      search_query: searchQuery.value || '',
      severity_text: severityText.value || ''
    })

    tableState.logs = response.logs || []
    tableState.totalRecords = response.total_count
  } catch (err) {
    console.error('❌ Page Error:', err)
    error.value = err instanceof Error ? err.message : 'Failed to fetch logs'
  } finally {
    tableState.loading = false
  }
}

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
      limit: 50,
      search_query: searchQuery.value || '',
      severity_text: severityText.value || ''
    }, prefetchController.signal)

    if (response.logs?.length) {
      tableState.logs = [...tableState.logs, ...response.logs]
      tableState.hasMore = response.has_more || false
    }
  } catch (err) {
    if (err.name !== 'AbortError') {
      console.error('Error prefetching:', err)
    }
  }
}

// Add this function to handle lazy loading
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

    tableState.logs = response.logs || []
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

// Update the DataTable component to use onLazyLoad

</script>

<template>
  <div class="h-screen flex flex-col overflow-hidden">
    <!-- Main Content Area -->
    <div class="flex-1 flex flex-col">
      <!-- Fixed Header -->
      <div class="flex-none bg-white border-b border-gray-200">
        <!-- Primary Controls -->
        <div class="p-4 border-b border-gray-100">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-4 flex-1">
              <!-- Left side: Source and Columns -->
              <div class="flex items-center gap-3 flex-1">
                <FloatLabel class="w-[250px]" variant="on">
                  <Select
                    v-model="sourceId"
                    :options="sources"
                    optionLabel="Name"
                    optionValue="ID"
                    class="w-full"
                    @change="resetLogs"
                  />
                  <label>Choose Log Source</label>
                </FloatLabel>

                <FloatLabel class="w-[350px]" variant="on">
                  <MultiSelect
                    v-model="selectedColumns"
                    :options="columns"
                    optionLabel="header"
                    display="chip"
                    class="w-full"
                    @update:modelValue="onToggleColumns"
                    :disabled="tableState.loading"
                  />
                  <label>Select Columns</label>
                </FloatLabel>
              </div>

              <!-- Right side: Time Range -->
              <div class="flex items-center gap-3">
                <FloatLabel class="min-w-[400px]" variant="on">
                  <DateRangeFilter
                    v-model:startDate="startDate"
                    v-model:endDate="endDate"
                    class="w-full"
                    @fetch="fetchLogsAndSchema"
                  />
                  <label>Time Range</label>
                </FloatLabel>
                <div class="h-6 w-px bg-gray-200 mx-2"></div>
                <Button
                  icon="pi pi-share-alt"
                  severity="secondary"
                  text
                  v-tooltip.bottom="'Share Query'"
                  @click="copyShareableURL"
                />
                <Button
                  icon="pi pi-download"
                  severity="secondary"
                  text
                  @click="exportLogs"
                  v-tooltip.bottom="'Export as CSV'"
                  :disabled="!tableState.logs.length"
                />
                <Paginator
                  v-if="!initialLoad && tableState.logs.length > 0"
                  :rows="tableState.rows"
                  :total-records="tableState.totalRecords"
                  :rows-per-page-options="[50, 100, 500, 1000]"
                  @page="handlePageChange"
                  template="FirstPageLink PrevPageLink PageLinks NextPageLink LastPageLink RowsPerPageDropdown"
                />
              </div>
            </div>
          </div>
        </div>

      </div>

      <!-- Rest of the DataTable code remains the same -->
      <div class="flex-1 overflow-y-auto">
        <!-- Loading skeleton -->
        <div v-if="initialLoad || tableState.loading" class="p-4 space-y-4">
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

        <!-- Empty state - consolidated -->
        <div v-else-if="!tableState.logs.length" class="p-8 text-center">
          <div class="text-gray-500 mb-2">No logs found</div>
          <Button label="Change Filters" severity="secondary" @click="resetLogs" />
        </div>

        <!-- DataTable -->
        <DataTable
          v-else
          ref="dt"
          :value="tableState.logs"
          dataKey="id"
          :rows="tableState.rows"
          :lazy="true"
          class="logs-table"
          :scrollable="true"
          scrollHeight="calc(100vh - 200px)"
          :resizableColumns="true"
          @row-click="showLogDetails"
          columnResizeMode="fit"
          @lazy="fetchLogsPage"
          :totalRecords="tableState.totalRecords"
          :loading="tableState.loading"
        >
          <Column v-for="field in sortedVisibleColumns"
                  :key="field"
                  :field="field"
                  :header="formatColumnHeader(field)"
                  :sortable="field === 'timestamp'"
                  :style="getColumnStyle(field)"
          >
            <template #body="{ data }">
              <LogFieldValue
                  :field="field"
                  :value="getNestedValue(data, field)"
              />
            </template>
          </Column>
        </DataTable>
      </div>
    </div>

    <!-- Simplified Log Details Drawer -->
    <Drawer
      v-model:visible="drawerVisible"
      position="right"
      :modal="true"
      :dismissable="true"
      :closable="true"
      :style="{ width: 'min(85vw, 960px)' }"
      class="drawer-wide"
      :pt="{
        root: { class: 'border-l border-gray-200', style: { width: 'min(85vw, 960px)' } },
        header: { class: 'bg-gray-50 px-6 py-4 border-b border-gray-200' },
        content: { class: 'p-6 overflow-y-auto' }
      }"
    >
      <template #header>
        <div class="flex justify-between items-center">
          <h3 class="text-lg font-semibold text-gray-900">Log Details</h3>
          <button
            @click="copyToClipboard(selectedLog)"
            class="text-gray-400 hover:text-gray-600 p-2 rounded-md hover:bg-gray-100"
            title="Copy to clipboard"
          >
            <i class="pi pi-copy"></i>
          </button>
        </div>
      </template>

      <pre v-if="selectedLog" class="text-sm font-mono bg-gray-50 p-4 rounded-md overflow-x-auto whitespace-pre-wrap">{{ JSON.stringify(selectedLog, null, 2) }}</pre>
    </Drawer>
  </div>
</template>

<style>
/* Remove duplicate styles and keep only these optimized versions */
.logs-table {
  height: 100%;
  contain: strict;
}

.logs-table .p-datatable-wrapper {
  border: none;
  height: 100%;
  contain: strict;
}

.logs-table .p-datatable-thead > tr > th {
  background-color: white !important;
  position: sticky !important;
  top: 0 !important;
  z-index: 2 !important;
  border-bottom: 1px solid #e5e7eb;
}

.logs-table .p-datatable-tbody > tr {
  transition: background-color 0.15s ease;
  will-change: transform;
  contain: content;
}

.logs-table .p-datatable-tbody > tr:nth-child(even) {
  background-color: rgb(249, 250, 251);
}

.logs-table .p-datatable-tbody > tr:hover {
  background-color: rgb(243, 244, 246);
  transform: translateZ(0);
}

/* Skeleton animation */
@keyframes shimmer {
  0% {
    background-position: -200% 0;
  }
  100% {
    background-position: 200% 0;
  }
}

.p-skeleton {
  background: linear-gradient(
    90deg,
    rgba(226, 232, 240, 0.6) 25%,
    rgba(226, 232, 240, 0.8) 37%,
    rgba(226, 232, 240, 0.6) 63%
  );
  background-size: 200% 100%;
  animation: shimmer 1.5s infinite;
}

/* Drawer styles */
:deep(.p-drawer) {
  width: min(85vw, 960px) !important;
  max-width: none !important;
}

:deep(.p-drawer-content) {
  width: 100% !important;
}
</style>
