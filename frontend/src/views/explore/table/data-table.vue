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

// Helper functions for cell handling
function formatCellValue(value: any): string {
    if (value === null || value === undefined) return '';
    return String(value);
}

function isLongIdField(cell: any): boolean {
    const value = cell.getValue();
    const columnId = cell.column.id;
    return columnId.includes('id') &&
        typeof value === 'string' &&
        value.length > 20;
}

function truncateId(str: string): string {
    if (str.length <= 20) return str;
    return `${str.substring(0, 8)}...${str.substring(str.length - 8)}`;
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
    <div class="h-full flex flex-col">
        <!-- Results Header with Controls (removed duplicated stats) -->
        <div class="flex items-center justify-between p-2 border-b">
            <div class="flex items-center gap-2">
                <h3 class="text-sm font-medium">Data</h3>
            </div>

            <!-- Right side controls -->
            <div class="flex items-center gap-3">
                <DataTableColumnSelector :table="table" />
                <div class="relative w-64">
                    <Search class="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
                    <Input placeholder="Search across all columns..." aria-label="Search in all columns"
                        v-model="globalFilter" class="pl-8 h-8 text-sm" />
                </div>
            </div>
        </div>

        <!-- Table Section with improved scrolling behavior -->
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

                        <!-- Improved table body styling without severity markers -->
                        <tbody class="[&_tr:last-child]:border-0">
                            <template v-for="(row, index) in table.getRowModel().rows" :key="row.id">
                                <!-- Main Row with improved styling -->
                                <tr :data-state="row.getIsSelected() ? 'selected' : undefined"
                                    @click="(e) => handleRowClick(e, row)" :class="[
                                        'cursor-pointer hover:bg-muted/50 border-b border-b-muted-foreground/10 transition-colors',
                                        index % 2 === 0 ? 'bg-transparent' : 'bg-muted/5'
                                    ]">
                                    <td v-for="cell in row.getVisibleCells()" :key="cell.id"
                                        class="h-auto px-3 py-1.5 min-w-[120px] whitespace-normal break-all align-middle group"
                                        :data-column-id="cell.column.id">
                                        <div class="max-w-none flex items-center gap-1">
                                            <!-- Cell Content with improved typography -->
                                            <span
                                                v-if="cell.column.id === severityFieldName && getSeverityClasses(cell.getValue(), cell.column.id)"
                                                :class="getSeverityClasses(cell.getValue(), cell.column.id)"
                                                class="font-medium px-1.5 py-0.5 rounded text-xs">
                                                {{ cell.getValue() }}
                                            </span>
                                            <!-- Handle ID-like fields with truncation -->
                                            <span v-else-if="isLongIdField(cell)"
                                                :title="formatCellValue(cell.getValue())"
                                                class="truncate max-w-[180px]">
                                                {{ truncateId(formatCellValue(cell.getValue())) }}
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

        <!-- Bottom pagination controls with result count -->
        <div v-if="table.getRowModel().rows?.length > 0"
            class="flex items-center justify-between border-t pt-2 px-3 mt-auto">
            <div class="text-xs text-muted-foreground">
                {{ table.getFilteredRowModel().rows.length }}
                <span class="text-muted-foreground/70">results</span>
                <span v-if="table.getFilteredRowModel().rows.length !== props.data.length">
                    (filtered from {{ props.data.length }} total)
                </span>
            </div>
            <DataTablePagination :table="table" />
        </div>
    </div>
</template>

<style>
/* Remove hljs styles since they're now in JsonViewer */

/* Custom scrollbar styles - improved for better visibility */
.custom-scrollbar::-webkit-scrollbar {
    width: 8px !important;
    height: 8px !important;
    display: block !important;
}

.custom-scrollbar::-webkit-scrollbar-track {
    background: transparent;
}

.custom-scrollbar::-webkit-scrollbar-thumb {
    background-color: rgba(155, 155, 155, 0.6);
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
    scrollbar-color: rgba(155, 155, 155, 0.6) transparent;
}

/* Ensure proper table layout */
.flex-1.relative.overflow-hidden {
    position: relative;
    display: flex;
    flex-direction: column;
    flex: 1 1 auto;
    min-height: 0;
}

.absolute.inset-0 {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    left: 0;
    display: flex;
    flex-direction: column;
}

/* Additional fixes for table rendering */
table {
    width: 100%;
    border-collapse: separate;
    border-spacing: 0;
}

.w-full.h-full.overflow-auto.custom-scrollbar {
    -webkit-overflow-scrolling: touch;
}

/* Additional improvements for data table */
th {
    position: sticky;
    top: 0;
    z-index: 10;
    transition: background-color 0.2s;
    font-family: var(--font-sans);
}

th:hover {
    background-color: rgba(0, 0, 0, 0.06);
}

/* Improve table cells */
td {
    max-width: 300px;
    overflow: hidden;
    line-height: 1.3;
    font-size: 11px;
    /* Slightly smaller font size */
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
}

/* Make all cell content use monospace font */
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
}

/* Ensure the JSON viewer also uses small monospace */
.json-viewer {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
    font-size: 11px;
}

/* Remove severity marker styles as requested */

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

/* We ensure these styles are applied to the configured timestamp field */
[data-column-id] {
    /* Applied dynamically based on column ID */
}

/* Ensure consistent styling for timestamp-related fields */
td[data-column-id="timestamp"],
td[data-column-id$="_at"],
td[data-column-id$="_time"] {
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
    letter-spacing: -0.01em;
}

/* Additional styles for all columns to ensure proper layout */
.max-w-none.flex.items-center.gap-1 {
    white-space: normal;
    word-break: break-word;
}
</style>
