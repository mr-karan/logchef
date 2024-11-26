<script setup>
import { ref, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '@/services/api'
import { useToast } from 'primevue/usetoast'
import Toast from 'primevue/toast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'

const route = useRoute()
const toast = useToast()
const dt = ref()

const logs = ref([])
const loading = ref(false)
const error = ref(null)
const totalRecords = ref(0)
const virtualScrollerOptions = ref({
  itemSize: 51,
  delay: 200,
  showLoader: true,
  loading: loading,
  lazy: true,
  onLazyLoad: loadLogs
})

async function loadLogs(event) {
  try {
    loading.value = true
    const sourceId = route.params.id
    const response = await api.fetchLogs(sourceId, {
      offset: event.first,
      limit: event.rows
    })
    
    if (response.logs) {
      logs.value = response.logs
      totalRecords.value = response.total_count || 0
    }
  } catch (err) {
    error.value = err.message
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to fetch logs',
      life: 3000
    })
  } finally {
    loading.value = false
  }
}

function getSeverityType(severityText) {
  switch (severityText?.toLowerCase()) {
    case 'error': return 'danger'
    case 'warn': return 'warning'
    case 'info': return 'secondary'
    case 'debug': return 'success'
    default: return null
  }
}

function formatTimestamp(value) {
  return new Date(value).toLocaleString()
}

watch(() => route.params.id, () => {
  logs.value = []
  totalRecords.value = 0
  if (dt.value) {
    dt.value.resetVirtualScroll()
  }
})

onMounted(() => {
  // Initial load will be handled by virtual scroller
})
</script>

<template>
  <div class="container py-8">
    <Toast />
    <div class="mb-8">
      <h1 class="text-3xl font-semibold tracking-tight">Logs</h1>
      <p class="text-sm text-muted-foreground mt-1">View and search logs from your sources.</p>
    </div>

    <div v-if="error" class="mb-4 p-4 text-red-500 bg-red-50 rounded-md">
      {{ error }}
    </div>

    <DataTable
      ref="dt"
      :value="logs"
      :loading="loading"
      :virtualScrollerOptions="virtualScrollerOptions"
      scrollable
      scrollHeight="calc(100vh - 300px)"
      virtualScroller
      class="p-datatable-sm"
      :rows="50"
      dataKey="id"
      tableStyle="min-width: 50rem"
      stripedRows
      showGridlines
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
          {{ formatTimestamp(data.timestamp) }}
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

      <template #empty>
        <div class="text-center py-8">
          <div class="max-w-md mx-auto">
            <h3 class="text-lg font-semibold">No logs found</h3>
            <p class="text-muted-foreground">
              There are no logs available for this source yet.
            </p>
          </div>
        </div>
      </template>

      <template #loading>
        <div class="text-center py-8">Loading logs...</div>
      </template>
    </DataTable>
  </div>
</template>
