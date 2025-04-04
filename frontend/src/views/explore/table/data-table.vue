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
import { Search, GripVertical } from 'lucide-vue-next'
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
import { useStorage, type UseStorageOptions, type RemovableRef } from '@vueuse/core'

interface Props {
    columns: ColumnDef<Record<string, any>>[]
    data: Record<string, any>[]
    stats: QueryStats
    sourceId: string
    teamId: number | null
    timestampField?: string
    severityField?: string
    timezone?: 'local' | 'utc'
}

// Define the structure for storing state
interface DataTableState {
    columnOrder: string[];
    columnSizing: ColumnSizingState;
    columnVisibility: VisibilityState;
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

// Watch for changes in columns
watch(() => props.columns, (newColumns) => {
    if (!newColumns || newColumns.length === 0) return;

    const { initialOrder, initialSizing, initialVisibility } = initializeState(newColumns);

    // Only update if different to prevent infinite loops
    if (JSON.stringify(columnOrder.value) !== JSON.stringify(initialOrder)) {
        columnOrder.value = initialOrder;
    }

    if (JSON.stringify(columnSizing.value) !== JSON.stringify(initialSizing)) {
        columnSizing.value = initialSizing;
    }

    if (JSON.stringify(columnVisibility.value) !== JSON.stringify(initialVisibility)) {
        columnVisibility.value = initialVisibility;
    }
}, { immediate: true });

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

// Memoize the resolved columns based on the current order and props
const resolvedColumns = computed(() => {
    const columnMap = new Map(props.columns.map(col => [col.id, col]));
    return columnOrder.value
        .map(id => columnMap.get(id))
        .filter((col): col is ColumnDef<Record<string, any>> => !!col);
});

// Initialize table
const table = useVueTable({
    get data() {
        return props.data
    },
    // Use the memoized resolvedColumns directly
    get columns() {
        return resolvedColumns.value;
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
        // Make sure timestamp field exists in the current order before sorting
        // The watch effect handles the initial columnOrder based on storage or defaults
        if (columnOrder.value.includes(timestampFieldName.value)) {
            // Check if sorting is already set (e.g., by stored state if we persist it later)
            if (!sorting.value || sorting.value.length === 0) {
                sorting.value = [{ id: timestampFieldName.value, desc: true }]
            }
        }
    }
    // Column sizing is handled by the watch effect that reads from storedState or defaults
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

</script>

<template>
    <div class="h-full flex flex-col w-full min-w-0 flex-1 overflow-hidden"
        :class="{ 'cursor-col-resize select-none': isResizing }">
        <!-- Subtle resize indicator - just a cursor change, no overlay -->

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
                <DataTableColumnSelector :table="table" :column-order="columnOrder"
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
            <div v-if="table.getRowModel().rows?.length" class="absolute inset-0">
                <div
                    class="w-full h-full overflow-auto scrollbar-thin scrollbar-thumb-gray-400/50 scrollbar-track-transparent">
                    <table ref="tableRef" class="table-fixed border-separate border-spacing-0 text-sm shadow-sm"
                        :data-resizing="isResizing">
                        <thead class="sticky top-0 z-10 bg-card border-b shadow-sm">
                            <tr v-if="table.getHeaderGroups()[0]" class="border-b border-b-muted-foreground/10">
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
                                            <!-- FlexRender now renders the Button containing the title and sort icon -->
                                            <FlexRender v-if="!header.isPlaceholder"
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
                                <!-- Bind expanded class directly -->
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
                                            <span
                                                v-if="cell.column.id === severityFieldName && getSeverityClasses(cell.getValue(), cell.column.id)"
                                                :class="getSeverityClasses(cell.getValue(), cell.column.id)"
                                                class="shrink-0 mx-1">
                                                {{ formatCellValue(cell.getValue()).toUpperCase() }}
                                            </span>
                                            <template v-else>
                                                <div class="whitespace-nowrap overflow-hidden text-ellipsis w-full">
                                                    <FlexRender :render="cell.column.columnDef.cell"
                                                        :props="cell.getContext()" />
                                                </div>
                                            </template>
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
            <div v-else class="h-full">
                <EmptyState />
            </div>
        </div>
    </div>
</template>

<style scoped>
/* Add scoped attribute */
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

/* --- Heuristic Formatting Styles --- */

/* Base HTTP Method Tag Style */
.http-method {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 4px;
  font-weight: 600;
  font-size: 11px; /* Slightly smaller */
  line-height: 1.4;
  margin: 0 2px;
  border: 1px solid transparent;
  white-space: nowrap;
}

/* Specific HTTP Method Colors (using Tailwind-like color shades) */
.http-method-get {
  background-color: #ecfccb; /* lime-100 */
  color: #4d7c0f; /* lime-800 */
  border-color: #a3e635; /* lime-400 */
}
.dark .http-method-get {
  background-color: #365314; /* lime-950 */
  color: #d9f99d; /* lime-300 */
  border-color: #4d7c0f; /* lime-800 */
}

.http-method-post {
  background-color: #cffafe; /* cyan-100 */
  color: #155e75; /* cyan-800 */
  border-color: #67e8f9; /* cyan-300 */
}
.dark .http-method-post {
  background-color: #164e63; /* cyan-950 */
  color: #a5f3fc; /* cyan-200 */
  border-color: #155e75; /* cyan-800 */
}

.http-method-put {
  background-color: #fef3c7; /* amber-100 */
  color: #92400e; /* amber-800 */
  border-color: #fcd34d; /* amber-300 */
}
.dark .http-method-put {
  background-color: #78350f; /* amber-950 */
  color: #fde68a; /* amber-200 */
  border-color: #92400e; /* amber-800 */
}

.http-method-delete {
  background-color: #fee2e2; /* red-100 */
  color: #991b1b; /* red-800 */
  border-color: #fca5a5; /* red-300 */
}
.dark .http-method-delete {
  background-color: #7f1d1d; /* red-950 */
  color: #fecaca; /* red-200 */
  border-color: #991b1b; /* red-800 */
}

.http-method-head {
  background-color: #e0e7ff; /* indigo-100 */
  color: #3730a3; /* indigo-800 */
  border-color: #a5b4fc; /* indigo-300 */
}
.dark .http-method-head {
  background-color: #312e81; /* indigo-950 */
  color: #c7d2fe; /* indigo-200 */
  border-color: #3730a3; /* indigo-800 */
}

/* Timestamp Formatting */
.timestamp {
  /* Optional: Add a subtle background or border if needed */
}
.timestamp-date {
  color: hsl(var(--foreground) / 0.85); /* Slightly dimmer date */
}
.dark .timestamp-date {
  color: hsl(var(--foreground) / 0.75);
}
.timestamp-separator {
  color: hsl(var(--muted-foreground) / 0.6); /* Very subtle separator */
  margin: 0 1px;
}
.timestamp-time {
  color: hsl(var(--foreground)); /* Normal time */
  font-weight: 500; /* Slightly bolder time */
}
.dark .timestamp-time {
  color: hsl(var(--foreground));
  font-weight: 500;
}
</style>
