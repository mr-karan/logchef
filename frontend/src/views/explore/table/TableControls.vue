<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed } from 'vue'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { RefreshCw, Search, FilterX } from 'lucide-vue-next'
import DataTablePagination from './data-table-pagination.vue'
import DataTableColumnSelector from './data-table-column-selector.vue'
import type { Table } from './tableFeatures'
import type { QueryStats } from '@/api/explore'

const { t } = useI18n();

interface Props {
  table: Table<Record<string, unknown>>
  stats?: QueryStats
  isLoading?: boolean
  showColumnSelector?: boolean
  showResetColumns?: boolean
  showPagination?: boolean
  showSearch?: boolean
  showStats?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  stats: undefined,
  isLoading: false,
  showColumnSelector: true,
  showResetColumns: false,
  showPagination: true,
  showSearch: true,
  showStats: true
})

const emit = defineEmits<{
  'update:globalFilter': [value: string]
  'reset-columns': []
}>()

const globalFilter = computed({
  get: () => props.table.atoms.globalFilter.get() ?? '',
  set: (value) => {
    props.table.setGlobalFilter(value)
    emit('update:globalFilter', value)
  }
})

// Check if table has rows
const hasRows = computed(() => props.table && props.table.getRowModel().rows?.length > 0)

// Column filters are client-side (see data-table.vue / columnFilter.ts) and only
// ever filter the currently loaded result page - surface that plainly here.
const hasColumnFilters = computed(() => (props.table?.atoms.columnFilters.get()?.length ?? 0) > 0)
const filteredRowCount = computed(() => props.table?.getFilteredRowModel().rows.length ?? 0)
const totalRowCount = computed(() => props.table?.getCoreRowModel().rows.length ?? 0)

function clearColumnFilters() {
  props.table.resetColumnFilters()
}
</script>

<template>
  <div class="flex items-center justify-between h-8 px-2 border-b flex-shrink-0">
    <!-- Left zone: compact query stats -->
    <div class="flex items-center gap-2 text-xs text-muted-foreground">
      <RefreshCw v-if="isLoading" class="h-3.5 w-3.5 text-primary animate-spin" />
      <span v-if="isLoading" class='text-primary animate-pulse hidden sm:inline'>{{ t('ui.loading2') }}</span>
      <template v-else-if="showStats && stats">
        <span v-if="stats.rows_read !== undefined" :title="t('pages.rowsReadTooltip', { count: stats.rows_read.toLocaleString() })">
          {{ stats.rows_read.toLocaleString() }} {{ t('ui.rows') }}
        </span>
      </template>

      <!-- Column filter indicator - client-side filtering of the loaded page only -->
      <template v-if="hasColumnFilters">
        <span v-if="!isLoading && (stats || showStats)" class="text-muted-foreground/40">·</span>
        <span
          class="text-primary font-medium"
          :title="t('ui.columnFiltersOnlyNarrowTheRowsAlreadyLoadedOnThisPage')"
        >
          {{ t('explore.filteredRows', { count: filteredRowCount.toLocaleString(), total: totalRowCount.toLocaleString() }) }}
        </span>
        <Button
          variant="ghost"
          size="sm"
          class="h-6 px-1.5 text-xs text-muted-foreground hover:text-foreground"
          :title="t('ui.clearAllColumnFilters')"
          @click="clearColumnFilters"
        >
          <FilterX class="h-3 w-3 mr-1" />
          {{ t('ui.clearFilters') }}
        </Button>
      </template>
    </div>

    <!-- Right zone: 3 clusters separated by subtle dividers -->
    <div class="flex items-center gap-2">
      <!-- Pagination + Columns -->
      <DataTablePagination v-if="showPagination && hasRows" :table="table" />
      <DataTableColumnSelector
        v-if="showColumnSelector && table"
        :table="table"
        :show-reset-defaults="showResetColumns"
        @reset-defaults="emit('reset-columns')"
      />

      <div v-if="showSearch" class="h-5 w-px bg-border" />

      <!-- Search -->
      <div v-if="showSearch" class="relative hidden md:block w-48 lg:w-64">
        <Search class="absolute left-2 top-1.5 h-4 w-4 text-muted-foreground" />
        <Input
          :placeholder="t('ui.search')"
          :aria-label="t('ui.searchInAllColumns')"
          v-model="globalFilter"
          class="pl-8 h-7 text-sm"
        />
      </div>
    </div>
  </div>
</template>
