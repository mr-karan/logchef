<template>
  <n-card class="explore-view">
    <n-space vertical :size="24" style="height: 100%">
      <!-- Header Controls -->
      <n-space justify="space-between" align="center" :size="16">
        <!-- Left side: Source and Refresh -->
        <n-space align="center" :size="12">
          <n-space vertical :size="4">
            <n-text class="input-label">Source</n-text>
            <n-select v-model:value="selectedSource" :options="sourceOptions" placeholder="Select a source"
              :loading="loading" size="medium" style="width: 240px" />
          </n-space>

          <n-space vertical :size="4">
            <n-text class="input-label">Fetch Limit</n-text>
            <n-input-number v-model:value="fetchLimit" :min="1" :max="10000" :step="100" size="medium"
              style="width: 120px" @update:value="handleLimitChange" />
          </n-space>

          <n-space vertical :size="4">
            <n-text class="input-label">Refresh</n-text>
            <n-space :size="4">
              <n-button size="medium" @click="loadLogs" secondary>
                <template #icon>
                  <n-icon><refresh-icon /></n-icon>
                </template>
                Refresh
              </n-button>
              <n-dropdown trigger="click" :options="autoRefreshOptions" @select="handleAutoRefreshSelect">
                <n-button size="medium" :type="autoRefreshEnabled ? 'primary' : 'default'" ghost>
                  <template #icon>
                    <n-icon :class="{ 'spin-icon': autoRefreshEnabled }">
                      <sync-icon />
                    </n-icon>
                  </template>
                  {{ autoRefreshDisplay }}
                </n-button>
              </n-dropdown>
            </n-space>
          </n-space>
        </n-space>

        <!-- Right side: Time Range and Settings -->
        <n-space align="center" :size="12">
          <time-range-selector v-model="timeRange" size="medium" />
          <n-button @click="showTableSettings = true" secondary circle>
            <template #icon>
              <n-icon><settings-icon /></n-icon>
            </template>
          </n-button>
        </n-space>
      </n-space>

      <n-divider style="margin: 0" />

      <!-- Main Content -->
      <n-spin :show="loading" style="flex: 1; height: 100%; min-height: 0">
        <div v-if="!selectedSource" class="empty-state">
          <n-empty description="Select a source to explore logs">
            <template #icon>
              <n-icon size="48" color="#ccc">
                <analytics-outline />
              </n-icon>
            </template>
          </n-empty>
        </div>
        <div v-else class="table-container">
          <!-- Move pagination controls here, above the table -->
          <div class="table-controls">
            <n-space justify="space-between" align="center">
              <!-- Left: Stats -->
              <n-space align="center" :size="16">
                <n-text class="stats-text">{{ logs.length.toLocaleString() }} logs</n-text>
                <n-text class="stats-text">({{ queryTime }}ms)</n-text>
              </n-space>

              <!-- Right: Pagination -->
              <n-pagination v-model:page="currentPage" v-model:page-size="pageSize"
                :page-count="Math.ceil(logs.length / pageSize)" :page-sizes="pageSizeOptions"
                @update:page="handlePageChange" @update:page-size="handlePageSizeChange">
                <template #suffix>
                  <n-text style="margin: 0 8px">Rows per page:</n-text>
                  <n-select v-model:value="pageSize" :options="pageSizeOptions" size="small" style="width: 100px" />
                </template>
              </n-pagination>
            </n-space>
          </div>

          <!-- Table comes after the controls -->
          <log-table :data="displayedLogs" :loading="logsLoading" :columns="availableColumns"
            v-model:visible-column-keys="visibleColumns" :severity-colors="severityColors" class="log-analytics-table"
            @row-click="showLogDetails" />

          <!-- Log Details Drawer -->
          <n-drawer v-model:show="showDrawer" :width="500" placement="right">
            <n-drawer-content closable>
              <template #header>
                <n-space align="center" justify="space-between">
                  <n-space vertical size="small">
                    <n-text class="drawer-title">Log Details</n-text>
                    <n-text depth="3" size="small">
                      {{ formatTimestamp(selectedLog?.timestamp) }}
                    </n-text>
                  </n-space>
                  <n-button size="small" @click="copyLogToClipboard" secondary>
                    <template #icon>
                      <n-icon><copy-icon /></n-icon>
                    </template>
                    Copy JSON
                  </n-button>
                </n-space>
              </template>
              <n-code :code="JSON.stringify(selectedLog, null, 2)" language="json" :hljs="hljs" />
            </n-drawer-content>
          </n-drawer>
        </div>
      </n-spin>
    </n-space>

    <!-- Add Table Settings Modal -->
    <n-modal v-model:show="showTableSettings" preset="card" style="width: 600px" title="Table Settings">
      <n-space vertical :size="16">
        <n-space justify="space-between" align="center">
          <n-text>Select columns to display:</n-text>
          <n-space :size="8">
            <n-button size="small" @click="selectAllColumns" secondary>
              Check All
            </n-button>
            <n-button size="small" @click="unselectAllColumns" secondary>
              Uncheck All
            </n-button>
          </n-space>
        </n-space>
        <n-checkbox-group v-model:value="visibleColumns">
          <n-space vertical>
            <n-checkbox v-for="col in availableColumns" :key="col.key" :value="col.key" :label="col.title"
              :disabled="col.required">
              {{ col.title }}
              <n-text v-if="col.type" depth="3" style="margin-left: 8px">({{ col.type }})</n-text>
            </n-checkbox>
          </n-space>
        </n-checkbox-group>
      </n-space>
    </n-modal>
  </n-card>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { AnalyticsOutline, Sync as SyncIcon, Refresh as RefreshIcon, Settings as SettingsIcon, Copy as CopyIcon } from '@vicons/ionicons5'
import hljs from 'highlight.js'
import type { Log, LogQueryParams, LogResponse } from '@/api/types'
import TimeRangeSelector from '@/components/TimeRangeSelector.vue'
import LogTable from '@/components/LogTable.vue'
import sourcesApi from '@/api/sources'
import logsApi from '@/api/logs'
import type { Source } from '@/api/types'
import { useMessage } from 'naive-ui'
import { format } from 'date-fns'

// State
const loading = ref(false)
const logsLoading = ref(false)
const isLoadingLogs = ref(false)
const lastLoadTime = ref(0) // Track last successful load time
const minLoadInterval = 2000 // Minimum time between loads in ms
const sources = ref<Source[]>([])
const selectedSource = ref<string | null>(null)
const sourceDetails = ref<Source | null>(null)
const logs = ref<Log[]>([])
const showDrawer = ref(false)
const selectedLog = ref<Log | null>(null)
const timeRange = ref<[number, number]>([Date.now() - 3 * 60 * 60 * 1000, Date.now()])
const autoRefreshEnabled = ref(false)
const refreshInterval = ref<ReturnType<typeof setInterval>>()
const visibleColumns = ref<string[]>(['timestamp', 'body'])
const currentPage = ref(1)
const pageSize = ref(50)
const pageSizes = [10, 20, 50, 100, 200]
const pageSizeOptions = pageSizes.map(size => ({
  label: `${size} rows`,
  value: size
}))
const fetchLimit = ref(100) // Default fetch limit of 100
const showTableSettings = ref(false)
const autoRefreshInterval = ref<number>(0)
const message = useMessage()
const queryTime = ref<number>(0)
const timeWindowSize = ref<number>(300000) // Default 5 minutes

// Constants
const REQUIRED_COLUMNS = ['timestamp', 'body']
const severityColors = {
  error: 'rgba(255,77,79,0.1)',
  warn: 'rgba(250,173,20,0.1)',
  info: 'rgba(24,144,255,0.1)'
}

// Auto refresh options
const autoRefreshOptions = [
  { label: 'Off', key: 0 },
  { label: 'Last 5s', key: 5000 },
  { label: 'Last 15s', key: 15000 },
  { label: 'Last 30s', key: 30000 },
  { label: 'Last 1m', key: 60000 },
  { label: 'Last 5m', key: 300000 },
  { label: 'Last 15m', key: 900000 },
  { label: 'Last 30m', key: 1800000 },
  { label: 'Last 1h', key: 3600000 }
]

// Computed
const sourceOptions = computed(() =>
  sources.value.map(source => ({
    label: `${source.connection.database}.${source.connection.table_name}`,
    value: source.id
  }))
)

const availableColumns = computed(() => {
  if (!sourceDetails.value?.columns) return []

  // Start with required columns
  const required = REQUIRED_COLUMNS.map(colName => {
    const col = sourceDetails.value!.columns.find(c => c.name === colName)
    return {
      key: colName,
      title: colName.replace(/_/g, ' ').replace(/\b\w/g, l => l.toUpperCase()),
      width: getColumnWidth(colName),
      required: true,
      type: col?.type
    }
  })

  // Add all other columns
  const others = sourceDetails.value.columns
    .filter(col => !REQUIRED_COLUMNS.includes(col.name))
    .map(col => ({
      key: col.name,
      title: col.name.replace(/_/g, ' ').replace(/\b\w/g, l => l.toUpperCase()),
      width: getColumnWidth(col.name),
      required: false,
      type: col.type
    }))

  return [...required, ...others]
})

const tableColumns = computed(() => {
  if (!sourceDetails.value?.columns) return []

  // Start with required columns
  const requiredColumns = REQUIRED_COLUMNS
    .map(colName => {
      const col = sourceDetails.value!.columns.find(c => c.name === colName)
      return {
        key: colName,
        title: col?.name.replace(/_/g, ' ').replace(/\b\w/g, l => l.toUpperCase()) || colName,
        width: getColumnWidth(colName),
        required: true
      }
    })

  // Get all other visible columns that aren't required
  const otherVisibleColumns = visibleColumns.value
    .filter(colName => !REQUIRED_COLUMNS.includes(colName))
    .map(colName => {
      const col = sourceDetails.value!.columns.find(c => c.name === colName)
      return {
        key: colName,
        title: col?.name.replace(/_/g, ' ').replace(/\b\w/g, l => l.toUpperCase()) || colName,
        width: getColumnWidth(colName),
        required: false
      }
    })

  // Combine required columns first, then other visible columns
  return [...requiredColumns, ...otherVisibleColumns]
})

const displayedLogs = computed(() => {
  if (!logs.value) return []
  const start = (currentPage.value - 1) * pageSize.value
  const end = start + pageSize.value
  return logs.value.slice(start, end)
})

// Methods
async function loadSources() {
  try {
    loading.value = true
    sources.value = await sourcesApi.listSources()
    if (sources.value.length > 0) {
      selectedSource.value = sources.value[0].id
    }
  } catch (error) {
    console.error('Failed to load sources:', error)
  } finally {
    loading.value = false
  }
}

async function loadSourceDetails(id: string) {
  try {
    loading.value = true
    const source = await sourcesApi.getSource(id)

    // Use the columns from the source object directly
    sourceDetails.value = {
      ...source,
      columns: source.columns // Assign columns to sourceDetails
    }

    // Keep only required columns if no columns are selected
    if (visibleColumns.value.length === 0) {
      visibleColumns.value = [...REQUIRED_COLUMNS]
    } else {
      // Ensure required columns are always present
      const newColumns = new Set(visibleColumns.value)
      REQUIRED_COLUMNS.forEach(col => newColumns.add(col))
      visibleColumns.value = Array.from(newColumns)
    }
    await loadLogs()
  } catch (error) {
    console.error('Failed to load source details:', error)
  } finally {
    loading.value = false
  }
}

async function loadLogs() {
  if (!selectedSource.value || !timeRange.value) return
  
  // Prevent too frequent refreshes
  const now = Date.now()
  if (isLoadingLogs.value || (now - lastLoadTime.value) < minLoadInterval) {
    return
  }

  try {
    isLoadingLogs.value = true
    logsLoading.value = true
    
    const response = await logsApi.getLogs(selectedSource.value, {
      start_timestamp: Math.floor(timeRange.value[0]),
      end_timestamp: Math.floor(timeRange.value[1]),
      limit: fetchLimit.value
    })

    logs.value = response.data.logs || []
    queryTime.value = Number(response.data.stats.execution_time_ms.toFixed(2))
    currentPage.value = 1
    lastLoadTime.value = Date.now()
  } catch (error) {
    console.error('Failed to load logs:', error)
    logs.value = []
    queryTime.value = 0
  } finally {
    logsLoading.value = false
    isLoadingLogs.value = false
  }
}

function clearFilters() {
  timeRange.value = [Date.now() - 3 * 60 * 60 * 1000, Date.now()]
  autoRefreshEnabled.value = false
}

function showLogDetails(log: Log) {
  selectedLog.value = log
  showDrawer.value = true
}

function handleLimitChange(value: number) {
  if (value < 1) {
    fetchLimit.value = 1
    return
  }
  currentPage.value = 1
  loadLogs()
}

function handlePageChange(page: number) {
  currentPage.value = page
}

function handlePageSizeChange(size: number) {
  pageSize.value = size
  currentPage.value = 1 // Reset to first page when changing page size
}

function handleAutoRefreshSelect(key: number) {
  stopAutoRefresh() // Always stop existing interval first
  
  if (key === 0) {
    autoRefreshEnabled.value = false
    return
  }

  timeWindowSize.value = key // Set window size to match interval
  autoRefreshInterval.value = key // Set polling interval
  autoRefreshEnabled.value = true
  startAutoRefresh()
}

function startAutoRefresh() {
  // Clear any existing interval first
  stopAutoRefresh()

  // Initial update and fetch
  updateTimeWindow()
  loadLogs()

  // Set up new interval with the selected interval duration
  // Ensure interval is at least as long as minLoadInterval
  const interval = Math.max(autoRefreshInterval.value, minLoadInterval)
  
  refreshInterval.value = setInterval(() => {
    updateTimeWindow()
    loadLogs()
  }, interval)
}

function stopAutoRefresh() {
  if (refreshInterval.value) {
    clearInterval(refreshInterval.value)
    refreshInterval.value = null
  }
  autoRefreshEnabled.value = false
  autoRefreshInterval.value = 0
}

function updateTimeWindow() {
  const now = Date.now()
  timeRange.value = [now - timeWindowSize.value, now]
}

function formatInterval(ms: number): string {
  if (ms < 60000) return `${ms / 1000}s`
  if (ms < 3600000) return `${ms / 60000}m`
  return `${ms / 3600000}h`
}

function selectAllColumns() {
  const allColumns = availableColumns.value.map(col => col.key)
  visibleColumns.value = allColumns
}

function unselectAllColumns() {
  visibleColumns.value = REQUIRED_COLUMNS
}

async function copyLogToClipboard() {
  if (!selectedLog.value) return

  try {
    await navigator.clipboard.writeText(JSON.stringify(selectedLog.value, null, 2))
    message.success('Copied to clipboard')
  } catch (error) {
    message.error('Failed to copy to clipboard')
  }
}

function formatTimestamp(timestamp: string | undefined): string {
  if (!timestamp) return ''
  return format(new Date(timestamp), 'MMM d, yyyy HH:mm:ss.SSS')
}

// Update the getColumnWidth function to be simpler
function getColumnWidth(columnName: string): number {
  // Use explicit pixel values for known columns
  const explicitWidths: Record<string, number> = {
    timestamp: 160,
    body: 400,
    severity_text: 100,
    level: 80,
    service: 120,
    host: 120,
    pod: 140,
    container: 120,
    namespace: 100,
    message: 400
  }
  
  return explicitWidths[columnName.toLowerCase()] || 120
}

// Update the dropdown button display
const autoRefreshDisplay = computed(() => {
  if (!autoRefreshEnabled.value) return 'Auto Refresh'
  const interval = autoRefreshOptions.find(opt => opt.key === timeWindowSize.value)
  return `Auto (${interval?.label})`
})

// Watchers
watch(selectedSource, async (newSourceId) => {
  if (newSourceId) {
    await loadSourceDetails(newSourceId)
  } else {
    sourceDetails.value = null
    logs.value = []
  }
})

watch(timeRange, () => {
  if (!autoRefreshEnabled.value) {
    loadLogs()
  }
})

// Lifecycle
onMounted(() => {
  loadSources()
})

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

<style scoped>
.explore-view {
  height: calc(100vh - 64px);
  /* Assuming 64px is the header height */
  display: flex;
  flex-direction: column;
}

.empty-state {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}

.table-container {
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
  /* Important for Firefox */
}

.table-controls {
  padding: 8px 16px;
  background: #fff;
  border-bottom: 1px solid #eee;
  display: flex;
  justify-content: flex-end;
}

.pagination-container {
  display: none;
}

.spin-icon {
  animation: spin 1.5s linear infinite;
}

@keyframes spin {
  from {
    transform: rotate(0deg);
  }

  to {
    transform: rotate(360deg);
  }
}

.input-label {
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.stats-text {
  font-size: 13px;
  color: rgba(0, 0, 0, 0.65);
  font-family: 'JetBrains Mono', monospace;
}

.log-analytics-table {
  font-family: 'JetBrains Mono', monospace;
  font-size: 12px;
  line-height: 1.4;
}

.log-analytics-table :deep(td) {
  padding: 4px 8px !important;
}

.log-analytics-table :deep(th) {
  padding: 8px !important;
  font-weight: 600;
  background: rgba(0, 0, 0, 0.02);
}

/* Remove column hover controls since we have table settings */
.log-analytics-table :deep(.table-controls) {
  display: none;
}

.drawer-title {
  font-size: 16px;
  font-weight: 600;
}

:deep(.n-drawer-header__main) {
  flex: 1;
}

:deep(.n-code) {
  font-family: 'JetBrains Mono', monospace;
  font-size: 12px;
}
</style>
