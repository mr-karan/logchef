<script setup lang="ts">
import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Download, Timer, Rows4, RefreshCw, Search } from 'lucide-vue-next'
import DataTablePagination from './data-table-pagination.vue'
import DataTableColumnSelector from './data-table-column-selector.vue'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { exportTableData } from './export'
import type { Table } from '@tanstack/vue-table'
import type { QueryStats } from '@/api/explore'
import { usePreferencesStore } from '@/stores/preferences'

interface Props {
  table: Table<any>
  stats?: QueryStats
  isLoading?: boolean
  showColumnSelector?: boolean
  showExport?: boolean
  showPagination?: boolean
  showSearch?: boolean
  showTimezoneToggle?: boolean
  showStats?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  stats: undefined,
  isLoading: false,
  showColumnSelector: true,
  showExport: true,
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
    <!-- Left side - Query stats & Loading Indicator -->
    <div class="flex items-center gap-2 lg:gap-3 text-sm text-muted-foreground">
      <!-- Loading Spinner -->
      <RefreshCw v-if="isLoading" class="h-4 w-4 text-primary animate-spin" />
      
      <!-- Query Stats -->
      <template v-if="showStats && !isLoading && stats">
        <span v-if="stats.execution_time_ms !== undefined" class="inline-flex items-center" :title="`Query time: ${formatExecutionTime(stats.execution_time_ms)}`">
          <Timer class="h-3.5 w-3.5 lg:mr-1.5 text-muted-foreground/80" />
          <span class="hidden lg:inline">Query time:</span>
          <span class="ml-1 font-medium text-foreground/90">{{ formatExecutionTime(stats.execution_time_ms) }}</span>
        </span>
        <span v-if="stats.rows_read !== undefined" class="inline-flex items-center" :title="`Rows: ${stats.rows_read.toLocaleString()}`">
          <Rows4 class="h-3.5 w-3.5 lg:mr-1.5 text-muted-foreground/80" />
          <span class="hidden lg:inline">Rows:</span>
          <span class="ml-1 font-medium text-foreground/90">{{ stats.rows_read.toLocaleString() }}</span>
        </span>
      </template>
      
      <span v-if="isLoading" class="text-primary animate-pulse hidden sm:inline">Loading...</span>
    </div>

    <!-- Right side controls -->
    <div class="flex items-center gap-1 lg:gap-3">
      <!-- Export CSV Button with Dropdown -->
      <DropdownMenu v-if="showExport && hasRows">
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="sm" class="h-8 px-2 lg:px-3" title="Export table data">
            <Download class="h-4 w-4" />
            <span class="hidden lg:inline ml-1">Export</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" class="w-48">
          <DropdownMenuItem @click="exportTableData(table, {
            fileName: `logchef-export-all-${new Date().toISOString().slice(0, 10)}`,
            exportType: 'all',
            includeHiddenColumns: true
          })">
            Export All Data
          </DropdownMenuItem>
          <DropdownMenuItem @click="exportTableData(table, {
            fileName: `logchef-export-${new Date().toISOString().slice(0, 10)}`,
            exportType: 'visible'
          })">
            Export Visible Rows
          </DropdownMenuItem>
          <DropdownMenuItem @click="exportTableData(table, {
            fileName: `logchef-export-filtered-${new Date().toISOString().slice(0, 10)}`,
            exportType: 'filtered'
          })">
            Export All Filtered Rows
          </DropdownMenuItem>
          <DropdownMenuItem @click="exportTableData(table, {
            fileName: `logchef-export-page-${new Date().toISOString().slice(0, 10)}`,
            exportType: 'page'
          })">
            Export Current Page
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <!-- Timezone toggle -->
      <div v-if="showTimezoneToggle" class="flex items-center space-x-0.5 lg:space-x-1">
        <Button 
          variant="ghost" 
          size="sm" 
          class="h-8 px-1.5 lg:px-2 text-xs"
          :class="{ 'bg-muted': displayTimezone === 'local' }" 
          @click="displayTimezone = 'local'"
          title="Local Time"
        >
          <span class="hidden lg:inline">Local Time</span>
          <span class="lg:hidden">Local</span>
        </Button>
        <Button 
          variant="ghost" 
          size="sm" 
          class="h-8 px-1.5 lg:px-2 text-xs"
          :class="{ 'bg-muted': displayTimezone === 'utc' }" 
          @click="displayTimezone = 'utc'"
          title="UTC"
        >
          UTC
        </Button>
      </div>

      <!-- Pagination -->
      <DataTablePagination v-if="showPagination && hasRows" :table="table" />

      <!-- Column selector -->
      <DataTableColumnSelector 
        v-if="showColumnSelector && table" 
        :table="table" 
        :column-order="columnOrder"
        @update:column-order="table.setColumnOrder($event)" 
      />

      <!-- Search input - hidden on small screens -->
      <div v-if="showSearch" class="relative hidden md:block w-48 lg:w-64">
        <Search class="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
        <Input 
          placeholder="Search..." 
          aria-label="Search in all columns"
          v-model="globalFilter" 
          class="pl-8 h-8 text-sm" 
        />
      </div>
    </div>
  </div>
</template>
