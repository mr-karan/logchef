<script setup lang="ts">
import { ref, onMounted, watch, computed } from 'vue'
import { useToast } from '@/composables/useToast'
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
const { error: storeError } = storeToRefs(sourcesStore)

// Computed properties to reactively access store state
const isLoadingStats = computed(() =>
  sourcesStore.isLoadingOperation(`getSourceStats-${selectedSourceId.value}`)
)

const statsError = computed(() => {
  // Just check if there's any error in the store when we're not loading
  if (!isLoadingStats.value && storeError.value && selectedSourceId.value) {
    return 'Failed to load source statistics. Please try again.';
  }
  return null;
})

// Computed properties for stats data
const stats = computed(() => {
  if (!selectedSourceId.value) return {
    tableStats: null,
    columnStats: null,
    tableInfo: null,
    ttl: null
  }

  const sourceStats = sourcesStore.getSourceStatsById(parseInt(selectedSourceId.value))
  return {
    tableStats: sourceStats?.table_stats || null,
    columnStats: sourceStats?.column_stats || null,
    tableInfo: sourceStats?.table_info || null,
    ttl: sourceStats?.ttl || null
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

  // For the admin view, we don't need a team context (admin route)
  // This is why this component is only accessible to admins in the router config
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

        <!-- Table Info Card -->
        <Card v-if="stats.tableInfo" class="mb-6">
          <CardHeader>
            <CardTitle>Table Schema Information</CardTitle>
            <CardDescription>
              {{ stats.tableInfo.database }}.{{ stats.tableInfo.name }}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 mb-4">
              <div class="bg-muted p-3 rounded-md">
                <div class="text-sm text-muted-foreground">Engine</div>
                <div class="text-lg font-semibold">{{ stats.tableInfo.engine }}</div>
                <div v-if="stats.tableInfo.engine_params && stats.tableInfo.engine_params.length > 0" class="text-xs text-muted-foreground mt-1">
                  {{ stats.tableInfo.engine_params.join(', ') }}
                </div>
              </div>
              <div class="bg-muted p-3 rounded-md">
                <div class="text-sm text-muted-foreground">Columns</div>
                <div class="text-lg font-semibold">{{ stats.tableInfo.columns?.length || 0 }}</div>
              </div>
              <div v-if="stats.tableInfo.sort_keys && stats.tableInfo.sort_keys.length > 0" class="bg-muted p-3 rounded-md">
                <div class="text-sm text-muted-foreground">Sort Keys</div>
                <div class="text-sm font-medium">{{ stats.tableInfo.sort_keys.join(', ') }}</div>
              </div>
              <div v-if="stats.ttl" class="bg-muted p-3 rounded-md">
                <div class="text-sm text-muted-foreground">TTL</div>
                <div class="text-sm font-medium">{{ stats.ttl }}</div>
              </div>
            </div>
          </CardContent>
        </Card>

        <!-- Table Stats Card -->
        <Card v-if="stats.tableStats" class="mb-6">
          <CardHeader>
            <CardTitle>Table Statistics</CardTitle>
            <CardDescription>
              Storage and performance metrics
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

        <!-- Schema Details Table -->
        <Card v-if="stats.tableInfo && stats.tableInfo.ext_columns && stats.tableInfo.ext_columns.length > 0" class="mb-6">
          <CardHeader>
            <CardTitle>Schema Details</CardTitle>
            <CardDescription>
              Detailed column schema information
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Column</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Nullable</TableHead>
                  <TableHead>Primary Key</TableHead>
                  <TableHead>Default</TableHead>
                  <TableHead>Comment</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow v-for="column in stats.tableInfo.ext_columns" :key="column.name">
                  <TableCell class="font-medium">{{ column.name }}</TableCell>
                  <TableCell>{{ column.type }}</TableCell>
                  <TableCell>
                    <span :class="column.is_nullable ? 'text-green-600' : 'text-red-600'">
                      {{ column.is_nullable ? 'Yes' : 'No' }}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span v-if="column.is_primary_key" class="text-blue-600 font-semibold">Yes</span>
                    <span v-else class="text-muted-foreground">No</span>
                  </TableCell>
                  <TableCell class="text-sm">{{ column.default_expression || '–' }}</TableCell>
                  <TableCell class="text-sm">{{ column.comment || '–' }}</TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        <!-- Basic Schema Table (fallback if extended columns not available) -->
        <Card v-else-if="stats.tableInfo && stats.tableInfo.columns && stats.tableInfo.columns.length > 0" class="mb-6">
          <CardHeader>
            <CardTitle>Schema Overview</CardTitle>
            <CardDescription>
              Basic column information
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Column</TableHead>
                  <TableHead>Type</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow v-for="column in stats.tableInfo.columns" :key="column.name">
                  <TableCell class="font-medium">{{ column.name }}</TableCell>
                  <TableCell>{{ column.type }}</TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        <ErrorAlert v-if="statsError" :error="statsError" title="Failed to load statistics" @retry="fetchSourceStats"
          class="mb-6" />

        <div v-if="!stats.tableStats && !isLoadingStats && !statsError" class="text-center py-6 text-muted-foreground">
          Select a source and click "Get Stats" to view statistics
        </div>
      </CardContent>
    </Card>
  </div>
</template>
