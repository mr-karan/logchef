<script setup lang="ts">
import type { ColumnDef, ColumnMeta } from '@tanstack/vue-table'
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
    type ColumnSizing as TColumnSizing,
    type ColumnSizingState,
    type ColumnResizeMode,
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
import { createColumns } from './columns'
import { useExploreStore } from '@/stores/explore'
import type { Source } from '@/api/sources'
import { useSourcesStore } from '@/stores/sources'

interface Props {
    columns: ColumnDef<Record<string, any>>[]
    data: Record<string, any>[]
    stats: QueryStats
    sourceId: string
    timestampField?: string
    severityField?: string
    timezone?: 'local' | 'utc'
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
const columnSizing = ref<ColumnSizingState>({})
const columnResizeMode = ref<ColumnResizeMode>('onChange')
const displayTimezone = ref<'local' | 'utc'>(localStorage.getItem('logchef_timezone') === 'utc' ? 'utc' : 'local')

// Save timezone preference whenever it changes
watch(displayTimezone, (newValue) => {
    localStorage.setItem('logchef_timezone', newValue)
})


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

// Define default column configurations
const defaultColumn = {
    minSize: 100,
    size: 150,
    maxSize: 1500,
    enableResizing: true, // Enable resizing for all columns by default
}

// Initialize table with explicit column sizing configurations
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
        },
        get columnSizing() {
            return columnSizing.value
        },
    },
    onSortingChange: updaterOrValue => valueUpdater(updaterOrValue, sorting),
    onExpandedChange: updaterOrValue => valueUpdater(updaterOrValue, expanded),
    onColumnVisibilityChange: updaterOrValue => valueUpdater(updaterOrValue, columnVisibility),
    onPaginationChange: updaterOrValue => valueUpdater(updaterOrValue, pagination),
    onGlobalFilterChange: updaterOrValue => valueUpdater(updaterOrValue, globalFilter),
    onColumnSizingChange: updaterOrValue => valueUpdater(updaterOrValue, columnSizing),
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    // Enable fuzzy search for better matching
    globalFilterFn: 'includesString',
    // Enable column resizing with explicit settings
    enableColumnResizing: true,
    columnResizeMode: columnResizeMode.value,
    defaultColumn: defaultColumn,
    columnResizeDirection: 'ltr',
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

    // Set initial column sizing mode
    columnResizeMode.value = 'onChange'

    // Set explicit column sizes for better horizontal scrolling
    const initialSizes: Record<string, number> = {}

    props.columns.forEach(column => {
        const columnId = column.id || ''
        if (columnId === timestampFieldName.value) {
            initialSizes[columnId] = 220 // timestamps need more space for timezone
        } else if (columnId === severityFieldName.value) {
            initialSizes[columnId] = 100 // severity is usually short
        } else if (columnId === 'message' || columnId === 'msg' || columnId === 'log') {
            initialSizes[columnId] = 500 // message fields get more space
        } else if (columnId.includes('time') || columnId.includes('date')) {
            initialSizes[columnId] = 180 // date/time fields
        } else if (columnId.includes('id')) {
            initialSizes[columnId] = 120 // id fields
        } else {
            initialSizes[columnId] = 200 // default size for other columns
        }
    })

    // Apply the initial sizes - update the state with an object to trigger reactivity
    columnSizing.value = { ...initialSizes }

    // Watch for column size changes to adjust table layout as needed
    watch(columnSizing, () => { }, { deep: true })
})

const exploreStore = useExploreStore()
const sourcesStore = useSourcesStore()

// Add type for column meta
interface CustomColumnMeta extends ColumnMeta<Record<string, any>, unknown> {
    className?: string;
}

// Add type for column definition with custom meta
type CustomColumnDef = ColumnDef<Record<string, any>> & {
    meta?: CustomColumnMeta;
}

// Update refs with proper types
const tableColumns = ref<CustomColumnDef[]>([])
const sourceDetails = ref<Source | null>(null)

// Watch for source details changes
watch(
    () => exploreStore.sourceId,
    async (newSourceId) => {
        if (newSourceId) {
            const result = await sourcesStore.getSource(newSourceId)
            if (result.success && result.data) {
                sourceDetails.value = result.data as Source
            }
        }
    },
    { immediate: true }
)

watch(
    () => exploreStore.columns,
    (newColumns) => {
        if (newColumns) {
            tableColumns.value = createColumns(
                newColumns,
                sourceDetails.value?._meta_ts_field || 'timestamp',
                localStorage.getItem('logchef_timezone') === 'utc' ? 'utc' : 'local',
                sourceDetails.value?._meta_severity_field || 'severity_text'
            )
        }
    },
    { immediate: true }
)
</script>

<template>
    <div class="h-full flex flex-col w-full min-w-0 flex-1 overflow-hidden log-data-table">
        <!-- Results Header with Controls and Pagination -->
        <div class="flex items-center justify-between p-2 border-b flex-shrink-0">
            <!-- Left side - Empty now that stats have been moved to LogExplorer.vue -->
            <div class="flex-1"></div>

            <!-- Right side controls with pagination moved to top -->
            <div class="flex items-center gap-3">
                <!-- Timezone toggle -->
                <div class="flex items-center space-x-1 mr-1">
                    <Button variant="ghost" size="sm" class="h-8 px-2 text-xs"
                        :class="{ 'bg-muted': displayTimezone === 'local' }" @click="displayTimezone = 'local'">
                        Local Time
                    </Button>
                    <Button variant="ghost" size="sm" class="h-8 px-2 text-xs"
                        :class="{ 'bg-muted': displayTimezone === 'utc' }" @click="displayTimezone = 'utc'">
                        UTC
                    </Button>
                </div>

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
                    <div style="min-width: max-content; width: auto;">
                        <table class="caption-bottom text-sm border-separate border-spacing-0 relative"
                            style="width: auto; table-layout: fixed;">
                            <!-- Enhanced header styling -->
                            <thead class="sticky top-0 z-10 bg-card border-b shadow-sm">
                                <tr class="border-b border-b-muted-foreground/10">
                                    <th v-for="header in table.getHeaderGroups()[0]?.headers || []" :key="header.id"
                                        class="h-9 px-3 text-sm font-medium text-left align-middle bg-muted/30 whitespace-nowrap relative sticky top-0 z-20 overflow-hidden"
                                        :class="[
                                            { 'font-semibold': header.id === timestampFieldName || header.id === severityFieldName },
                                            { 'resizing': header.column.getIsResizing() },
                                            (header.column.columnDef as CustomColumnDef).meta?.className
                                        ]" :style="{
                                            width: header.column.getSize() ? `${header.column.getSize()}px` : '200px',
                                            minWidth: '100px'
                                        }">
                                        <FlexRender v-if="!header.isPlaceholder"
                                            :render="header.column.columnDef.header" :props="header.getContext()" />

                                        <div v-if="header.column.getCanResize()" :class="[
                                            'resizer',
                                            { 'isResizing': header.column.getIsResizing() }
                                        ]" @mousedown="(e) => {
                                            try {
                                                // Get and execute the resize handler
                                                const handler = header.getResizeHandler();
                                                handler(e);
                                            } catch (error) {
                                                console.error('Error in resize handler:', error);
                                            }

                                            // Prevent the event from bubbling
                                            e.stopPropagation();
                                        }" @touchstart="header.getResizeHandler()" @click.stop></div>
                                    </th>
                                </tr>
                            </thead>

                            <!-- Improved table body styling -->
                            <tbody class="[&_tr:last-child]:border-0">
                                <template v-for="(row, index) in table.getRowModel().rows" :key="row.id">
                                    <!-- Main Row -->
                                    <tr :data-state="row.getIsSelected() ? 'selected' : undefined"
                                        @click="(e) => handleRowClick(e, row)" :class="[
                                            'cursor-pointer hover:bg-muted/50 border-b border-b-muted-foreground/10 transition-colors w-full',
                                            index % 2 === 0 ? 'bg-transparent' : 'bg-muted/5',
                                            row.getIsExpanded() ? 'bg-primary/5 hover:bg-primary/10 border-primary/10' : ''
                                        ]">
                                        <td v-for="cell in row.getVisibleCells()" :key="cell.id"
                                            class="h-auto px-3 py-2 align-top group overflow-hidden whitespace-nowrap text-ellipsis font-mono text-[13px] leading-normal"
                                            :data-column-id="cell.column.id" :class="[
                                                (cell.column.columnDef as CustomColumnDef).meta?.className,
                                                { 'resizing': cell.column.getIsResizing() }
                                            ]" :style="{
                                                width: cell.column.getSize() ? `${cell.column.getSize()}px` : '200px',
                                                minWidth: '100px',
                                                maxWidth: cell.column.getSize() ? `${cell.column.getSize()}px` : '200px',
                                                overflow: 'hidden'
                                            }">
                                            <div class="flex items-center gap-1 w-full overflow-hidden">
                                                <!-- Cell Content with improved typography -->
                                                <span
                                                    v-if="cell.column.id === severityFieldName && getSeverityClasses(cell.getValue(), cell.column.id)"
                                                    :class="getSeverityClasses(cell.getValue(), cell.column.id)"
                                                    class="shrink-0 mx-1">
                                                    {{ formatCellValue(cell.getValue()).toUpperCase() }}
                                                </span>
                                                <template v-else>
                                                    <div class="whitespace-nowrap overflow-hidden text-ellipsis w-full">
                                                        <FlexRender :render="cell.column.columnDef.cell"
                                                            :props="cell.getContext()" class="text-[13px]" />
                                                    </div>
                                                </template>

                                            </div>
                                        </td>
                                    </tr>

                                    <!-- Expanded Row with improved JSON viewer styling -->
                                    <tr v-if="row.getIsExpanded()">
                                        <td :colspan="row.getVisibleCells().length" class="p-0">
                                            <div class="p-1 bg-muted/30 border-y border-primary/10 overflow-hidden">
                                                <JsonViewer :value="row.original" :expanded="false"
                                                    class="text-[13px]" />
                                            </div>
                                        </td>
                                    </tr>
                                </template>
                            </tbody>
                        </table>
                    </div>
                </div>
            </div>
            <div v-else class="h-full">
                <EmptyState />
            </div>
        </div>
    </div>
</template>

<style scoped>
/* Custom scrollbar styles scoped to this component only */
.log-data-table .custom-scrollbar::-webkit-scrollbar {
    width: 10px !important;
    height: 10px !important;
    display: block !important;
}

.log-data-table .custom-scrollbar::-webkit-scrollbar-track {
    background: transparent;
}

.log-data-table .custom-scrollbar::-webkit-scrollbar-thumb {
    background-color: rgba(155, 155, 155, 0.5);
    border-radius: 4px;
    min-height: 40px;
    min-width: 40px;
}

.log-data-table .custom-scrollbar::-webkit-scrollbar-thumb:hover {
    background-color: rgba(155, 155, 155, 0.8);
}

.log-data-table .custom-scrollbar::-webkit-scrollbar-corner {
    background: transparent;
}

/* Firefox scrollbar */
.log-data-table .custom-scrollbar {
    scrollbar-width: thin;
    scrollbar-color: rgba(155, 155, 155, 0.5) transparent;
    overflow-x: scroll !important;
    overflow-y: auto !important;
    -webkit-overflow-scrolling: touch;
    height: 100% !important;
    max-height: 100% !important;
    width: 100% !important;
}

/* Resizer styles - scoped to this component only */
.log-data-table .resizer {
    position: absolute;
    right: 0;
    top: 0;
    height: 100%;
    width: 4px;
    cursor: col-resize !important;
    user-select: none;
    touch-action: none;
    z-index: 50;
    transition: background-color 0.1s ease;
    background-color: rgba(0, 0, 0, 0.05);
}

.log-data-table .resizer:hover {
    background-color: rgba(0, 0, 0, 0.3);
    width: 8px;
    right: -3px;
}

.log-data-table .resizer.isResizing {
    background-color: rgba(0, 0, 0, 0.5);
    opacity: 1;
    width: 8px;
}

/* Class for column being resized */
.log-data-table th.resizing,
.log-data-table td.resizing {
    border-right: 2px dashed rgba(0, 0, 0, 0.2);
    user-select: none;
}

/* Column width presets - with specific widths for log viewing */
.log-data-table .timestamp-column {
    min-width: 220px;
    width: 220px;
}

.log-data-table .severity-column {
    min-width: 100px;
    width: 100px;
}

.log-data-table .message-column {
    min-width: 500px;
    width: 500px;
}

.log-data-table .default-column {
    min-width: 200px;
    width: 200px;
}

/* Special content styling */
:deep(.log-data-table .json-content) {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    padding: 2px 0;
    background-color: rgba(0, 0, 0, 0.01);
    border-radius: 2px;
    font-size: 13px;
}


:deep(.log-data-table .flex-render-content) {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
    font-size: 13px;
    width: 100%;
}

/* Update severity label styling to be more compact */
:deep(.severity-label) {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 80px;
    margin: 0 4px;
    font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, sans-serif;
}

:deep(.severity-error) {
    background-color: hsl(var(--destructive));
    color: white;
}

:deep(.severity-warn),
:deep(.severity-warning) {
    background-color: hsl(var(--warning));
    color: hsl(var(--warning-foreground));
}

:deep(.severity-info) {
    background-color: hsl(var(--info));
    color: white;
}

:deep(.severity-debug) {
    background-color: hsl(var(--muted));
    color: hsl(var(--muted-foreground));
}
</style>
