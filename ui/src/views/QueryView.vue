<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '@/services/api'
import LogTable from '@/components/logs/LogTable.vue'
import type { Log, LogResponse } from '@/types/logs'
import type { Source } from '@/types/source'

const logs = ref<Log[]>([])
const loading = ref(true)
const error = ref<string | null>(null)
const sourceId = ref('')
const sources = ref<Source[]>([])

async function fetchLogs() {
  if (!sourceId.value) {
    error.value = 'No source selected'
    return
  }
  
  try {
    loading.value = true
    const data = await api.fetchLogs(sourceId.value, { limit: 50 })
    logs.value = data.logs || []
    error.value = null
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to fetch logs'
    logs.value = []
  } finally {
    loading.value = false
  }
}

// Initial fetch of sources to get the first sourceId
async function fetchSources() {
  try {
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
  }
}

onMounted(() => {
  fetchSources()
})
</script>

<template>
  <div class="container py-8">
    <div class="mb-8">
      <h1 class="text-2xl font-bold">Query Explorer</h1>
      <p class="text-sm text-muted-foreground mt-1">Search and analyze your logs across all sources.</p>
    </div>

    <div v-if="error" class="mb-4 p-4 bg-red-50 text-red-600 rounded-md">
      {{ error }}
    </div>

    <!-- Source selector -->
    <div class="mb-4">
      <select 
        v-model="sourceId"
        class="w-full p-2 border rounded-md"
        @change="fetchLogs"
      >
        <option v-for="source in sources" :key="source.ID" :value="source.ID">
          {{ source.Name }}
        </option>
      </select>
    </div>

    <!-- Loading state -->
    <div v-if="loading" class="flex items-center justify-center h-64">
      <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-gray-900"></div>
    </div>

    <!-- Error state -->
    <div v-else-if="error" class="p-4 mb-4 text-red-700 bg-red-100 rounded-md">
      {{ error }}
    </div>

    <!-- Empty state -->
    <div v-else-if="logs.length === 0" class="text-center py-8 text-gray-500">
      No logs found
    </div>

    <!-- Logs table -->
    <LogTable v-else :data="logs" />
  </div>
</template>
