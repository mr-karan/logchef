<script setup lang="ts">
import { ref, onMounted, watch, computed } from 'vue'
import { useToast } from '@/components/ui/toast'
import ErrorAlert from '@/components/ui/ErrorAlert.vue'
import { useRoute } from 'vue-router'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Button } from '@/components/ui/button'
import { useSourcesStore } from '@/stores/sources'
import { storeToRefs } from 'pinia'

const sourcesStore = useSourcesStore()
const { toast } = useToast()
const route = useRoute()
const selectedSourceId = ref<string>('')

// Get reactive state from the store
const { loadingStates, error: storeError } = storeToRefs(sourcesStore)

// Computed properties to reactively access store state
const isLoadingStats = computed(() => 
  sourcesStore.isLoadingOperation(`getSourceStats-${selectedSourceId.value}`)
)

const statsError = computed(() => {
  if (storeError.value && selectedSourceId.value) {
    return storeError.value.getSourceStats?.[selectedSourceId.value] || null
  }
  return null
})

// Computed properties for stats data
const stats = computed(() => {
  if (!selectedSourceId.value) return { tableStats: null, columnStats: null }
  
  const sourceStats = sourcesStore.getSourceStatsById(parseInt(selectedSourceId.value))
  return {
    tableStats: sourceStats?.table_stats || null,
    columnStats: sourceStats?.column_stats || null
  }
})

// Fetch all sources on component mount
onMounted(async () => {
  // Hydrate the store
  await sourcesStore.hydrate()
  
  // Check if sourceId is provided in the URL query parameters
  const sourceIdFromQuery = route.query.sourceId as string
  if (sourceIdFromQuery) {
    selectedSourceId.value = sourceIdFromQuery
    await fetchSourceStats()
  }
})

// Watch for changes in route query parameters
watch(() => route.query.sourceId, (newSourceId) => {
  if (newSourceId && newSourceId !== selectedSourceId.value) {
    selectedSourceId.value = newSourceId as string
    fetchSourceStats()
  }
})

// Fetch source stats
const fetchSourceStats = async () => {
  if (!selectedSourceId.value) {
    // Keep this toast as it's a validation error, not an API error
    toast({
      title: 'Error',
      description: 'Please select a source first',
      variant: 'destructive',
    })
    return
  }

  await sourcesStore.getSourceStats(parseInt(selectedSourceId.value))
}
</script>

<template>
  <div class="space-y-6">
    <Card>
      <CardHeader>
        <CardTitle>Source Statistics</CardTitle>
        <CardDescription>
          View disk usage and performance metrics for your ClickHouse tables
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div class="flex gap-4 mb-6">
          <div class="space-y-2 flex-1">
            <label for="source" class="block text-sm font-medium">Select Source</label>
            <Select v-model="selectedSourceId">
              <SelectTrigger class="w-full">
                <SelectValue placeholder="Select a source" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="source in sourcesStore.visibleSources" :key="source.id" :value="String(source.id)">
                  {{ source.connection.database }}.{{ source.connection.table_name }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="flex items-end">
            <Button @click="fetchSourceStats" :disabled="isLoadingStats">
              <span v-if="isLoadingStats">Loading...</span>
              <span v-else>Get Stats</span>
            </Button>
          </div>
        </div>

        <!-- Table Stats Card -->
        <Card v-if="stats.tableStats" class="mb-6">
          <CardHeader>
            <CardTitle>Table Overview</CardTitle>
            <CardDescription>
              {{ stats.tableStats.database }}.{{ stats.tableStats.table }}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              <div class="bg-muted p-3 rounded-md">
                <div class="text-sm text-muted-foreground">Rows</div>
                <div class="text-2xl font-bold">{{ stats.tableStats.rows.toLocaleString() }}</div>
              </div>
              <div class="bg-muted p-3 rounded-md">
                <div class="text-sm text-muted-foreground">Parts</div>
                <div class="text-2xl font-bold">{{ stats.tableStats.part_count }}</div>
              </div>
              <div class="bg-muted p-3 rounded-md">
                <div class="text-sm text-muted-foreground">Compression Ratio</div>
                <div class="text-2xl font-bold">{{ stats.tableStats.compr_rate.toFixed(2) }}x</div>
              </div>
              <div class="bg-muted p-3 rounded-md">
                <div class="text-sm text-muted-foreground">Compressed Size</div>
                <div class="text-2xl font-bold">{{ stats.tableStats.compressed }}</div>
              </div>
              <div class="bg-muted p-3 rounded-md">
                <div class="text-sm text-muted-foreground">Uncompressed Size</div>
                <div class="text-2xl font-bold">{{ stats.tableStats.uncompressed }}</div>
              </div>
            </div>
          </CardContent>
        </Card>

        <!-- Column Stats Table -->
        <Card v-if="stats.columnStats && stats.columnStats.length > 0">
          <CardHeader>
            <CardTitle>Column Statistics</CardTitle>
            <CardDescription>
              Storage and performance metrics for each column
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Column</TableHead>
                  <TableHead>Compressed</TableHead>
                  <TableHead>Uncompressed</TableHead>
                  <TableHead>Compression Ratio</TableHead>
                  <TableHead>Avg. Row Size</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow v-for="column in stats.columnStats" :key="column.column">
                  <TableCell class="font-medium">{{ column.column }}</TableCell>
                  <TableCell>{{ column.compressed }}</TableCell>
                  <TableCell>{{ column.uncompressed }}</TableCell>
                  <TableCell>{{ column.compr_ratio.toFixed(2) }}x</TableCell>
                  <TableCell>{{ column.avg_row_size.toFixed(2) }} bytes</TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        <ErrorAlert 
          v-if="statsError"
          :error="statsError"
          title="Failed to load statistics"
          @retry="fetchSourceStats"
          class="mb-6"
        />

        <div v-if="!stats.tableStats && !isLoadingStats && !statsError" class="text-center py-6 text-muted-foreground">
          Select a source and click "Get Stats" to view statistics
        </div>
      </CardContent>
    </Card>
  </div>
</template>
