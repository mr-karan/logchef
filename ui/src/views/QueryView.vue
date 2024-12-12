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

const logs = ref<Log[]>([])
const loading = ref(false)
const sourcesLoading = ref(true)
const error = ref<string | null>(null)
const route = useRoute()
const router = useRouter()

// Define props for initial values
const props = defineProps<{
  sourceId?: string
  initialStartTime?: string
  initialEndTime?: string
  initialSearchQuery?: string
  initialSeverity?: string
}>()

// Initialize refs with props or defaults
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

// Create computed property for URL parameters
const queryParams = computed(() => ({
  source: sourceId.value,
  start_time: startDate.value?.toISOString(),
  end_time: endDate.value?.toISOString()
}))

// Update URL when parameters change
watch([sourceId, startDate, endDate], () => {
  // Only update if values have changed from URL params
  if (
    sourceId.value !== props.sourceId ||
    startDate.value.toISOString() !== props.initialStartTime ||
    endDate.value.toISOString() !== props.initialEndTime
  ) {
    router.replace({
      query: {
        ...route.query,
        ...queryParams.value
      }
    })
  }
})

const sources = ref<Source[]>([])
const offset = ref(0)
const toast = useToast()
const rowsPerPageOptions = [50, 100, 500, 1000]
const limit = ref(50)
const hasMore = ref(true)
const totalRecords = ref(0)
const lazyState = ref({
  first: 0,
  rows: limit.value
})

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

const loadLogs = async (event?: any) => {
  if (!sourceId.value) {
    error.value = 'No source selected'
    return
  }

  try {
    loading.value = true
    const pageNumber = event?.page ?? 0
    const rows = event?.rows ?? limit.value
    const offset = pageNumber * rows

    if (!startDate.value || !endDate.value) {
      throw new Error('Start and end dates are required')
    }

    const params = {
      offset,
      limit: rows,
      start_time: startDate.value.toISOString(),
      end_time: endDate.value.toISOString()
    }

    // Update URL with current parameters
    router.replace({
      query: {
        ...route.query,
        ...queryParams.value
      }
    })

    const data = await api.fetchLogs(sourceId.value, params)

    if (data) {
      logs.value = []
      totalRecords.value = data.total_count || 0
      hasMore.value = data.has_more

      if (data.chunks && data.chunks.length > 1) {
        isProgressiveLoading.value = true
        progressPercentage.value = 0

        for (let i = 0; i < data.chunks.length; i++) {
          const chunk = data.chunks[i]
          await nextTick()
          logs.value.push(...chunk)
          progressPercentage.value = Math.round(((i + 1) / data.chunks.length) * 100)
        }

        isProgressiveLoading.value = false
        progressPercentage.value = 0
      } else {
        logs.value = data.logs || []
      }
    }
    error.value = null
  } catch (err) {
    console.error('Error fetching logs:', err)
    error.value = err instanceof Error ? err.message : 'Failed to fetch logs'
    logs.value = []
  } finally {
    loading.value = false
  }
}

const resetLogs = async () => {
  lazyState.value = {
    first: 0,
    rows: limit.value
  }
  await loadLogs({
    first: 0,
    rows: limit.value
  })
}

// Initial fetch of sources to get the first sourceId
async function fetchSources() {
  try {
    sourcesLoading.value = true
    const sourcesData = await api.fetchSources()
    if (sourcesData && sourcesData.length > 0) {
      sources.value = sourcesData
      sourceId.value = sourcesData[0].ID
      await resetLogs()
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

// Modified onMounted to handle initial URL parameters
onMounted(async () => {
  await fetchSources()

  // If we have sourceId in URL, use it and load logs
  if (props.sourceId) {
    sourceId.value = props.sourceId
    await resetLogs()
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

const visibleColumns = ref<string[]>(['timestamp', 'severity_text', 'body'])

const sortedVisibleColumns = computed(() => {
    const columns = [...visibleColumns.value]
    // If timestamp exists but is not first, move it to front
    const timestampIndex = columns.indexOf('timestamp')
    if (timestampIndex > 0) {
        columns.splice(timestampIndex, 1)
        columns.unshift('timestamp')
    } else if (timestampIndex === -1) {
        // If timestamp doesn't exist, add it to front
        columns.unshift('timestamp')
    }
    return columns
})

const updateVisibleColumns = (fields: string[]) => {
    visibleColumns.value = fields
}

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

// Add this function to handle page changes
const handlePageChange = async (event: any) => {
    scrollToTop()
    await loadLogs(event)
}

const tableRef = ref()

// Update the watch to use the component instance
watch(tableRef, (newRef) => {
    if (newRef) {
        setTableWrapper(newRef)
    }
}, { flush: 'post' }) // Add flush: 'post' to ensure DOM is updated

// Add these methods for pagination
const prevPage = () => {
  if (lazyState.value.first > 0) {
    lazyState.value.first -= lazyState.value.rows
    loadLogs()
  }
}

const nextPage = () => {
  if (hasMore.value) {
    lazyState.value.first += lazyState.value.rows
    loadLogs()
  }
}
</script>

<template>
  <div class="h-screen flex">
    <!-- Left Sidebar -->
    <div class="w-64 border-r border-gray-200 bg-white flex-shrink-0">
      <LogSchemaSidebar
        :sourceId="sourceId"
        :startTime="startDate"
        :endTime="endDate"
        @field-toggle="updateVisibleColumns"
      />
    </div>

    <!-- Main Content Area -->
    <div class="flex-1 flex flex-col h-screen">
      <!-- Top Controls Area -->
      <div class="flex-shrink-0 bg-white border-b border-gray-200 p-4">
        <div class="flex items-center justify-between mb-4">
          <div class="flex items-center gap-4">
            <Select
              v-model="sourceId"
              :options="sources"
              optionLabel="Name"
              optionValue="ID"
              placeholder="Select a source"
              class="w-[200px]"
              @change="resetLogs"
            />
            <DateRangeFilter
              v-model:startDate="startDate"
              v-model:endDate="endDate"
              @update:dates="resetLogs"
            />
          </div>
          <Button
            icon="pi pi-share-alt"
            severity="secondary"
            rounded
            aria-label="Share Query"
            @click="copyShareableURL"
          />
        </div>

        <!-- Compact Paginator in Header -->
        <div class="flex items-center justify-between">
          <div class="text-sm text-gray-600">
            {{ totalRecords }} logs found
          </div>
          <div class="flex items-center gap-2">
            <span class="text-sm text-gray-600">Show:</span>
            <Select
              v-model="lazyState.rows"
              :options="rowsPerPageOptions"
              @change="resetLogs"
            />
            <div class="flex items-center gap-1">
              <Button
                icon="pi pi-angle-left"
                text
                :disabled="lazyState.first === 0"
                @click="prevPage"
              />
              <span class="text-sm text-gray-600">
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

        <!-- Error and Progress Messages -->
        <div v-if="error" class="mt-4 p-3 bg-red-50 text-red-600 rounded-md text-sm">
          {{ error }}
        </div>
        <div v-if="isProgressiveLoading" class="mt-4">
          <div class="text-sm text-gray-600 mb-2">
            Loading large result set... {{ progressPercentage }}%
          </div>
          <div class="w-full bg-gray-200 rounded-full h-1.5">
            <div
              class="bg-blue-600 h-1.5 rounded-full transition-all duration-200"
              :style="{ width: `${progressPercentage}%` }"
            ></div>
          </div>
        </div>
      </div>

      <!-- Main Scrollable Content -->
      <div class="flex-1 overflow-auto">
        <DataTable
          v-if="logs.length > 0"
          :value="logs"
          :loading="loading"
          :scrollable="false"
          selectionMode="single"
          @row-click="showLogDetails"
          :resizableColumns="true"
          columnResizeMode="fit"
          class="logs-table"
          v-bind:pt="{
            wrapper: { class: 'min-w-full' },
            table: { class: 'text-sm border-t border-gray-200 min-w-full' },
            bodyCell: { class: ['p-1.5 border-b border-gray-100'] },
            headerCell: { class: ['p-2 bg-white border-b border-gray-200 font-medium text-gray-700 sticky top-0'] },
            bodyRow: { class: ['hover:bg-blue-50/50 cursor-pointer transition-colors duration-100'] }
          }"
        >
          <Column v-for="field in sortedVisibleColumns"
                  :key="field"
                  :field="field"
                  :header="formatColumnHeader(field)"
                  :style="getColumnStyle(field)"
                  :resizeable="true"
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
      class="w-full md:w-[800px] drawer-wide"
      :pt="{
        root: 'border-l border-gray-200',
        header: 'bg-gray-50 px-6 py-4 border-b border-gray-200',
        content: 'p-6'
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
.logs-table {
  border-collapse: collapse;
  width: 100%;
}

/* Remove all DataTable specific scroll styles */
.p-datatable {
  width: 100%;
}

.p-datatable .p-datatable-wrapper {
  overflow: visible !important;
}

/* Improve dropdown appearance */
.p-dropdown {
  @apply border border-gray-300 rounded-md text-sm;
}

.p-dropdown:not(.p-disabled):hover {
  @apply border-gray-400;
}

/* Loading overlay */
.p-datatable-loading-overlay {
  background: rgba(255, 255, 255, 0.8);
  backdrop-filter: blur(2px);
}
</style>
