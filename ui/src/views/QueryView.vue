<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, watch, computed } from 'vue'
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

// Move state declarations to the top
const logs = ref<Log[]>([])
const loading = ref(false)
const sourcesLoading = ref(true)
const error = ref<string | null>(null)
const route = useRoute()
const router = useRouter()
const sources = ref<Source[]>([])
const rowsPerPageOptions = [50, 100, 500, 1000]
const limit = ref(50)
const hasMore = ref(true)
const totalRecords = ref(0)
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
const tableLoading = ref(false)
const initialLoad = ref(true) // Track first load to show skeleton

// Now define loadLogs and resetLogs
const loadLogs = async () => {
  if (!sourceId.value) {
    error.value = 'No source selected'
    return
  }

  tableLoading.value = true
  error.value = null

  try {
    const response = await api.getLogs(sourceId.value, {
      start_time: startDate.value?.toISOString(),
      end_time: endDate.value?.toISOString(),
      offset: lazyState.value.first,
      limit: lazyState.value.rows,
      search_query: '',
      severity_text: ''
    })

    logs.value = response.logs || []
    totalRecords.value = response.total_count || 0
    hasMore.value = response.has_more || false
  } catch (err) {
    console.error('Error fetching logs:', err)
    error.value = err instanceof Error ? err.message : 'Failed to fetch logs'
    logs.value = []
  } finally {
    tableLoading.value = false
    initialLoad.value = false
  }
}

const resetLogs = async () => {
  lazyState.value = {
    first: 0,
    rows: lazyState.value.rows
  }
  await loadLogs()
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
  if (!sourceId.value) return

  try {
    loading.value = true
    tableLoading.value = true
    error.value = null

    const controller = new AbortController()
    const signal = controller.signal

    const [logsResponse, schemaResponse] = await Promise.all([
      api.getLogs(sourceId.value, {
        start_time: startDate.value?.toISOString(),
        end_time: endDate.value?.toISOString(),
        offset: lazyState.value.first,
        limit: lazyState.value.rows,
        search_query: '',
        severity_text: ''
      }, signal),
      api.getLogSchema(sourceId.value, {
        start_time: startDate.value?.toISOString(),
        end_time: endDate.value?.toISOString()
      }, signal)
    ])

    if (!signal.aborted) {
      logs.value = logsResponse.logs || []
      totalRecords.value = logsResponse.total_count || 0
      hasMore.value = logsResponse.has_more || false
      schema.value = schemaResponse || []
    }
  } catch (err) {
    if (err.name !== 'AbortError') {
      console.error('Error fetching data:', err)
      error.value = err instanceof Error ? err.message : 'Failed to fetch data'
      logs.value = []
      schema.value = []
    }
  } finally {
    loading.value = false
    tableLoading.value = false
    initialLoad.value = false
  }
}

// Initialize data on mount
onMounted(async () => {
  await fetchSources()

  if (!sourceId.value && sources.value.length > 0) {
    sourceId.value = sources.value[0].ID
  }

  if (sourceId.value) {
    startDate.value = props.initialStartTime
      ? new Date(props.initialStartTime)
      : new Date(Date.now() - 60 * 60 * 1000)
    endDate.value = props.initialEndTime
      ? new Date(props.initialEndTime)
      : new Date()

    await fetchLogsAndSchema()
  }
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
  if (!dt.value || !logs.value.length) return

  // Get all selected columns
  const columnsToExport = selectedColumns.value.map(col => ({
    field: col.field,
    header: formatColumnHeader(col.field)
  }))

  // Prepare CSV data
  const csvData = logs.value.map(log => {
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
                    :disabled="tableLoading"
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
                    @fetch="fetchLogsAndSchema"
                    class="w-full"
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
              </div>
            </div>
          </div>
        </div>

        <!-- Secondary Controls: Pagination and Actions -->
        <div class="px-4 py-2 bg-gray-50/50 flex items-center justify-between">
          <div class="flex items-center gap-3">
            <span class="text-sm text-gray-600">
              <span v-if="tableLoading">Loading logs...</span>
              <span v-else>{{ totalRecords }} logs found</span>
            </span>
            <div class="h-4 w-px bg-gray-200"></div>
            <Button
              icon="pi pi-download"
              severity="secondary"
              text
              @click="exportLogs"
              v-tooltip.bottom="'Export as CSV'"
              :disabled="!logs.length"
            />
          </div>

          <!-- Pagination Controls -->
          <div class="flex items-center gap-3">
            <span class="text-sm text-gray-600">Show:</span>
            <Select
              v-model="lazyState.rows"
              :options="rowsPerPageOptions"
              class="w-[100px]"
              @change="resetLogs"
            />
            <div class="flex items-center gap-1">
              <Button
                icon="pi pi-angle-left"
                text
                :disabled="lazyState.first === 0"
                @click="prevPage"
              />
              <span class="text-sm text-gray-600 min-w-[80px] text-center">
                {{ Math.floor(lazyState.first / lazyState.rows) + 1 }} of {{ Math.ceil(totalRecords / lazyState.rows) }}
              </span>
              <Button
                icon="pi pi-angle-right"
                text
                :disabled="!hasMore"
                @click="nextPage"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- Rest of the DataTable code remains the same -->
      <div class="flex-1 overflow-y-auto">
        <!-- Loading skeleton for initial load -->
        <div v-if="initialLoad" class="p-4 space-y-4">
          <div v-for="i in 5" :key="i" class="animate-pulse space-y-2">
            <div class="h-10 bg-gray-100 rounded-md"></div>
          </div>
        </div>

        <!-- Error state -->
        <div v-else-if="error" class="p-8 text-center">
          <div class="text-red-500 mb-2">{{ error }}</div>
          <Button label="Retry" severity="secondary" @click="fetchLogsAndSchema" />
        </div>

        <!-- Empty state -->
        <div v-else-if="!logs.length && !tableLoading" class="p-8 text-center">
          <div class="text-gray-500 mb-2">No logs found for the selected time range</div>
          <Button label="Change Filters" severity="secondary" @click="() => {}" />
        </div>

        <!-- DataTable with loading overlay -->
        <DataTable
          v-else
          ref="dt"
          :value="logs"
          :loading="tableLoading"
          class="logs-table"
          selectionMode="single"
          @row-click="showLogDetails"
          :resizableColumns="true"
          columnResizeMode="fit"
          scrollable
          scrollHeight="calc(100vh - 140px)"
          tableStyle="width: 100%"
          showGridlines
          v-bind:pt="{
            root: { class: 'h-full' },
            wrapper: { class: 'h-full' },
            table: { class: 'text-sm border-t border-gray-200' },
            bodyCell: { class: ['p-1.5 border-b border-gray-100'] },
            headerCell: {
              class: [
                'p-2 bg-white border-b border-gray-200 font-medium text-gray-700',
                'sticky top-0 z-20'
              ]
            },
            bodyRow: {
              class: ['hover:bg-blue-50/50 cursor-pointer transition-colors duration-100']
            },
            loadingOverlay: {
              class: [
                'absolute inset-0 bg-white/80 backdrop-blur-sm transition-opacity',
                'flex items-center justify-center z-50'
              ]
            }
          }"
        >
          <!-- Loading overlay slot -->
          <template #loading>
            <div class="flex flex-col items-center">
              <i class="pi pi-spin pi-spinner text-blue-500 text-2xl mb-2"></i>
              <span class="text-sm text-gray-600">Loading logs...</span>
            </div>
          </template>

          <!-- Column definitions -->
          <Column v-for="field in sortedVisibleColumns"
                  :key="field"
                  :field="field"
                  :header="formatColumnHeader(field)"
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
/* Update scrollHeight calculation in styles */
.logs-table {
  height: 100%;
}

/* Clean up styles */
.logs-table {
  height: 100%;
}

/* Remove all wrapper scrolling */
.logs-table .p-datatable-wrapper {
  border: none;
}

/* Ensure header is properly positioned */
.logs-table .p-datatable-thead > tr > th {
  background-color: white !important;
  position: sticky !important;
  top: 0 !important;
  z-index: 2 !important;
}

/* Scrollable table styles */
.logs-table .p-datatable-wrapper {
  height: 100%;
  border-radius: 6px;
}

/* Loading state */
.p-datatable-loading-overlay {
  background: rgba(255, 255, 255, 0.8);
  backdrop-filter: blur(2px);
}

/* Skeleton animation */
@keyframes pulse {
  0%, 100% { opacity: 0.5; }
  50% { opacity: 1; }
}

.animate-pulse {
  animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
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
