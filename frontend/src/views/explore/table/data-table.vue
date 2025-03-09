<script setup lang="ts">
import type { ColumnDef } from '@tanstack/vue-table'
import {
    FlexRender,
    getCoreRowModel,
    getSortedRowModel,
    getExpandedRowModel,
    getPaginationRowModel,
    getFilteredRowModel,
    useVueTable,
    type SortingState,
    type ExpandedState,
    type VisibilityState,
    type PaginationState,
} from '@tanstack/vue-table'
import { ref } from 'vue'
import { Button } from '@/components/ui/button'
import { Copy, Search } from 'lucide-vue-next'
import { valueUpdater, getSeverityClasses } from '@/lib/utils'
import { Input } from '@/components/ui/input'
import DataTableToolbar from './data-table-toolbar.vue'
import DataTableColumnSelector from './data-table-column-selector.vue'
import DataTablePagination from './data-table-pagination.vue'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import type { QueryStats } from '@/api/explore'
import JsonViewer from '@/components/json-viewer/JsonViewer.vue'

interface Props {
    columns: ColumnDef<Record<string, any>>[]
    data: Record<string, any>[]
    stats: QueryStats
    sourceId: string
}

const props = defineProps<Props>()

// Table state
const sorting = ref<SortingState>([])
const expanded = ref<ExpandedState>({})
const columnVisibility = ref<VisibilityState>({})
const pagination = ref<PaginationState>({
    pageIndex: 0,
    pageSize: 50,
})
const globalFilter = ref('')

const { toast } = useToast()

// Initialize table
const table = useVueTable({
    get data() {
        return props.data
    },
    get columns() {
        return props.columns
    },
    state: {
        get sorting() {
            return sorting.value
        },
        get expanded() {
            return expanded.value
        },
        get columnVisibility() {
            return columnVisibility.value
        },
        get pagination() {
            return pagination.value
        },
        get globalFilter() {
            return globalFilter.value
        }
    },
    onSortingChange: updaterOrValue => valueUpdater(updaterOrValue, sorting),
    onExpandedChange: updaterOrValue => valueUpdater(updaterOrValue, expanded),
    onColumnVisibilityChange: updaterOrValue => valueUpdater(updaterOrValue, columnVisibility),
    onPaginationChange: updaterOrValue => valueUpdater(updaterOrValue, pagination),
    onGlobalFilterChange: updaterOrValue => valueUpdater(updaterOrValue, globalFilter),
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    // Enable fuzzy search for better matching
    globalFilterFn: 'includesString',
})

// Handle row click for expansion
const handleRowClick = (e: MouseEvent, row: any) => {
    // Don't expand if clicking on the dropdown
    if ((e.target as HTMLElement).closest('.actions-dropdown')) {
        return
    }
    row.toggleExpanded()
}

// Add back the copyCell function since it's still needed for individual cells
const copyCell = (value: any) => {
    const text = typeof value === 'object' ? JSON.stringify(value, null, 2) : String(value)
    navigator.clipboard.writeText(text)
    toast({
        title: 'Copied',
        description: 'Value copied to clipboard',
        duration: TOAST_DURATION.SUCCESS,
    })
}
</script>

<template>
    <div class="space-y-2">
        <!-- Results Header with Stats and Controls -->
        <div class="flex items-center justify-between p-3 border-b">
            <div class="flex items-center gap-2">
                <h3 class="text-sm font-medium">Results</h3>
                <!-- Stats display with better visuals -->
                <div class="flex items-center text-xs text-muted-foreground" v-if="stats?.rows_read !== undefined">
                    <span class="px-2 py-0.5 rounded-full bg-muted font-medium">
                        {{ props.data.length.toLocaleString() }} 
                        <span class="text-xs font-normal">shown</span>
                    </span>
                    <span class="mx-1.5">/</span>
                    <span class="text-xs">
                        {{ stats.rows_read.toLocaleString() }} rows processed
                        <template v-if="stats?.elapsed !== undefined">
                            in {{ stats.elapsed.toFixed(3) }}s
                        </template>
                    </span>
                </div>
            </div>
            
            <!-- Right side controls -->
            <div class="flex items-center gap-3">
                <DataTableColumnSelector :table="table" />
                <div class="relative w-64">
                    <Search class="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
                    <Input 
                        placeholder="Search in all columns..." 
                        v-model="globalFilter" 
                        class="pl-8 h-8 text-sm" 
                    />
                </div>
            </div>
        </div>

        <!-- Table Section with improved styling -->
        <div class="rounded-md border-0">
            <div class="overflow-auto max-h-[calc(100vh-280px)]">
                <table class="w-full caption-bottom text-sm">
                    <!-- Enhanced header styling -->
                    <thead class="sticky top-0 z-10 bg-card border-b">
                        <tr class="border-b border-b-muted-foreground/10">
                            <th v-for="header in table.getHeaderGroups()[0]?.headers || []" :key="header.id"
                                class="h-9 px-3 text-xs font-medium min-w-[150px] text-left align-middle">
                                <FlexRender 
                                    v-if="!header.isPlaceholder" 
                                    :render="header.column.columnDef.header"
                                    :props="header.getContext()" 
                                />
                            </th>
                        </tr>
                    </thead>
                    
                    <!-- Improved table body styling -->
                    <tbody class="[&_tr:last-child]:border-0">
                        <template v-if="table.getRowModel().rows?.length">
                            <template v-for="row in table.getRowModel().rows" :key="row.id">
                                <!-- Main Row with improved styling -->
                                <tr 
                                    :data-state="row.getIsSelected() ? 'selected' : undefined"
                                    @click="(e) => handleRowClick(e, row)"
                                    class="cursor-pointer hover:bg-muted/30 border-b border-b-muted-foreground/10 transition-colors"
                                >
                                    <td 
                                        v-for="cell in row.getVisibleCells()" 
                                        :key="cell.id"
                                        class="h-auto px-3 py-2 min-w-[150px] whitespace-normal break-all align-middle group"
                                    >
                                        <div class="max-w-none flex items-center gap-1">
                                            <!-- Cell Content with improved typography -->
                                            <span 
                                                v-if="getSeverityClasses(cell.getValue(), cell.column.id)"
                                                :class="getSeverityClasses(cell.getValue(), cell.column.id)"
                                                class="font-medium"
                                            >
                                                {{ cell.getValue() }}
                                            </span>
                                            <template v-else>
                                                <FlexRender 
                                                    :render="cell.column.columnDef.cell"
                                                    :props="cell.getContext()" 
                                                />
                                            </template>

                                            <!-- Enhanced Copy Value Button -->
                                            <Button 
                                                variant="ghost" 
                                                size="icon"
                                                class="h-5 w-5 p-0 opacity-0 group-hover:opacity-100 transition-opacity ml-1"
                                                @click.stop="copyCell(cell.getValue())" 
                                                title="Copy value"
                                            >
                                                <Copy class="h-3 w-3" />
                                            </Button>
                                        </div>
                                    </td>
                                </tr>

                                <!-- Expanded Row with improved JSON viewer styling -->
                                <tr v-if="row.getIsExpanded()">
                                    <td :colspan="row.getVisibleCells().length" class="p-0">
                                        <div class="p-4 bg-muted/30 border-t border-b border-muted">
                                            <JsonViewer 
                                                :value="row.original" 
                                                :expanded="false" 
                                                class="text-sm" 
                                            />
                                        </div>
                                    </td>
                                </tr>
                            </template>
                        </template>
                        <template v-else>
                            <tr>
                                <td :colspan="props.columns.length + 1" class="h-24 text-center text-sm">
                                    No results. Try adjusting your query or time range.
                                </td>
                            </tr>
                        </template>
                    </tbody>
                </table>
            </div>
        </div>
        
        <!-- Bottom pagination controls -->
        <div class="flex items-center justify-end border-t pt-2 px-2">
            <DataTablePagination :table="table" />
        </div>
    </div>
</template>

<style>
/* Remove hljs styles since they're now in JsonViewer */
</style>
