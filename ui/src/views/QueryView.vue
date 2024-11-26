<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { api } from '@/services/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import type { Log, LogResponse } from '@/types/logs'
import type { Source } from '@/types/source'
import Dropdown from 'primevue/dropdown'

const logs = ref<Log[]>([])
const loading = ref(false)
const sourcesLoading = ref(true)
const error = ref<string | null>(null)
const sourceId = ref<string | undefined>(undefined)
const sources = ref<Source[]>([])
const offset = ref(0)
const limit = ref(50)
const hasMore = ref(true)

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
      limit: limit.value
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
  <div class="py-8 px-6">
    <div class="mb-8">
      <h1 class="text-2xl font-bold">Query Explorer</h1>
      <p class="text-sm text-muted-foreground mt-1">Search and analyze your logs across all sources.</p>
    </div>

    <div v-if="error" class="mb-4 p-4 bg-red-50 text-red-600 rounded-md">
      {{ error }}
    </div>

    <!-- Source selector -->
    <div class="mb-4">
      <Dropdown
        v-model="sourceId"
        :options="sources"
        optionLabel="Name"
        optionValue="ID"
        placeholder="Select a source"
        class="w-full"
      />
    </div>

    <!-- Sticky logs count -->
    <div v-if="logs.length > 0" class="sticky top-0 z-10 bg-white py-2 border-b mb-2 flex justify-between items-center">
      <span class="text-sm text-muted-foreground">
        Showing {{ logs.length }} logs
      </span>
      <span v-if="loading" class="text-sm text-muted-foreground">
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
          <div class="whitespace-pre-wrap">{{ data.body }}</div>
        </template>
      </Column>
    </DataTable>

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
