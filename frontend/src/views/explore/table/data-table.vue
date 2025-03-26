<script setup lang="ts">
import type { ColumnDef, ColumnMeta, Row } from '@tanstack/vue-table'
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

// Get column type from meta data
function getColumnType(column: any): string | undefined {
    return column?.columnDef?.meta?.columnType;
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
    defaultColumn,
    columnResizeDirection: 'ltr',
})

// Simplified row expansion handler
const handleRowClick = (row: Row<Record<string, any>>) => (e: MouseEvent) => {
    // Don't expand if clicking on an interactive element
    if ((e.target as HTMLElement).closest('.actions-dropdown, button, a, input, select')) {
        return;
    }
    row.toggleExpanded();
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

// Initialize table when component mounts with sensible defaults
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
    <div class="h-full flex flex-col w-full min-w-0 flex-1 overflow-hidden">
        <!-- Results Header with Controls and Pagination -->
        <div class="flex items-center justify-between p-2 border-b flex-shrink-0">
            <!-- Left side - Empty now that stats have been moved to LogExplorer.vue -->
            <div class="flex-1"></div>

            <!-- Right side controls with pagination moved to top -->
            <div class="flex items-center gap-3">
                <!-- Timezone toggle -->
                <div class="flex items-center space-x-1 mr-1">
                    <Button variant="ghost" size="sm" 
                        class="h-8 px-2 text-xs"
                        :class="{ 'bg-muted': displayTimezone === 'local' }" 
                        @click="displayTimezone = 'local'">
                        Local Time
                    </Button>
                    <Button variant="ghost" size="sm" 
                        class="h-8 px-2 text-xs"
                        :class="{ 'bg-muted': displayTimezone === 'utc' }" 
                        @click="displayTimezone = 'utc'">
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
                    <Input placeholder="Search across all columns..." 
                        aria-label="Search in all columns"
                        v-model="globalFilter" 
                        class="pl-8 h-8 text-sm" />
                </div>
            </div>
        </div>

        <!-- Table Section with full-height scrolling -->
        <div class="flex-1 relative overflow-hidden">
            <div v-if="table.getRowModel().rows?.length" class="absolute inset-0">
                <div class="w-full h-full overflow-auto scrollbar-thin scrollbar-thumb-gray-400/50 scrollbar-track-transparent">
                    <div class="min-w-max w-auto">
                        <table class="w-auto table-fixed border-separate border-spacing-0 text-sm">
                            <!-- Table Header -->
                            <thead class="sticky top-0 z-10 bg-card border-b shadow-sm">
                                <tr class="border-b border-b-muted-foreground/10">
                                    <th v-for="header in table.getHeaderGroups()[0]?.headers || []" 
                                        :key="header.id"
                                        class="h-9 px-3 text-sm font-medium text-left align-middle bg-muted/30 whitespace-nowrap sticky top-0 z-20 overflow-hidden"
                                        :class="[
                                            getColumnType(header.column) === 'timestamp' ? 'font-semibold' : '',
                                            getColumnType(header.column) === 'severity' ? 'font-semibold' : '',
                                            header.column.getIsResizing() ? 'relative border-r-2 border-r-primary/30' : ''
                                        ]"
                                        :style="{
                                            width: `${header.column.getSize()}px`,
                                            minWidth: `${header.column.getSize() / 2}px`
                                        }">
                                        <FlexRender 
                                            v-if="!header.isPlaceholder"
                                            :render="header.column.columnDef.header" 
                                            :props="header.getContext()" />

                                        <!-- Column Resizer Handle -->
                                        <div 
                                            v-if="header.column.getCanResize()"
                                            class="absolute right-0 top-0 h-full w-1.5 group-hover:bg-primary/20 cursor-col-resize"
                                            :class="{ 'bg-primary/50 w-2': header.column.getIsResizing() }"
                                            @mousedown="header.getResizeHandler()"
                                            @touchstart="header.getResizeHandler()"
                                            @click.stop>
                                        </div>
                                    </th>
                                </tr>
                            </thead>

                            <!-- Table Body -->
                            <tbody>
                                <template v-for="(row, index) in table.getRowModel().rows" :key="row.id">
                                    <!-- Main Data Row -->
                                    <tr 
                                        :data-state="row.getIsSelected() ? 'selected' : undefined"
                                        @click="handleRowClick(row)"
                                        class="group cursor-pointer border-b border-b-muted-foreground/10 transition-colors"
                                        :class="[
                                            'hover:bg-muted/50',
                                            index % 2 === 0 ? 'bg-transparent' : 'bg-muted/5',
                                            row.getIsExpanded() ? 'bg-primary/5 hover:bg-primary/10 border-primary/10' : ''
                                        ]">
                                        <td 
                                            v-for="cell in row.getVisibleCells()" 
                                            :key="cell.id"
                                            class="px-3 py-2 align-top font-mono text-xs leading-normal overflow-hidden"
                                            :class="[
                                                cell.column.getIsResizing() ? 'border-r-2 border-r-primary/30' : ''
                                            ]"
                                            :style="{
                                                width: `${cell.column.getSize()}px`,
                                                maxWidth: `${cell.column.getSize()}px`
                                            }">
                                            <!-- Cell Content -->
                                            <div class="flex items-center gap-1 w-full overflow-hidden">
                                                <!-- Severity Field Cell -->
                                                <span
                                                    v-if="cell.column.id === severityFieldName && getSeverityClasses(cell.getValue(), cell.column.id)"
                                                    :class="getSeverityClasses(cell.getValue(), cell.column.id)"
                                                    class="shrink-0 mx-1">
                                                    {{ formatCellValue(cell.getValue()).toUpperCase() }}
                                                </span>
                                                <!-- Regular Cell Content -->
                                                <template v-else>
                                                    <div class="whitespace-nowrap overflow-hidden text-ellipsis w-full">
                                                        <FlexRender 
                                                            :render="cell.column.columnDef.cell"
                                                            :props="cell.getContext()" />
                                                    </div>
                                                </template>
                                            </div>
                                        </td>
                                    </tr>

                                    <!-- Expanded Row -->
                                    <tr v-if="row.getIsExpanded()">
                                        <td :colspan="row.getVisibleCells().length" class="p-0">
                                            <div class="p-3 bg-muted/30 border-y border-primary/10">
                                                <JsonViewer 
                                                    :value="row.original" 
                                                    :expanded="false"
                                                    class="text-xs" />
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

<style>
/* No component-specific styles needed - all moved to Tailwind utility classes */
</style>
