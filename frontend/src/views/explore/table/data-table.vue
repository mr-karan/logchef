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
    type Header,
} from '@tanstack/vue-table'
import { ref, computed, onMounted, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Search, GripVertical, Download } from 'lucide-vue-next'
import { valueUpdater, getSeverityClasses } from '@/lib/utils'
import { Input } from '@/components/ui/input'
import DataTableColumnSelector from './data-table-column-selector.vue'
import DataTablePagination from './data-table-pagination.vue'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import type { QueryStats, ColumnInfo } from '@/api/explore'
import JsonViewer from '@/components/json-viewer/JsonViewer.vue'
import EmptyState from '@/views/explore/EmptyState.vue'
import { createColumns } from './columns'
import { useExploreStore } from '@/stores/explore'
import type { Source } from '@/api/sources'
import { useSourcesStore } from '@/stores/sources'
import { useStorage, type UseStorageOptions, type RemovableRef } from '@vueuse/core'
import { exportTableData } from './export'
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'

interface Props {
    columns: ColumnDef<Record<string, any>>[]
    data: Record<string, any>[]
    stats: QueryStats
    sourceId: string
    teamId: number | null
    timestampField?: string
    severityField?: string
    timezone?: 'local' | 'utc'
    searchTerms?: string[] // Add new prop
}

// Define the structure for storing state
interface DataTableState {
    columnOrder: string[];
    columnSizing: ColumnSizingState;
    columnVisibility: VisibilityState;
}

const props = withDefaults(defineProps<Props>(), {
    timestampField: 'timestamp',
    severityField: 'severity_text',
    timezone: 'local',
    searchTerms: () => [] // Default to empty array
})

// Get the actual field names to use with fallbacks
const timestampFieldName = computed(() => props.timestampField || 'timestamp')
const severityFieldName = computed(() => props.severityField || 'severity_text')

// Move tableColumns declaration near the top
const tableColumns = ref<CustomColumnDef[]>([])

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
const columnOrder = ref<string[]>([])
const draggingColumnId = ref<string | null>(null)
const dragOverColumnId = ref<string | null>(null)

// --- Local Storage State Management ---
const storageKey = computed(() => {
    if (props.teamId == null || !props.sourceId) return null; // Check for null teamId explicitly
    return `logchef-tableState-${props.teamId}-${props.sourceId}`;
});

// Load state from localStorage
function loadStateFromStorage(): DataTableState | null {
    if (!storageKey.value) return null;

    try {
        const storedData = localStorage.getItem(storageKey.value);
        if (!storedData) return null;

        return JSON.parse(storedData) as DataTableState;
    } catch (error) {
        console.error("Error loading table state from localStorage:", error);
        return null;
    }
}

// Save state to localStorage
function saveStateToStorage(state: DataTableState) {
    if (!storageKey.value) return;

    try {
        localStorage.setItem(storageKey.value, JSON.stringify(state));
    } catch (error) {
        console.error("Error saving table state to localStorage:", error);
    }
}

// Initialize state from localStorage or defaults
function initializeState(columns: ColumnDef<Record<string, any>>[]) {
    const currentColumnIds = columns.map(c => c.id!).filter(Boolean);
    let initialOrder: string[] = [];
    let initialSizing: ColumnSizingState = {};
    let initialVisibility: VisibilityState = {};

    // Try to load from storage
    const savedState = loadStateFromStorage();

    if (savedState) {
        // Process column order
        const savedOrder = savedState.columnOrder || [];
        const filteredSavedOrder = savedOrder.filter(id => currentColumnIds.includes(id));
        const newColumnIds = currentColumnIds.filter(id => !filteredSavedOrder.includes(id));
        initialOrder = [...filteredSavedOrder, ...newColumnIds];

        // Process column sizing and visibility
        const savedSizing = savedState.columnSizing || {};
        const savedVisibility = savedState.columnVisibility || {};

        currentColumnIds.forEach(id => {
            // Handle sizing
            if (savedSizing[id] !== undefined) {
                initialSizing[id] = savedSizing[id];
            } else {
                const columnDef = columns.find(c => c.id === id);
                initialSizing[id] = columnDef?.size ?? defaultColumn.size;
            }

            // Handle visibility
            initialVisibility[id] = savedVisibility[id] !== undefined ? savedVisibility[id] : true;
        });
    } else {
        initialOrder = currentColumnIds;

        currentColumnIds.forEach(id => {
            const columnDef = columns.find(c => c.id === id);
            initialSizing[id] = columnDef?.size ?? defaultColumn.size;
            initialVisibility[id] = true; // Default all columns to visible
        });
    }

    return { initialOrder, initialSizing, initialVisibility };
}

// Watch for changes in columns OR search terms to regenerate table columns
watch(
    () => [props.columns, props.searchTerms, displayTimezone.value], // Also watch timezone for createColumns
    ([newColumns, newSearchTerms, newTimezone]) => {
        if (!newColumns || newColumns.length === 0) {
            tableColumns.value = []; // Clear columns if input is empty
            // Reset dependent state if columns are cleared
            columnOrder.value = [];
            columnSizing.value = {};
            columnVisibility.value = {};
            return;
        }

        // Regenerate columns with the current search terms and timezone
        tableColumns.value = createColumns(
            newColumns as any, // Use the columns directly as was working before
            timestampFieldName.value,
            newTimezone as 'local' | 'utc',
            severityFieldName.value,
            props.searchTerms
        );

        // Re-initialize state based on the potentially new columns
        const { initialOrder, initialSizing, initialVisibility } = initializeState(tableColumns.value);

        // Only update if different to prevent infinite loops or unnecessary updates
        if (JSON.stringify(columnOrder.value) !== JSON.stringify(initialOrder)) {
            columnOrder.value = initialOrder;
        }
        if (JSON.stringify(columnSizing.value) !== JSON.stringify(initialSizing)) {
            columnSizing.value = initialSizing;
        }
        if (JSON.stringify(columnVisibility.value) !== JSON.stringify(initialVisibility)) {
            columnVisibility.value = initialVisibility;
        }
    },
    { immediate: true, deep: true } // Use deep watch for searchTerms array changes
);

// Save state whenever relevant parts change
watch([columnOrder, columnSizing, columnVisibility], () => {
    if (!storageKey.value) return;

    // Make sure we have columns loaded before saving
    if (props.columns && props.columns.length > 0) {
        saveStateToStorage({
            columnOrder: columnOrder.value,
            columnSizing: columnSizing.value,
            columnVisibility: columnVisibility.value
        });
    }
}, { deep: true });

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

// Column order is now managed by state, no need for sortedColumns computed

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

// Handle column resizing with a clean custom implementation
function handleResize(e: MouseEvent | TouchEvent, header: any) {
    // Prevent default behavior
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

    // Create custom resize handlers
    const onMouseMove = (moveEvent: MouseEvent) => {
        // Calculate how far the mouse has moved
        const delta = moveEvent.clientX - startX;

        // Calculate new size respecting min/max constraints
        const minSize = header.column.columnDef.minSize || defaultColumn.minSize;
        const maxSize = header.column.columnDef.maxSize || defaultColumn.maxSize;
        let newSize = Math.max(minSize, Math.min(maxSize, startSize + delta));

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

// Initialize table
const table = useVueTable({
    get data() {
        return props.data
    },
    // Use tableColumns directly
    get columns() {
        return tableColumns.value;
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
        get columnOrder() {
            // Important: Let the table read the order directly from the state ref
            return columnOrder.value
        },
    },
    // Keep columnOrder handling separate using onColumnOrderChange
    // Do NOT set initialState.columnOrder here as it might conflict with the reactive ref
    onSortingChange: updaterOrValue => valueUpdater(updaterOrValue, sorting),
    onExpandedChange: updaterOrValue => valueUpdater(updaterOrValue, expanded),
    onColumnVisibilityChange: updaterOrValue => valueUpdater(updaterOrValue, columnVisibility),
    onPaginationChange: updaterOrValue => valueUpdater(updaterOrValue, pagination),
    onGlobalFilterChange: updaterOrValue => valueUpdater(updaterOrValue, globalFilter),
    onColumnSizingChange: updaterOrValue => valueUpdater(updaterOrValue, columnSizing),
    onColumnOrderChange: updaterOrValue => valueUpdater(updaterOrValue, columnOrder),
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    enableColumnResizing: true,
    columnResizeMode: columnResizeMode.value,
    // Let table derive column sizing info from the state ref
    // Remove onColumnSizingInfoChange if not strictly needed for custom logic
    // onColumnSizingInfoChange: (info) => { ... },
    defaultColumn,
})

// Simplified row expansion handler
const handleRowClick = (row: Row<Record<string, any>>) => (e: MouseEvent) => {
    // Don't expand if clicking on an interactive element
    if ((e.target as HTMLElement).closest('.actions-dropdown, button, a, input, select')) {
        return;
    }
    row.toggleExpanded();
    // No need to manually set dataset attribute, use :class binding in template
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

// Initialize default sorting on mount
onMounted(() => {
    // Initialize default sort by timestamp if available
    if (timestampFieldName.value) {
        // Check if the column exists in the initial order derived from state/defaults
        if (columnOrder.value.includes(timestampFieldName.value)) {
            if (!sorting.value || sorting.value.length === 0) {
                sorting.value = [{ id: timestampFieldName.value, desc: true }]
            }
        }
    }
})

// Add refs for DOM elements
const tableContainerRef = ref<HTMLElement | null>(null)
const tableRef = ref<HTMLElement | null>(null)

onMounted(() => {
    if (!tableContainerRef.value) return

    const resizeObserver = new ResizeObserver(() => {
        // Force a layout update when container size changes
        table.setColumnSizing({ ...columnSizing.value })
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

// sourceDetails ref is already declared at the top
const sourceDetails = ref<Source | null>(null)

// Watch for source details changes
watch(
    () => exploreStore.sourceId,
    async (newSourceId) => {
        if (newSourceId) {
            const result = await sourcesStore.getSource(newSourceId)
            if (result.success && result.data) {
                sourceDetails.value = result.data as Source
            } else {
                sourceDetails.value = null; // Clear if fetch fails or no data
            }
        } else {
            sourceDetails.value = null; // Clear if sourceId is null/0
        }
    },
    { immediate: true }
)

// This watch is now redundant as we handle column creation in the combined watch above

// --- Native Drag and Drop Implementation ---

// Utility function to move array element (needed for native DnD)
function arrayMove<T>(arr: T[], fromIndex: number, toIndex: number): T[] {
    const newArr = [...arr];
    const element = newArr.splice(fromIndex, 1)[0];
    newArr.splice(toIndex, 0, element);
    return newArr;
}

const onDragStart = (event: DragEvent, columnId: string) => {
    draggingColumnId.value = columnId;
    if (event.dataTransfer) {
        event.dataTransfer.effectAllowed = 'move';
        // Optional: Set drag image if needed
        // event.dataTransfer.setData('text/plain', columnId); // Set data for compatibility
    }
    // Add a class to the body or table to indicate dragging state globally if needed
    document.body.classList.add('dragging-column');
}

const onDragEnter = (event: DragEvent, columnId: string) => {
    if (draggingColumnId.value && draggingColumnId.value !== columnId) {
        dragOverColumnId.value = columnId;
    }
    event.preventDefault(); // Necessary to allow drop
}

const onDragOver = (event: DragEvent) => {
    event.preventDefault(); // Necessary to allow drop
    if (event.dataTransfer) {
        event.dataTransfer.dropEffect = 'move';
    }
}

const onDragLeave = (event: DragEvent, columnId: string) => {
    if (dragOverColumnId.value === columnId) {
        dragOverColumnId.value = null;
    }
}

const onDrop = (event: DragEvent, targetColumnId: string) => {
    event.preventDefault();
    if (draggingColumnId.value && draggingColumnId.value !== targetColumnId) {
        const oldIndex = columnOrder.value.indexOf(draggingColumnId.value);
        const newIndex = columnOrder.value.indexOf(targetColumnId);
        if (oldIndex !== -1 && newIndex !== -1) {
            const newOrder = arrayMove(columnOrder.value, oldIndex, newIndex);
            table.setColumnOrder(newOrder); // Update table state via the handler
        }
    }
    // Cleanup
    draggingColumnId.value = null;
    dragOverColumnId.value = null;
    document.body.classList.remove('dragging-column');
}

const onDragEnd = () => { // No event parameter
    // Ensure cleanup happens regardless of drop success
    draggingColumnId.value = null;
    dragOverColumnId.value = null;
    document.body.classList.remove('dragging-column');
}

// Helper function to format execution time
function formatExecutionTime(ms: number): string {
    if (ms >= 1000) {
        return `${(ms / 1000).toFixed(2)}s`;
    }
    return `${Math.round(ms)}ms`;
}

</script>

<template>
    <div class="h-full flex flex-col w-full min-w-0 flex-1 overflow-hidden"
        :class="{ 'cursor-col-resize select-none': isResizing }">
        <!-- Subtle resize indicator - just a cursor change, no overlay -->

        <!-- Results Header with Controls and Pagination -->
        <div class="flex items-center justify-between p-2 border-b flex-shrink-0">
            <!-- Left side - Query stats -->
            <div class="flex-1 text-sm text-muted-foreground">
                <span v-if="stats && stats.execution_time_ms !== undefined" class="text-sm">
                    Query time: <span class="font-medium">{{ formatExecutionTime(stats.execution_time_ms) }}</span>
                </span>
                <span v-if="stats && stats.rows_read !== undefined" class="ml-3 text-sm">
                    Rows: <span class="font-medium">{{ stats.rows_read.toLocaleString() }}</span>
                </span>
            </div>

            <!-- Right side controls with pagination moved to top -->
            <div class="flex items-center gap-3">
                <!-- Export CSV Button with Dropdown -->
                <DropdownMenu v-if="table && table.getRowModel().rows?.length > 0">
                    <DropdownMenuTrigger asChild>
                        <Button variant="outline" size="sm" class="h-8 flex items-center gap-1"
                            title="Export table data">
                            <Download class="h-4 w-4 mr-1" />
                            Export
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
                <DataTablePagination v-if="table && table.getRowModel().rows?.length > 0" :table="table" />

                <!-- Column selector -->
                <DataTableColumnSelector v-if="table" :table="table" :column-order="columnOrder"
                    @update:column-order="table.setColumnOrder($event)" />

                <!-- Search input -->
                <div class="relative w-64">
                    <Search class="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
                    <Input placeholder="Search across all columns..." aria-label="Search in all columns"
                        v-model="globalFilter" class="pl-8 h-8 text-sm" />
                </div>
            </div>
        </div>

        <!-- Table Section with full-height scrolling -->
        <div class="flex-1 relative overflow-hidden" ref="tableContainerRef">
            <!-- Add v-if="table" here -->
            <div v-if="table && table.getRowModel().rows?.length" class="absolute inset-0">
                <div
                    class="w-full h-full overflow-auto scrollbar-thin scrollbar-thumb-gray-400/50 scrollbar-track-transparent">
                    <table ref="tableRef" class="table-fixed border-separate border-spacing-0 text-sm shadow-sm"
                        :data-resizing="isResizing">
                        <thead class="sticky top-0 z-10 bg-card border-b shadow-sm">
                            <!-- Check table.getHeaderGroups() exists -->
                            <tr v-if="table.getHeaderGroups().length > 0 && table.getHeaderGroups()[0]"
                                class="border-b border-b-muted-foreground/10">
                                <th v-for="header in table.getHeaderGroups()[0].headers" :key="header.id" scope="col"
                                    class="group relative h-9 px-3 text-sm font-medium text-left align-middle bg-muted/30 whitespace-nowrap sticky top-0 z-20 overflow-hidden border-r border-muted/30"
                                    :class="[
                                        getColumnType(header.column) === 'timestamp' ? 'font-semibold' : '',
                                        getColumnType(header.column) === 'severity' ? 'font-semibold' : '',
                                        header.column.getIsResizing() ? 'border-r-2 border-r-primary' : '',
                                        header.column.id === draggingColumnId ? 'opacity-50 bg-primary/10' : '', // Style for dragging column
                                        header.column.id === dragOverColumnId ? 'border-l-2 border-l-primary' : '' // Style for drop target indicator
                                    ]" :style="{
                                        width: `${header.getSize()}px`,
                                        minWidth: `${header.column.columnDef.minSize ?? defaultColumn.minSize}px`,
                                        maxWidth: `${header.column.columnDef.maxSize ?? defaultColumn.maxSize}px`,
                                    }" draggable="true" @dragstart="onDragStart($event, header.column.id)"
                                    @dragenter="onDragEnter($event, header.column.id)" @dragover="onDragOver($event)"
                                    @dragleave="onDragLeave($event, header.column.id)"
                                    @drop="onDrop($event, header.column.id)" @dragend="onDragEnd">
                                    <div class="flex items-center h-full">
                                        <!-- Drag Handle -->
                                        <span
                                            class="flex items-center justify-center flex-shrink-0 w-5 h-full mr-1.5 cursor-grab text-muted-foreground/50 group-hover:text-muted-foreground"
                                            title="Drag to reorder column">
                                            <GripVertical class="h-4 w-4" />
                                        </span>

                                        <!-- Column Header Content (Title + Sort button from columns.ts) -->
                                        <div class="flex-grow min-w-0 overflow-hidden mr-5">
                                            <!-- Check header.column.columnDef.header exists -->
                                            <FlexRender v-if="!header.isPlaceholder && header.column.columnDef.header"
                                                :render="header.column.columnDef.header" :props="header.getContext()" />
                                        </div>

                                        <!-- Column Resizer (Absolute Positioned) -->
                                        <div v-if="header.column.getCanResize()"
                                            class="absolute right-0 top-0 h-full w-5 cursor-col-resize select-none touch-none flex items-center justify-center hover:bg-muted/40 transition-colors z-10"
                                            @mousedown="(e) => { e.preventDefault(); e.stopPropagation(); handleResize(e, header); }"
                                            @touchstart="(e) => { e.preventDefault(); e.stopPropagation(); handleResize(e, header); }"
                                            @click.stop title="Resize column">
                                            <!-- Resize Grip Visual -->
                                            <div class="h-full w-4 flex flex-col items-center justify-center">
                                                <div
                                                    class="resize-grip flex flex-col items-center justify-center gap-1">
                                                    <div
                                                        class="w-1 h-1 rounded-full bg-muted-foreground/60 group-hover:bg-primary">
                                                    </div>
                                                    <div
                                                        class="w-1 h-1 rounded-full bg-muted-foreground/60 group-hover:bg-primary">
                                                    </div>
                                                    <div
                                                        class="w-1 h-1 rounded-full bg-muted-foreground/60 group-hover:bg-primary">
                                                    </div>
                                                </div>
                                            </div>
                                        </div>
                                    </div>
                                </th>
                            </tr>
                        </thead>

                        <tbody>
                            <template v-for="(row, index) in table.getRowModel().rows" :key="row.id">
                                <tr class="group cursor-pointer border-b transition-colors hover:bg-muted/30" :class="[
                                    row.getIsExpanded() ? 'expanded-row bg-primary/15' : index % 2 === 0 ? 'bg-transparent' : 'bg-muted/5'
                                ]" @click="handleRowClick(row)($event)">
                                    <td v-for="cell in row.getVisibleCells()" :key="cell.id"
                                        class="px-3 py-2 align-top font-mono text-xs leading-normal overflow-hidden border-r border-muted/20"
                                        :class="[
                                            cell.column.getIsResizing() ? 'border-r-2 border-r-primary' : '',
                                        ]" :style="{
                                            width: `${cell.column.getSize()}px`,
                                            minWidth: `${cell.column.columnDef.minSize ?? defaultColumn.minSize}px`,
                                            maxWidth: `${cell.column.columnDef.maxSize ?? defaultColumn.maxSize}px`
                                        }">
                                        <div class="flex items-center gap-1 w-full overflow-hidden">
                                            <div class="whitespace-pre w-full overflow-hidden"
                                                :title="formatCellValue(cell.getValue())">
                                                <FlexRender v-if="cell.column.columnDef.cell"
                                                    :render="cell.column.columnDef.cell" :props="cell.getContext()" />
                                            </div>
                                        </div>
                                    </td>
                                </tr>

                                <tr v-if="row.getIsExpanded()" class="expanded-json-row">
                                    <td :colspan="row.getVisibleCells().length" class="p-0">
                                        <div class="p-3 bg-muted/30 border-y border-y-primary/40">
                                            <JsonViewer :value="row.original" :expanded="false" class="text-xs" />
                                        </div>
                                    </td>
                                </tr>
                            </template>
                        </tbody>
                    </table>
                </div>
            </div>
            <!-- Check table exists for empty state -->
            <div v-else-if="table" class="h-full">
                <EmptyState />
            </div>
            <!-- Optional: Add a loading indicator if table is not yet defined -->
            <div v-else class="h-full flex items-center justify-center">
                <p class="text-muted-foreground">Initializing table...</p>
            </div>
        </div>
    </div>
</template>

<style scoped>
/* Add scoped attribute back */
/* Table styling for log analytics */
.table-fixed {
    table-layout: fixed;
    width: 100%;
    border-collapse: separate;
    border-spacing: 0;
}

/* Add clear borders to create a grid-like structure */
.table-fixed th:last-child,
.table-fixed td:last-child {
    border-right: none;
}

/* Resize handle and cursor styling */
.cursor-col-resize {
    user-select: none;
    -webkit-user-select: none;
    -moz-user-select: none;
    -ms-user-select: none;
    touch-action: none;
    cursor: col-resize !important;
}

/* Ensure resize cursor and prevent text selection during resizing */
[data-resizing="true"] * {
    cursor: col-resize !important;
    user-select: none !important;
}

/* Highlight column being resized */
[data-resizing="true"] th.border-r-primary,
[data-resizing="true"] td.border-r-primary {
    border-right-width: 2px;
    border-right-color: hsl(var(--primary)) !important;
}

/* Style for resize grip */
.resize-grip {
    height: 16px;
    transition: transform 0.15s ease;
}

.group:hover .resize-grip {
    transform: scale(1.2);
}

.group:hover .w-1 {
    background-color: hsl(var(--primary));
}

/* Better visibility for cell borders */
.border-muted\/20 {
    border-color: hsl(var(--muted) / 0.2);
}

.border-muted\/30 {
    border-color: hsl(var(--muted) / 0.3);
}

/* Add visual marker for column boundaries */
.table-fixed th,
.table-fixed td {
    position: relative;
}

/* Make table row alternating colors more visible */
.table-fixed tbody tr:nth-child(odd) {
    background-color: hsl(var(--muted) / 0.03);
}

.table-fixed tbody tr:nth-child(even) {
    background-color: transparent;
}

/* Add highlight style */
:deep(.search-highlight) {
    background-color: hsl(var(--highlight, 60 100% 75%));
    /* Use theme variable with fallback */
    color: hsl(var(--highlight-foreground, 0 0% 0%));
    /* Use theme variable with fallback */
    padding: 0 1px;
    margin: 0 -1px;
    /* Prevent layout shift */
    border-radius: 2px;
    box-shadow: 0 0 0 1px hsl(var(--highlight, 60 100% 75%) / 0.5);
    /* Subtle outline */
}

/* Ensure highlight works well in dark mode if theme variables are set */
.dark :deep(.search-highlight) {
    background-color: hsl(var(--highlight, 60 90% 55%));
    color: hsl(var(--highlight-foreground, 0 0% 0%));
    box-shadow: 0 0 0 1px hsl(var(--highlight, 60 90% 55%) / 0.7);
}

/* Ensure proper rendering inside table cells - single line with no wrapping */
td>.flex>.whitespace-pre {
    white-space: pre !important;
    /* Prevent wrapping */
    overflow: hidden !important;
    /* Hide overflow within the cell's inner div */
    text-overflow: ellipsis !important;
    /* Add ellipsis for hidden text */
}

/* Ensure JSON objects don't wrap in table cells */
:deep(.json-content) {
    white-space: nowrap !important;
    /* Prevent wrapping for JSON content */
    overflow: hidden !important;
    /* Hide overflow */
    text-overflow: ellipsis !important;
    /* Add ellipsis for hidden text */
    display: inline-block !important;
    /* Keep it as inline block */
    max-width: 100% !important;
    /* Limit width to cell size */
}

/* Add cursor styling for drag handle */
.cursor-grab {
    cursor: grab;
}

.cursor-grabbing {
    cursor: grabbing;
}

/* Add global style for body when dragging */
.dragging-column {
    cursor: grabbing !important;
    /* Force grabbing cursor */
}

/* Optional: Style for the drag feedback (e.g., slightly transparent) */
.group[draggable="true"]:active {
    /* opacity: 0.7; */
    /* Browser might provide its own feedback */
}

/* Style for drop indicator */
.border-l-primary {
    border-left-color: hsl(var(--primary)) !important;
}

/* More prominent hover effect for table rows */
.table-fixed tbody tr:hover:not([data-expanded="true"]) {
    background-color: hsl(var(--muted) / 0.4) !important;
    box-shadow: inset 0 0 0 1px hsl(var(--muted) / 0.5);
    transition: background-color 0.15s ease, box-shadow 0.15s ease;
}

/* Active/expanded row styling - using complementary colors */
/* Use the class bound in the template */
.table-fixed tbody tr.expanded-row {
    background-color: hsl(var(--primary) / 0.15) !important;
    border-top: 1px solid hsl(var(--primary) / 0.3);
    border-bottom: 1px solid hsl(var(--primary) / 0.3);
    box-shadow: 0 0 0 1px hsl(var(--primary) / 0.2);
    position: relative;
    z-index: 1;
    /* Ensures expanded rows appear above others */
}

/* --- Heuristic Formatting Styles (using :deep to target dynamic content) --- */

/* Base HTTP Method Tag Style */
:deep(.http-method) {
    display: inline-block;
    padding: 1px 6px;
    border-radius: 4px;
    font-weight: 400;
    font-size: 0.6875rem;
    /* 11px */
    line-height: 1.4;
    margin: 0 2px;
    border: 1px solid transparent;
    white-space: nowrap;
}

/* Utility HTTP Methods (PATCH, OPTIONS) */
:deep(.http-method-utility) {
    background-color: #f3f4f6;
    /* gray-100 */
    color: #4b5563;
    /* gray-600 */
    border: 1px solid #e5e7eb;
    /* gray-200 */
}

.dark :deep(.http-method-utility) {
    background-color: #374151;
    /* gray-700 */
    color: #d1d5db;
    /* gray-300 */
    border: 1px solid #4b5563;
    /* gray-600 */
}

/* GET - Success Green */
:deep(.http-method-get) {
    background-color: #ecfccb;
    /* lime-100 */
    color: #4d7c0f;
    /* lime-800 */
    border-color: #a3e635;
    /* lime-400 */
}

.dark :deep(.http-method-get) {
    background-color: #365314;
    /* lime-950 */
    color: #d9f99d;
    /* lime-300 */
    border-color: #4d7c0f;
    /* lime-800 */
}

/* POST - Cyan */
:deep(.http-method-post) {
    background-color: #cffafe;
    /* cyan-100 */
    color: #155e75;
    /* cyan-800 */
    border-color: #67e8f9;
    /* cyan-300 */
}

.dark :deep(.http-method-post) {
    background-color: #164e63;
    /* cyan-950 */
    color: #a5f3fc;
    /* cyan-200 */
    border-color: #155e75;
    /* cyan-800 */
}

/* PUT - Amber */
:deep(.http-method-put) {
    background-color: #fef3c7;
    /* amber-100 */
    color: #92400e;
    /* amber-800 */
    border-color: #fcd34d;
    /* amber-300 */
}

.dark :deep(.http-method-put) {
    background-color: #78350f;
    /* amber-950 */
    color: #fde68a;
    /* amber-200 */
    border-color: #92400e;
    /* amber-800 */
}

/* DELETE - Red */
:deep(.http-method-delete) {
    background-color: #fee2e2;
    /* red-100 */
    color: #991b1b;
    /* red-800 */
    border-color: #fca5a5;
    /* red-300 */
}

.dark :deep(.http-method-delete) {
    background-color: #7f1d1d;
    /* red-950 */
    color: #fecaca;
    /* red-200 */
    border-color: #991b1b;
    /* red-800 */
}

/* HEAD - Indigo */
:deep(.http-method-head) {
    background-color: #e0e7ff;
    /* indigo-100 */
    color: #3730a3;
    /* indigo-800 */
    border-color: #a5b4fc;
    /* indigo-300 */
}

.dark :deep(.http-method-head) {
    background-color: #312e81;
    /* indigo-950 */
    color: #c7d2fe;
    /* indigo-200 */
    border-color: #3730a3;
    /* indigo-800 */
}

/* Status Code Styling */
:deep(.status-code) {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 35px;
    height: 20px;
    padding: 1px 6px;
    border-radius: 10px;
    font-weight: 400;
    font-size: 0.6875rem;
    /* 11px */
    line-height: 1.4;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
    transition: transform 0.1s ease-in-out;
    white-space: nowrap;
    cursor: help;
}

:deep(.status-code:hover) {
    transform: scale(1.05);
}

/* Status Code Types */
:deep(.status-info) {
    background-color: #e0f2fe;
    /* light blue */
    color: #075985;
    border: 1px solid #7dd3fc;
}

.dark :deep(.status-info) {
    background-color: #0c4a6e;
    color: #bae6fd;
    border: 1px solid #0284c7;
}

:deep(.status-success) {
    background-color: #dcfce7;
    /* light green */
    color: #166534;
    border: 1px solid #86efac;
}

.dark :deep(.status-success) {
    background-color: #14532d;
    color: #bbf7d0;
    border: 1px solid #16a34a;
}

:deep(.status-redirect) {
    background-color: #fef9c3;
    /* light yellow */
    color: #854d0e;
    border: 1px solid #fde047;
}

.dark :deep(.status-redirect) {
    background-color: #713f12;
    color: #fef08a;
    border: 1px solid #ca8a04;
}

:deep(.status-error) {
    background-color: #fee2e2;
    /* light red */
    color: #b91c1c;
    border: 1px solid #fca5a5;
}

.dark :deep(.status-error) {
    background-color: #7f1d1d;
    color: #fecaca;
    border: 1px solid #ef4444;
}

:deep(.status-server) {
    background-color: #ffe4e6;
    /* light pink */
    color: #be123c;
    border: 1px solid #fda4af;
}

.dark :deep(.status-server) {
    background-color: #881337;
    color: #fecdd3;
    border: 1px solid #e11d48;
}

/* Timestamp Formatting */
:deep(.timestamp) {
    /* Optional: Add a subtle background or border if needed */
}

/* Refined Timestamp Colors for better distinction */
:deep(.timestamp-date) {
    color: hsl(var(--foreground) / 0.75);
    /* More distinct dimming for date */
}

.dark :deep(.timestamp-date) {
    color: hsl(var(--foreground) / 0.65);
}

:deep(.timestamp-separator) {
    color: hsl(var(--muted-foreground) / 0.5);
    /* Even more subtle separator */
    margin: 0 1px;
}

:deep(.timestamp-time) {
    color: hsl(var(--foreground));
    /* Keep time prominent */
    font-weight: 500;
    /* Keep slightly bolder */
}

.dark :deep(.timestamp-time) {
    color: hsl(var(--foreground));
    font-weight: 500;
}

:deep(.timestamp-offset) {
    color: hsl(var(--muted-foreground) / 0.6);
    margin-left: 2px;
    font-size: 0.5625rem;
    /* 90% of base */
}

.dark :deep(.timestamp-offset) {
    color: hsl(var(--muted-foreground) / 0.5);
}

/* HTTP Method Colors */
:deep(.http-method) {
    padding: 1px 4px;
    border-radius: 3px;
    font-weight: 500;
}

:deep(.http-method-utility) {
    background-color: #f3f4f6;
    /* gray-100 */
    color: #4b5563;
    /* gray-600 */
    border: 1px solid #e5e7eb;
    /* gray-200 */
}

.dark :deep(.http-method-utility) {
    background-color: #374151;
    /* gray-700 */
    color: #d1d5db;
    /* gray-300 */
    border: 1px solid #4b5563;
    /* gray-600 */
}

:deep(.http-method-get) {
    background-color: #ecfccb;
    /* lime-100 */
    color: #4d7c0f;
    /* lime-800 */
    border-color: #a3e635;
    /* lime-400 */
}

.dark :deep(.http-method-get) {
    background-color: #365314;
    /* lime-950 */
    color: #d9f99d;
    /* lime-300 */
    border-color: #4d7c0f;
    /* lime-800 */
}

/* Remove all severity styling as it's handled in utils.ts */
</style>
