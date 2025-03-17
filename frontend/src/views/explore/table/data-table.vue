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
import { ref, computed, onMounted, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Copy, Search } from 'lucide-vue-next'
import { valueUpdater, getSeverityClasses } from '@/lib/utils'
import { Input } from '@/components/ui/input'
import DataTableColumnSelector from './data-table-column-selector.vue'
import DataTablePagination from './data-table-pagination.vue'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import type { QueryStats } from '@/api/explore'
import JsonViewer from '@/components/json-viewer/JsonViewer.vue'
import EmptyState from '@/views/explore/EmptyState.vue'

interface Props {
    columns: ColumnDef<Record<string, any>>[]
    data: Record<string, any>[]
    stats: QueryStats
    sourceId: string
    timestampField?: string
    severityField?: string
}

const props = defineProps<Props>()

// Get the actual field names to use with fallbacks
const timestampFieldName = computed(() => props.timestampField || 'timestamp')
const severityFieldName = computed(() => props.severityField || 'severity_text')

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

// Helper function for cell handling
function formatCellValue(value: any): string {
    if (value === null || value === undefined) return '';
    return String(value);
}

// Check if a column is the timestamp or severity column
function isSpecialColumn(columnId: string): boolean {
    return columnId === timestampFieldName.value || columnId === severityFieldName.value;
}

// Make sure timestamp field is first in column order
const sortedColumns = computed(() => {
    // If no timestamp field specified, just return the columns as is
    if (!timestampFieldName.value) return props.columns;

    // Filter the columns to put timestamp first
    const tsColumn = props.columns.find(col => col.id === timestampFieldName.value);
    if (!tsColumn) return props.columns;

    // Create a new array with timestamp column first, then all other columns
    return [
        tsColumn,
        ...props.columns.filter(col => col.id !== timestampFieldName.value)
    ];
});

// Initialize table
const table = useVueTable({
    get data() {
        return props.data
    },
    get columns() {
        return sortedColumns.value
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

// Ensure timestamp field is always first when component mounts
onMounted(() => {
    // Initialize default sort by timestamp if available
    if (timestampFieldName.value) {
        sorting.value = [
            {
                id: timestampFieldName.value,
                desc: true // Sort newest first by default
            }
        ]
    }
})
</script>

<template>
    <div class="h-full flex flex-col w-full min-w-0 flex-1 overflow-hidden">
        <!-- Results Header with Controls and Pagination -->
        <div class="flex items-center justify-between p-2 border-b flex-shrink-0">
            <!-- Left side - Empty now that stats have been moved to LogExplorer.vue -->
            <div class="flex-1"></div>

            <!-- Right side controls with pagination moved to top -->
            <div class="flex items-center gap-3">
                <!-- Pagination moved to top -->
                <DataTablePagination v-if="table.getRowModel().rows?.length > 0" :table="table" />

                <!-- Column selector -->
                <DataTableColumnSelector :table="table" />

                <!-- Search input -->
                <div class="relative w-64">
                    <Search class="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
                    <Input placeholder="Search across all columns..." aria-label="Search in all columns"
                        v-model="globalFilter" class="pl-8 h-8 text-sm" />
                </div>
            </div>
        </div>

        <!-- Table Section with full-height scrolling -->
        <div class="flex-1 relative overflow-hidden">
            <div v-if="table.getRowModel().rows?.length" class="absolute inset-0">
                <div class="w-full h-full overflow-auto custom-scrollbar">
                    <table class="w-full caption-bottom text-sm min-w-full">
                        <!-- Enhanced header styling -->
                        <thead class="sticky top-0 z-10 bg-card border-b">
                            <tr class="border-b border-b-muted-foreground/10">
                                <th v-for="header in table.getHeaderGroups()[0]?.headers || []" :key="header.id"
                                    class="h-8 px-3 text-xs font-medium min-w-[120px] text-left align-middle bg-muted/30"
                                    :class="{ 'font-semibold': header.id === timestampFieldName || header.id === severityFieldName }">
                                    <FlexRender v-if="!header.isPlaceholder" :render="header.column.columnDef.header"
                                        :props="header.getContext()" />
                                </th>
                            </tr>
                        </thead>

                        <!-- Improved table body styling -->
                        <tbody class="[&_tr:last-child]:border-0">
                            <template v-for="(row, index) in table.getRowModel().rows" :key="row.id">
                                <!-- Main Row -->
                                <tr :data-state="row.getIsSelected() ? 'selected' : undefined"
                                    @click="(e) => handleRowClick(e, row)" :class="[
                                        'cursor-pointer hover:bg-muted/50 border-b border-b-muted-foreground/10 transition-colors',
                                        index % 2 === 0 ? 'bg-transparent' : 'bg-muted/5'
                                    ]">
                                    <td v-for="cell in row.getVisibleCells()" :key="cell.id"
                                        class="h-auto px-3 py-1.5 min-w-[180px] whitespace-normal break-words align-middle group"
                                        :data-column-id="cell.column.id">
                                        <div class="max-w-none flex items-center gap-1">
                                            <!-- Cell Content with improved typography -->
                                            <span
                                                v-if="cell.column.id === severityFieldName && getSeverityClasses(cell.getValue(), cell.column.id)"
                                                :class="getSeverityClasses(cell.getValue(), cell.column.id)"
                                                class="font-medium px-1.5 py-0.5 rounded text-xs">
                                                {{ cell.getValue() }}
                                            </span>
                                            <template v-else>
                                                <FlexRender :render="cell.column.columnDef.cell"
                                                    :props="cell.getContext()" />
                                            </template>

                                            <!-- Enhanced Copy Value Button with tooltip -->
                                            <Button variant="ghost" size="icon"
                                                class="h-5 w-5 p-0 opacity-0 group-hover:opacity-100 transition-opacity ml-1 hover:bg-muted"
                                                @click.stop="copyCell(cell.getValue())"
                                                :title="`Copy ${cell.column.id} value`">
                                                <Copy class="h-3 w-3" />
                                            </Button>
                                        </div>
                                    </td>
                                </tr>

                                <!-- Expanded Row with improved JSON viewer styling -->
                                <tr v-if="row.getIsExpanded()">
                                    <td :colspan="row.getVisibleCells().length" class="p-0">
                                        <div class="p-1 bg-muted/30 border-t border-b border-muted">
                                            <JsonViewer :value="row.original" :expanded="false" class="text-xs" />
                                        </div>
                                    </td>
                                </tr>
                            </template>
                        </tbody>
                    </table>
                </div>
            </div>
            <div v-else class="h-full">
                <EmptyState />
            </div>
        </div>
    </div>
</template>

<style>
/* Custom scrollbar styles for better visibility */
.custom-scrollbar::-webkit-scrollbar {
    width: 10px !important;
    height: 10px !important;
    display: block !important;
}

.custom-scrollbar::-webkit-scrollbar-track {
    background: transparent;
}

.custom-scrollbar::-webkit-scrollbar-thumb {
    background-color: rgba(155, 155, 155, 0.5);
    border-radius: 4px;
    min-height: 40px;
    min-width: 40px;
}

.custom-scrollbar::-webkit-scrollbar-thumb:hover {
    background-color: rgba(155, 155, 155, 0.8);
}

.custom-scrollbar::-webkit-scrollbar-corner {
    background: transparent;
}

/* Firefox scrollbar */
.custom-scrollbar {
    scrollbar-width: thin;
    scrollbar-color: rgba(155, 155, 155, 0.5) transparent;
}

/* Enhanced full-height table layout - improve flex properties */
.h-full.flex.flex-col {
    display: flex;
    flex-direction: column;
    height: 100% !important;
    width: 100%;
    min-height: 0;
    flex: 1 1 auto !important;
    /* Important for flex child to shrink */
    overflow: hidden;
}

.flex-1.relative.overflow-hidden {
    position: relative;
    height: 100% !important;
    overflow: hidden;
}

.absolute.inset-0 {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    left: 0;
    height: 100%;
    width: 100%;
}

.w-full.h-full.overflow-auto.custom-scrollbar {
    -webkit-overflow-scrolling: touch;
    height: 100% !important;
    max-height: 100% !important;
    overflow: auto !important;
}

/* Enhanced table rendering */
table {
    width: 100%;
    border-collapse: separate;
    border-spacing: 0;
    table-layout: fixed;
}

/* Fixed header with shadow for better visibility */
thead {
    position: sticky;
    top: 0;
    z-index: 20;
    background-color: var(--background, #fff);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
}

th {
    position: sticky;
    top: 0;
    z-index: 20;
    background-color: var(--background, #fff);
    transition: background-color 0.2s;
    font-family: var(--font-sans);
    font-weight: 500;
}

th:hover {
    background-color: rgba(0, 0, 0, 0.04);
}

/* Improve table cells with consistent monospace for better log readability */
td {
    line-height: 1.4;
    font-size: 11px;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
}

/* Ensure all cell content uses monospace font */
td .max-w-none.flex.items-center.gap-1>span,
td .max-w-none.flex.items-center.gap-1 div {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
    font-size: 11px;
    letter-spacing: -0.01em;
}

/* Target FlexRender content specifically */
:deep(.flex-render-content) {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
    font-size: 11px;
    word-break: normal;
    overflow-wrap: break-word;
    white-space: normal;
}

/* Ensure the JSON viewer also uses small monospace */
.json-viewer {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
    font-size: 11px;
}

/* Expand rows to take full width */
tr {
    width: 100%;
}

/* Improved zebra striping */
tr:nth-of-type(even) {
    background-color: rgba(0, 0, 0, 0.02);
}

/* Responsive adjustments */
@media (max-width: 768px) {
    .custom-scrollbar {
        -webkit-overflow-scrolling: touch;
    }

    th,
    td {
        padding: 6px !important;
    }
}

/* Apply monospace tabular display to timestamps for better readability */
[data-column-id="timestamp"] {
    font-variant-numeric: tabular-nums !important;
    letter-spacing: -0.01em !important;
}

/* Additional styles for all columns to ensure proper layout */
.max-w-none.flex.items-center.gap-1 {
    white-space: normal;
    word-break: normal;
    overflow-wrap: break-word;
    max-width: 100%;
}
</style>
