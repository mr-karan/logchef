<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed } from 'vue'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { RefreshCw, Search, FilterX, WrapText, ListPlus } from 'lucide-vue-next'
import DataTablePagination from './data-table-pagination.vue'
import DataTableColumnSelector from './data-table-column-selector.vue'
import type { Table } from './tableFeatures'

const { t } = useI18n();

interface Props {
  table: Table<Record<string, unknown>>
  isLoading?: boolean
  // Undefined hides the wrap toggle.
  wrap?: boolean
  showColumnSelector?: boolean
  showResetColumns?: boolean
  showPagination?: boolean
  showSearch?: boolean
  // Field the search term is added to the query on; undefined hides "add to query".
  searchQueryField?: string
}

const props = withDefaults(defineProps<Props>(), {
  isLoading: false,
  wrap: undefined,
  showColumnSelector: true,
  showResetColumns: false,
  showPagination: true,
  showSearch: true,
  searchQueryField: undefined,
})

const emit = defineEmits<{
  'update:globalFilter': [value: string]
  'reset-columns': []
  'update:wrap': [value: boolean]
  'add-search-to-query': [field: string, term: string]
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

const pageRange = computed(() => {
  const { pageIndex, pageSize } = props.table.atoms.pagination.get()
  const total = filteredRowCount.value
  const start = total === 0 ? 0 : pageIndex * pageSize + 1
  return { start, end: Math.min(total, pageIndex * pageSize + pageSize), total }
})

const searchTerm = computed(() => globalFilter.value.trim())

function addSearchToQuery() {
  if (!props.searchQueryField || !searchTerm.value) return
  emit('add-search-to-query', props.searchQueryField, searchTerm.value)
}

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
      <span v-else-if="hasRows" data-result-range>
        {{ t('explore.resultRange', { start: pageRange.start.toLocaleString(), end: pageRange.end.toLocaleString(), total: pageRange.total.toLocaleString() }) }}
      </span>

      <!-- Column filter indicator - client-side filtering of the loaded page only -->
      <template v-if="hasColumnFilters">
        <span v-if="!isLoading && hasRows" class="text-muted-foreground/40">·</span>
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
      <slot name="tools" />
      <Button
        v-if="wrap !== undefined"
        variant="ghost"
        size="sm"
        class="h-7 px-2 text-xs"
        :class="wrap ? 'bg-muted text-foreground' : 'text-muted-foreground'"
        :aria-pressed="wrap"
        :title="t('explore.wrapLines')"
        @click="emit('update:wrap', !wrap)"
      >
        <WrapText class="h-3.5 w-3.5 mr-1" />
        {{ t('explore.wrap') }}
      </Button>
      <DataTableColumnSelector
        v-if="showColumnSelector && table"
        :table="table"
        :show-reset-defaults="showResetColumns"
        @reset-defaults="emit('reset-columns')"
      />

      <div v-if="showSearch" class="h-5 w-px bg-border" />

      <!-- Search: filters the loaded rows only, like the column filters -->
      <div v-if="showSearch" class="hidden md:flex items-center gap-1">
        <div class="relative w-48 lg:w-64">
          <Search class="absolute left-2 top-1.5 h-4 w-4 text-muted-foreground" />
          <Input
            :placeholder="t('explore.searchLoadedRows')"
            :aria-label="t('explore.searchLoadedRows')"
            :title="t('explore.searchLoadedRowsHint')"
            v-model="globalFilter"
            class="pl-8 h-7 text-sm"
          />
        </div>
        <Button
          v-if="searchQueryField && searchTerm"
          variant="ghost"
          size="sm"
          class="h-7 px-2 text-xs"
          data-add-search-to-query
          :title="t('explore.addSearchToQueryHint', { field: searchQueryField, term: searchTerm })"
          @click="addSearchToQuery"
        >
          <ListPlus class="h-3.5 w-3.5 mr-1" />
          {{ t('explore.addSearchToQuery') }}
        </Button>
      </div>
    </div>
  </div>
</template>
