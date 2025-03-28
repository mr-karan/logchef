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
const isResizing = ref(false)
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
    minSize: 50,
    size: 150,
    maxSize: 1000,
    enableResizing: true,
}

// Memoized column widths using CSS variables for better performance
const columnSizingVars = computed(() => {
    const styles: Record<string, string> = {}
    table.getAllLeafColumns().forEach(column => {
        styles[`--col-${column.id}-width`] = `${column.getSize()}px`
    })
    return styles
})

// Handle column resizing state with a completely custom implementation
function handleResize(e: MouseEvent | TouchEvent, header: any) {
    console.log('Custom resize handler initiated', { 
        id: header.id, 
        columnId: header.column.id,
        canResize: header.column.getCanResize(),
        event: e.type,
        clientX: 'clientX' in e ? e.clientX : 'N/A',
        clientY: 'clientY' in e ? e.clientY : 'N/A'
    });
    
    // Let's prevent any default behavior
    if ('preventDefault' in e) {
        e.preventDefault();
    }
    
    isResizing.value = true;
    
    // Get current column size and position
    const startSize = header.getSize();
    let startX = 0;
    
    if ('clientX' in e) {
        startX = e.clientX;
    } else if ('touches' in e && e.touches.length > 0) {
        startX = e.touches[0].clientX;
    }
    
    console.log('Starting resize', {
        column: header.column.id,
        startX,
        startSize,
        minSize: header.column.columnDef.minSize || defaultColumn.minSize,
        maxSize: header.column.columnDef.maxSize || defaultColumn.maxSize
    });
    
    // Create custom resize handlers
    const onMouseMove = (moveEvent: MouseEvent) => {
        // Calculate how far the mouse has moved
        const delta = moveEvent.clientX - startX;
        
        // Calculate new size respecting min/max constraints
        const minSize = header.column.columnDef.minSize || defaultColumn.minSize;
        const maxSize = header.column.columnDef.maxSize || defaultColumn.maxSize;
        let newSize = Math.max(minSize, Math.min(maxSize, startSize + delta));
        
        console.log('Resizing in progress', {
            column: header.column.id,
            startX,
            currentX: moveEvent.clientX,
            delta,
            startSize,
            newSize
        });
        
        // Update column size in the state
        const newSizing = {
            ...columnSizing.value,
            [header.column.id]: newSize
        };
        
        // Apply the new sizing
        columnSizing.value = newSizing;
        
        // Apply directly to the table
        table.setColumnSizing(newSizing);
    };
    
    const onTouchMove = (moveEvent: TouchEvent) => {
        if (moveEvent.touches.length === 0) return;
        
        // Calculate how far the touch has moved
        const delta = moveEvent.touches[0].clientX - startX;
        
        // Calculate new size respecting min/max constraints
        const minSize = header.column.columnDef.minSize || defaultColumn.minSize;
        const maxSize = header.column.columnDef.maxSize || defaultColumn.maxSize;
        let newSize = Math.max(minSize, Math.min(maxSize, startSize + delta));
        
        console.log('Touch resizing in progress', {
            column: header.column.id,
            startX,
            currentX: moveEvent.touches[0].clientX,
            delta,
            startSize,
            newSize
        });
        
        // Update column size in the state
        const newSizing = {
            ...columnSizing.value,
            [header.column.id]: newSize
        };
        
        // Apply the new sizing
        columnSizing.value = newSizing;
        
        // Apply directly to the table
        table.setColumnSizing(newSizing);
    };
    
    const onEnd = () => {
        console.log('Resize ended', { 
            column: header.column.id,
            finalSize: columnSizing.value[header.column.id]
        });
        
        isResizing.value = false;
        
        // Clean up event listeners
        window.removeEventListener('mousemove', onMouseMove);
        window.removeEventListener('touchmove', onTouchMove);
        window.removeEventListener('mouseup', onEnd);
        window.removeEventListener('touchend', onEnd);
    };
    
    // Add the event listeners
    window.addEventListener('mousemove', onMouseMove);
    window.addEventListener('touchmove', onTouchMove);
    window.addEventListener('mouseup', onEnd, { once: true });
    window.addEventListener('touchend', onEnd, { once: true });
}

// Initialize table with column sizing
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
        }
    },
    onSortingChange: updaterOrValue => valueUpdater(updaterOrValue, sorting),
    onExpandedChange: updaterOrValue => valueUpdater(updaterOrValue, expanded),
    onColumnVisibilityChange: updaterOrValue => valueUpdater(updaterOrValue, columnVisibility),
    onPaginationChange: updaterOrValue => valueUpdater(updaterOrValue, pagination),
    onGlobalFilterChange: updaterOrValue => valueUpdater(updaterOrValue, globalFilter),
    onColumnSizingChange: updaterOrValue => {
        console.log('onColumnSizingChange called', { 
            type: typeof updaterOrValue, 
            value: typeof updaterOrValue === 'function' ? 'function' : updaterOrValue 
        });
        const result = valueUpdater(updaterOrValue, columnSizing);
        console.log('Column sizing updated to:', result);
        return result;
    },
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    enableColumnResizing: true,
    columnResizeMode: columnResizeMode.value,
    onColumnSizingInfoChange: (info) => {
        // Update isResizing state based on columnSizingInfo
        console.log('onColumnSizingInfoChange called', { 
            columnSizingInfo: table.getState().columnSizingInfo, 
            info
        });
        
        if (table.getState().columnSizingInfo?.isResizingColumn) {
            console.log('Column resize in progress', {
                column: table.getState().columnSizingInfo.columnId,
                startOffset: table.getState().columnSizingInfo.startOffset,
                startSize: table.getState().columnSizingInfo.startSize,
                deltaOffset: table.getState().columnSizingInfo.deltaOffset
            });
            isResizing.value = true;
        }
    },
    defaultColumn,
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

// Initialize column sizing on mount
onMounted(() => {
    console.log('onMounted - initializing column sizing');
    
    // Initialize default sort by timestamp if available
    if (timestampFieldName.value) {
        sorting.value = [{ id: timestampFieldName.value, desc: true }]
    }

    // Initialize column sizing with defaults from column definitions
    const initialSizing: ColumnSizingState = {}
    table.getAllLeafColumns().forEach(column => {
        const size = column.columnDef.size || defaultColumn.size;
        initialSizing[column.id] = size;
        console.log(`Setting initial size for column ${column.id}: ${size}px`);
    })
    columnSizing.value = initialSizing;
    console.log('Initial columnSizing state:', initialSizing);
})

// Add refs for DOM elements
const tableContainerRef = ref<HTMLElement | null>(null)
const tableRef = ref<HTMLElement | null>(null)

onMounted(() => {
    if (!tableContainerRef.value) return

    const resizeObserver = new ResizeObserver(() => {
        // Force a layout update when container size changes
        table.setColumnSizing({...columnSizing.value})
    })

    resizeObserver.observe(tableContainerRef.value)

    return () => {
        resizeObserver.disconnect()
    }
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
    <div class="h-full flex flex-col w-full min-w-0 flex-1 overflow-hidden" :class="{ 'cursor-col-resize select-none': isResizing }">
        <!-- Absolute positioned resize overlay that appears during resize -->
        <div v-if="isResizing" class="absolute inset-0 z-50 pointer-events-none">
            <div class="w-full h-full bg-primary/5 flex items-center justify-center">
                <div class="bg-primary/10 px-4 py-2 rounded-md shadow-md text-xs">
                    Resizing column...
                </div>
            </div>
        </div>
    
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
        <div class="flex-1 relative overflow-hidden" ref="tableContainerRef">
            <div v-if="table.getRowModel().rows?.length" class="absolute inset-0">
                <div class="w-full h-full overflow-auto scrollbar-thin scrollbar-thumb-gray-400/50 scrollbar-track-transparent">
                    <table ref="tableRef" class="table-fixed border-separate border-spacing-0 text-sm" :data-resizing="isResizing">
                        <thead class="sticky top-0 z-10 bg-card border-b shadow-sm">
                            <tr class="border-b border-b-muted-foreground/10">
                                <th v-for="header in table.getHeaderGroups()[0]?.headers"
                                    :key="header.id"
                                    scope="col"
                                    class="group relative h-9 px-3 text-sm font-medium text-left align-middle bg-muted/30 whitespace-nowrap sticky top-0 z-20 overflow-hidden"
                                    :class="[
                                        getColumnType(header.column) === 'timestamp' ? 'font-semibold' : '',
                                        getColumnType(header.column) === 'severity' ? 'font-semibold' : '',
                                        header.column.getIsResizing() ? 'border-r-2 border-r-primary/30' : ''
                                    ]"
                                    :style="{
                                        width: `${header.getSize()}px`,
                                        minWidth: `${header.column.columnDef.minSize ?? defaultColumn.minSize}px`,
                                        maxWidth: `${header.column.columnDef.maxSize ?? defaultColumn.maxSize}px`
                                    }">
                                    <div class="flex items-center h-full">
                                        <FlexRender
                                            v-if="!header.isPlaceholder"
                                            :render="header.column.columnDef.header"
                                            :props="header.getContext()" />

                                        <!-- Column Resizer -->
                                        <div v-if="header.column.getCanResize()"
                                            class="absolute right-0 top-0 h-full w-6 cursor-col-resize select-none touch-none group-hover:bg-primary/30 flex items-center justify-center"
                                            :class="[
                                                header.column.getIsResizing() ? 'bg-primary/50 opacity-100' : ''
                                            ]"
                                            @mousedown="(e) => {
                                                console.log('Resize handle mousedown', {
                                                    column: header.column.id,
                                                    event: e,
                                                    target: e.target
                                                });
                                                e.preventDefault();
                                                e.stopPropagation();
                                                handleResize(e, header);
                                            }"
                                            @touchstart="(e) => {
                                                console.log('Resize handle touchstart', {
                                                    column: header.column.id,
                                                    event: e,
                                                    target: e.target
                                                });
                                                e.preventDefault();
                                                e.stopPropagation();
                                                handleResize(e, header);
                                            }"
                                            @click.stop>
                                            <!-- Visual indicator for resize handle - vertical bar -->
                                            <div class="h-5/6 w-0.5 bg-primary/50 group-hover:bg-primary"></div>
                                        </div>
                                    </div>
                                </th>
                            </tr>
                        </thead>

                        <tbody>
                            <template v-for="(row, index) in table.getRowModel().rows" :key="row.id">
                                <tr class="group cursor-pointer border-b transition-colors"
                                    :class="[
                                        row.getIsExpanded() ? 'bg-primary/10' : index % 2 === 0 ? 'bg-transparent' : 'bg-muted/5'
                                    ]"
                                    @click="handleRowClick(row)($event)">
                                    <td v-for="cell in row.getVisibleCells()"
                                        :key="cell.id"
                                        class="px-3 py-2 align-top font-mono text-xs leading-normal overflow-hidden"
                                        :class="[
                                            cell.column.getIsResizing() ? 'border-r-2 border-r-primary/30' : '',
                                        ]"
                                        :style="{
                                            width: `${cell.column.getSize()}px`,
                                            minWidth: `${cell.column.columnDef.minSize ?? defaultColumn.minSize}px`,
                                            maxWidth: `${cell.column.columnDef.maxSize ?? defaultColumn.maxSize}px`
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
                                        <div class="p-3 bg-muted/30 border-y border-y-primary/40">
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
            <div v-else class="h-full">
                <EmptyState />
            </div>
        </div>
    </div>
</template>

<style>
/* Ensure table layout is fixed */
.table-fixed {
    table-layout: fixed;
    width: 100%;
}

/* Prevent text selection while resizing */
.cursor-col-resize {
    user-select: none;
    -webkit-user-select: none;
    -moz-user-select: none;
    -ms-user-select: none;
    touch-action: none;
}

/* Add styles for resizing visual feedback */
[data-resizing="true"] * {
    cursor: col-resize !important;
    user-select: none !important;
}

/* Make the resize handle active area larger for easier grabbing */
.cursor-col-resize {
    cursor: col-resize !important;
}

/* Prevent selection during resizing */
.select-none {
    user-select: none !important;
    -webkit-user-select: none !important;
    -moz-user-select: none !important;
    -ms-user-select: none !important;
}
</style>
