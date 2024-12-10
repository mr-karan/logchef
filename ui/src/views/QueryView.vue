<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, watch } from 'vue'
import DateRangeFilter from '@/components/DateRangeFilter.vue'
import { api } from '@/services/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import Toast from 'primevue/toast'
import Drawer from 'primevue/drawer'
import type { Log, LogResponse } from '@/types/logs'
import type { Source } from '@/types/source'
import Dropdown from 'primevue/dropdown'
import FloatLabel from 'primevue/floatlabel'

const logs = ref<Log[]>([])
const loading = ref(false)
const sourcesLoading = ref(true)
const error = ref<string | null>(null)
const route = useRoute()
const sourceId = ref<string | undefined>(route.params.id?.toString())
const showSourceSelector = ref(!route.params.id)
const sources = ref<Source[]>([])
const offset = ref(0)
const toast = useToast()
const limit = ref(50)
const hasMore = ref(true)
const startDate = ref(null)
const endDate = ref(null)

const selectedLog = ref(null)
const drawerVisible = ref(false)

const showLogDetails = (log) => {
  selectedLog.value = log
  drawerVisible.value = true
}

const copyToClipboard = async (data) => {
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

async function fetchLogs(reset = false) {
  if (!sourceId.value) {
    error.value = 'No source selected'
    return
  }

  if (reset) {
    logs.value = []
    offset.value = 0
    hasMore.value = true
  }

  if (loading.value || (!hasMore.value && !reset)) return

  try {
    loading.value = true
    const scrollPosition = window.scrollY

    const data = await api.fetchLogs(sourceId.value, {
      offset: offset.value,
      limit: limit.value,
      start_time: startDate.value,
      end_time: endDate.value
    })

    if (data) {
      const newLogs = data.logs || []
      logs.value = reset ? newLogs : [...logs.value, ...newLogs]
      hasMore.value = data.has_more || false
      if (newLogs.length > 0) {
        offset.value += newLogs.length
      }

      nextTick(() => {
        window.scrollTo(0, scrollPosition)
      })
    }
    error.value = null
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to fetch logs'
    if (reset) logs.value = []
  } finally {
    loading.value = false
  }
}

// Initial fetch of sources to get the first sourceId
async function fetchSources() {
  try {
    sourcesLoading.value = true
    const sourcesData = await api.fetchSources()
    if (sourcesData && sourcesData.length > 0) {
      sources.value = sourcesData
      sourceId.value = sourcesData[0].ID
      await fetchLogs()
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

const handleScroll = () => {
  const scrollPosition = window.scrollY + window.innerHeight
  const scrollHeight = document.documentElement.scrollHeight

  if (!loading.value && hasMore.value && (scrollHeight - scrollPosition < 50)) {
    fetchLogs()
  }
}

onMounted(() => {
  fetchSources()
  window.addEventListener('scroll', handleScroll)
})

onUnmounted(() => {
  window.removeEventListener('scroll', handleScroll)
})

watch(sourceId, () => {
  fetchLogs(true)
})
</script>

<template>
  <div class="py-4 px-6">
    <Toast />
    <div class="flex justify-between items-center mb-6">
      <div>
        <h1 class="text-lg font-semibold text-gray-900">Query Explorer</h1>
        <p class="text-xs text-gray-500 mt-0.5">Search and analyze your logs across all sources</p>
      </div>
      <div class="flex items-center gap-4">
        <FloatLabel v-if="showSourceSelector">
          <Dropdown
            v-model="sourceId"
            :options="sources"
            optionLabel="Name"
            optionValue="ID"
            placeholder="Type or select a source"
            class="w-[220px]"
            :pt="{
              root: 'text-sm',
              input: 'text-sm py-1.5 h-8',
              panel: { root: 'text-sm' },
              item: 'text-sm py-1.5',
              filterInput: 'text-sm'
            }"
          />
          <label for="sourceId" class="text-[11px]">Source</label>
        </FloatLabel>
        <DateRangeFilter
          v-model:startDate="startDate"
          v-model:endDate="endDate"
          class="flex-shrink-0"
        />
      </div>
    </div>

    <div v-if="error" class="mb-4 p-3 bg-red-50 text-red-600 rounded-md text-sm">
      {{ error }}
    </div>

    <!-- Sticky logs count -->
    <div v-if="logs.length > 0" class="sticky top-0 z-10 bg-white/80 backdrop-blur-sm py-2 border-b mb-2 flex justify-between items-center px-4">
      <span class="text-xs text-gray-500">
        {{ logs.length }} logs displayed
      </span>
      <span v-if="loading" class="text-xs text-gray-500 flex items-center gap-2">
        <i class="pi pi-spinner animate-spin"></i>
        Loading more...
      </span>
    </div>

    <!-- Logs table -->
    <DataTable
      v-if="logs.length > 0"
      :value="logs"
      :loading="loading"
      class="p-datatable-sm"
      stripedRows
      showGridlines
      tableStyle="min-width: 50rem"
      size="small"
      v-bind:pt="{
        wrapper: 'border rounded-lg',
        table: 'text-xs leading-tight',
        headerCell: 'py-2 px-2 bg-gray-50 text-gray-700 font-medium',
        bodyCell: 'py-1 px-2',
      }"
    >
      <Column field="timestamp" header="Timestamp" sortable style="width: 200px">
        <template #body="{ data }">
          {{ new Date(data.timestamp).toLocaleString() }}
        </template>
      </Column>

      <Column field="severity_text" header="Severity" sortable style="width: 120px">
        <template #body="{ data }">
          <Tag
            :value="data.severity_text.toLowerCase()"
            :severity="getSeverityType(data.severity_text)"
            rounded
            :pt="{
              root: 'text-[10px] px-1.5 py-0.5 font-medium opacity-90',
              value: 'text-[10px] tracking-wide uppercase'
            }"
          />
        </template>
      </Column>

      <Column field="service_name" header="Service" sortable style="width: 150px" />
      <Column field="namespace" header="Namespace" sortable style="width: 150px" />

      <Column field="body" header="Message" style="min-width: 400px">
        <template #body="{ data }">
          <div class="relative cursor-pointer group" @click="showLogDetails(data)">
            <!-- Message Content -->
            <div class="flex items-center gap-2 py-1">
              <div class="flex-1 whitespace-pre-wrap">{{ data.body }}</div>
              <div class="flex items-center gap-1">
                <div class="text-xs text-gray-400 px-2 py-1 rounded-md invisible group-hover:visible">
                  <i class="pi pi-external-link text-xs"></i>
                </div>
              </div>
            </div>
          </div>
        </template>
      </Column>
    </DataTable>

    <!-- Log Details Drawer -->
    <Drawer 
      v-model:visible="drawerVisible"
      position="right"
      :modal="true"
      :dismissable="true"
      :closable="true"
      class="w-full md:w-[600px]"
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
          >
            <i class="pi pi-copy"></i>
          </button>
        </div>
      </template>

      <div v-if="selectedLog" class="space-y-6">
        <!-- Metadata Section -->
        <div class="space-y-4">
          <div class="grid grid-cols-2 gap-4">
            <div>
              <label class="block text-xs font-medium text-gray-500 mb-1">Timestamp</label>
              <div class="text-sm">{{ new Date(selectedLog.timestamp).toLocaleString() }}</div>
            </div>
            <div>
              <label class="block text-xs font-medium text-gray-500 mb-1">Severity</label>
              <Tag
                :value="selectedLog.severity_text.toLowerCase()"
                :severity="getSeverityType(selectedLog.severity_text)"
                rounded
              />
            </div>
            <div>
              <label class="block text-xs font-medium text-gray-500 mb-1">Service</label>
              <div class="text-sm">{{ selectedLog.service_name }}</div>
            </div>
            <div>
              <label class="block text-xs font-medium text-gray-500 mb-1">Namespace</label>
              <div class="text-sm">{{ selectedLog.namespace }}</div>
            </div>
          </div>
        </div>

        <!-- Message Section -->
        <div class="space-y-2">
          <label class="block text-xs font-medium text-gray-500">Message</label>
          <div class="text-sm whitespace-pre-wrap p-3 bg-gray-50 rounded-md">{{ selectedLog.body }}</div>
        </div>

        <!-- Raw JSON Section -->
        <div class="space-y-2">
          <label class="block text-xs font-medium text-gray-500">Raw JSON</label>
          <pre class="text-xs font-mono bg-gray-50 p-3 rounded-md overflow-x-auto">{{ JSON.stringify(selectedLog, null, 2) }}</pre>
        </div>
      </div>
    </Drawer>

    <!-- Loading more indicator -->
    <div v-if="loading" class="text-center py-4">
      Loading more logs...
    </div>

    <!-- No more logs message -->
    <div v-if="!loading && !hasMore" class="text-center py-4 text-muted-foreground">
      No more logs to load
    </div>
  </div>
</template>
