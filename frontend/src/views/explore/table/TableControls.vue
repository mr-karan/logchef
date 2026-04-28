<script setup lang="ts">
import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import { Input } from '@/components/ui/input'
import { RefreshCw, Search } from 'lucide-vue-next'
import DataTablePagination from './data-table-pagination.vue'
import DataTableColumnSelector from './data-table-column-selector.vue'
import type { Table } from '@tanstack/vue-table'
import type { QueryStats } from '@/api/explore'
import { usePreferencesStore } from '@/stores/preferences'

interface Props {
  table: Table<any>
  stats?: QueryStats
  isLoading?: boolean
  showColumnSelector?: boolean
  showPagination?: boolean
  showSearch?: boolean
  showTimezoneToggle?: boolean
  showStats?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  stats: undefined,
  isLoading: false,
  showColumnSelector: true,
  showPagination: true,
  showSearch: true,
  showTimezoneToggle: true,
  showStats: true
})

const emit = defineEmits<{
  'update:timezone': [value: 'local' | 'utc']
  'update:globalFilter': [value: string]
}>()

const preferencesStore = usePreferencesStore()
const { preferences } = storeToRefs(preferencesStore)

// Local state
const displayTimezone = computed({
  get: () => preferences.value.timezone,
  set: (value: 'local' | 'utc') => {
    preferencesStore.updatePreferences({ timezone: value })
    emit('update:timezone', value)
  }
})

const globalFilter = computed({
  get: () => props.table.getState().globalFilter ?? '',
  set: (value) => {
    props.table.setGlobalFilter(value)
    emit('update:globalFilter', value)
  }
})

// Column order state
const columnOrder = computed(() => props.table.getState().columnOrder)

// Save timezone preference handled via preferences store

// Helper function to format execution time
function formatExecutionTime(ms: number): string {
  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(2)}s`
  }
  return `${Math.round(ms)}ms`
}

// Check if table has rows
const hasRows = computed(() => props.table && props.table.getRowModel().rows?.length > 0)
</script>

<template>
  <div class="flex items-center justify-between p-2 border-b flex-shrink-0">
    <!-- Left zone: compact query stats -->
    <div class="flex items-center gap-2 text-xs text-muted-foreground">
      <RefreshCw v-if="isLoading" class="h-3.5 w-3.5 text-primary animate-spin" />
      <span v-if="isLoading" class="text-primary animate-pulse hidden sm:inline">Loading…</span>
      <template v-else-if="showStats && stats">
        <span v-if="stats.execution_time_ms !== undefined" :title="`Query time: ${formatExecutionTime(stats.execution_time_ms)}`">
          {{ formatExecutionTime(stats.execution_time_ms) }}
        </span>
        <span v-if="stats.execution_time_ms !== undefined && stats.rows_read !== undefined" class="text-muted-foreground/40">·</span>
        <span v-if="stats.rows_read !== undefined" :title="`Rows read: ${stats.rows_read.toLocaleString()}`">
          {{ stats.rows_read.toLocaleString() }} rows
        </span>
      </template>
    </div>

    <!-- Right zone: 3 clusters separated by subtle dividers -->
    <div class="flex items-center gap-2">
      <!-- Timezone segmented control -->
      <div v-if="showTimezoneToggle" class="flex items-center bg-muted rounded-md p-0.5">
        <button
          type="button"
          class="h-6 px-2 rounded text-xs transition-colors"
          :class="displayTimezone === 'local' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'"
          @click="displayTimezone = 'local'"
          title="Local Time"
        >
          Local
        </button>
        <button
          type="button"
          class="h-6 px-2 rounded text-xs transition-colors"
          :class="displayTimezone === 'utc' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'"
          @click="displayTimezone = 'utc'"
          title="UTC"
        >
          UTC
        </button>
      </div>

      <div v-if="showTimezoneToggle" class="h-5 w-px bg-border" />

      <!-- Pagination + Columns -->
      <DataTablePagination v-if="showPagination && hasRows" :table="table" />
      <DataTableColumnSelector
        v-if="showColumnSelector && table"
        :table="table"
        :column-order="columnOrder"
        @update:column-order="table.setColumnOrder($event)"
      />

      <div v-if="showSearch" class="h-5 w-px bg-border" />

      <!-- Search -->
      <div v-if="showSearch" class="relative hidden md:block w-48 lg:w-64">
        <Search class="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Search…"
          aria-label="Search in all columns"
          v-model="globalFilter"
          class="pl-8 h-8 text-sm"
        />
      </div>
    </div>
  </div>
</template>
