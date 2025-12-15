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
    type ColumnSizingState,
    type ColumnResizeMode,
} from '@tanstack/vue-table'
import { ref, computed, onMounted, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { GripVertical, Copy, Equal, EqualNot, Clock, ChevronUp, ChevronDown } from 'lucide-vue-next'
import LogTimelineModal from '@/components/log-timeline/LogTimelineModal.vue'
import { valueUpdater } from '@/lib/utils'
import type { QueryStats } from '@/api/explore'
import JsonViewer from '@/components/json-viewer/JsonViewer.vue'
import EmptyState from '@/views/explore/EmptyState.vue'
import { createColumns } from './columns'
import { useExploreStore } from '@/stores/explore'
import type { Source } from '@/api/sources'
import { useSourcesStore } from '@/stores/sources'
import TableControls from './TableControls.vue'

interface Props {
    columns: ColumnDef<Record<string, any>>[]
    data: Record<string, any>[]
    stats: QueryStats
    sourceId: string
    teamId: number | null
    displayMode?: 'table' | 'compact'
    timestampField?: string
    severityField?: string
    timezone?: 'local' | 'utc'
    queryFields?: string[] // Fields used in the query for column indicators
    regexHighlights?: Record<string, { pattern: string, isNegated: boolean }> // Column-specific regex patterns
    activeMode?: 'logchefql' | 'clickhouse-sql' | 'sql' // Current query mode
    isLoading?: boolean // Prop to indicate loading state
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
    queryFields: () => [],
    regexHighlights: () => ({}),
    activeMode: 'logchefql',
    isLoading: false // Default isLoading to false
})

// Get the actual field names to use with fallbacks
const timestampFieldName = computed(() => {
    // Ensure we prioritize the timestampField prop, which should contain the _meta_ts_field value
    return props.timestampField || 'timestamp';
})
const severityFieldName = computed(() => props.severityField || 'severity_text')

// Move tableColumns declaration near the top
const tableColumns = ref<CustomColumnDef[]>([])

// Table state
const sorting = ref<SortingState>([])
const expanded = ref<ExpandedState>({})

// Context modal state
const showContextModal = ref(false)
const contextLog = ref<Record<string, any> | null>(null)


// Open context modal for a log
const openContextModal = (log: Record<string, any>) => {
    contextLog.value = log
    showContextModal.value = true
}
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

    if (savedState && savedState.columnOrder && savedState.columnOrder.length > 0) {
        // --- Use Saved State ---
        console.log("Loading table state from localStorage for key:", storageKey.value);
        // Validate saved order against current columns
        const savedOrder = savedState.columnOrder;
        const filteredSavedOrder = savedOrder.filter(id => currentColumnIds.includes(id));
        const newColumnIds = currentColumnIds.filter(id => !filteredSavedOrder.includes(id));
        initialOrder = [...filteredSavedOrder, ...newColumnIds];

        // Process column sizing and visibility from saved state
        const savedSizing = savedState.columnSizing || {};
        const savedVisibility = savedState.columnVisibility || {};

        currentColumnIds.forEach(id => {
            // Handle sizing (prioritize saved state)
            if (savedSizing[id] !== undefined) {
                initialSizing[id] = savedSizing[id];
            } else {
                const columnDef = columns.find(c => c.id === id);
                initialSizing[id] = columnDef?.size ?? defaultColumn.size;
            }

            // Handle visibility (prioritize saved state)
            initialVisibility[id] = savedVisibility[id] !== undefined ? savedVisibility[id] : true;
        });
    } else {
        // --- Generate Default State ---
        console.log("No saved state found or no column order, generating default table state for key:", storageKey.value);
        // Generate default order with timestamp first
        const tsField = timestampFieldName.value; // Get the current timestamp field name
        const otherColumns = currentColumnIds.filter(id => id !== tsField);

        if (currentColumnIds.includes(tsField)) {
            initialOrder = [tsField, ...otherColumns]; // Put timestamp first
        } else {
            initialOrder = currentColumnIds; // Timestamp column doesn't exist, use default order
        }

        // Set default sizing and visibility for all current columns
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
    () => [props.columns, displayTimezone.value, props.timestampField], // Also watch timestampField changes
    ([newColumns, newTimezone]) => {
        if (!newColumns || newColumns.length === 0) {
            tableColumns.value = []; // Clear columns if input is empty
            // Reset dependent state if columns are cleared
            columnOrder.value = [];
            columnSizing.value = {};
            columnVisibility.value = {};
            return;
        }

        // Regenerate columns with the current timezone
        tableColumns.value = createColumns(
            newColumns as any, // Use the columns directly as was working before
            timestampFieldName.value,
            newTimezone as 'local' | 'utc',
            severityFieldName.value,
            props.queryFields,
            props.regexHighlights
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
// Allow very large maxSize for free resizing - professional log tools need this flexibility
const defaultColumn = {
    minSize: 50,
    size: 150,
    maxSize: 3000, // Effectively unlimited - allows users to resize columns as wide as needed
    enableResizing: true,
}

// Auto-fit column width based on content (double-click feature)
function autoFitColumn(header: any) {
    const columnId = header.column.id;
    const minSize = header.column.columnDef.minSize || defaultColumn.minSize;
    
    // Get all cells for this column
    const rows = table.getRowModel().rows;
    
    // Create a temporary element to measure text width
    const canvas = document.createElement('canvas');
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    
    // Use the same font as the table cells
    ctx.font = '13px ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace';
    
    // Measure header text width
    let maxWidth = ctx.measureText(columnId).width + 80; // Extra padding for header icons
    
    // Measure each cell's content width
    rows.forEach(row => {
        const cell = row.getVisibleCells().find(c => c.column.id === columnId);
        if (cell) {
            const value = cell.getValue();
            let text = '';
            if (value === null || value === undefined) {
                text = '-';
            } else if (typeof value === 'object') {
                text = JSON.stringify(value);
            } else {
                text = String(value);
            }
            const textWidth = ctx.measureText(text).width + 60; // Padding for cell + action buttons
            maxWidth = Math.max(maxWidth, textWidth);
        }
    });
    
    // Clamp to reasonable bounds
    const newSize = Math.max(minSize, Math.min(800, maxWidth));
    
    // Update column size
    const newSizing = {
        ...columnSizing.value,
        [columnId]: newSize
    };
    columnSizing.value = newSizing;
    table.setColumnSizing(newSizing);
}

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

        // Calculate new size respecting min constraint only (no max constraint for free resizing)
        const minSize = header.column.columnDef.minSize || defaultColumn.minSize;
        let newSize = Math.max(minSize, startSize + delta);

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

        // Calculate new size respecting min constraint only (no max constraint for free resizing)
        const minSize = header.column.columnDef.minSize || defaultColumn.minSize;
        let newSize = Math.max(minSize, startSize + delta);

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

// Row expansion handler - toggle expand/collapse
const handleRowClick = (row: Row<Record<string, any>>) => (e: MouseEvent) => {
    // Don't toggle if clicking on an interactive element or cell actions
    if ((e.target as HTMLElement).closest('.cell-actions, button, a, input, select')) {
        return;
    }
    row.toggleExpanded();
}

// Copy feedback state
const copiedCellId = ref<string | null>(null)

// Copy cell value with visual feedback
const copyCell = (value: any): boolean => {
    const text = typeof value === 'object' ? JSON.stringify(value, null, 2) : String(value)
    navigator.clipboard.writeText(text)
    return true
}

// Handle cell click - copy value with brief visual feedback
const handleCellClick = (_event: MouseEvent, cell: any) => {
    const cellId = cell.id
    copyCell(cell.getValue())
    
    // Show brief "copied" feedback
    copiedCellId.value = cellId
    setTimeout(() => {
        if (copiedCellId.value === cellId) {
            copiedCellId.value = null
        }
    }, 1000)
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

// Source details ref
const sourceDetails = ref<Source | null>(null);

// Use a computed property to get source details from the store instead of making API calls
const storeSourceDetails = computed(() => sourcesStore.currentSourceDetails);

// Watch for source details changes in the store
watch(
    storeSourceDetails,
    (newSourceDetails) => {
        sourceDetails.value = newSourceDetails;
    },
    { immediate: true }
)

// Watch for source ID changes and clear details when there's no source
watch(
    () => exploreStore.sourceId,
    (newSourceId) => {
        if (!newSourceId) {
            sourceDetails.value = null; // Clear if sourceId is null/0
        }
    }
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

const onDragLeave = (_event: DragEvent, columnId: string) => {
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


// Define emits
const emit = defineEmits<{
    (e: 'drill-down', value: { column: string, value: any, operator: string }): void
    (e: 'update:displayMode', value: 'table' | 'compact'): void
}>();

// Function to handle drill-down action with different operators
const handleDrillDown = (columnName: string, value: any, operator: string = '=') => {
    if (props.activeMode !== 'logchefql') return;

    emit('drill-down', { column: columnName, value, operator });
};

</script>

<template>
    <div class="h-full flex flex-col w-full min-w-0 flex-1 overflow-hidden"
        :class="{ 'cursor-col-resize select-none': isResizing }">
        <!-- Subtle resize indicator - just a cursor change, no overlay -->

        <!-- Shared Table Controls -->
        <TableControls 
            v-if="table"
            :table="table"
            :stats="stats"
            :is-loading="props.isLoading"
            @update:timezone="displayTimezone = $event"
            @update:globalFilter="globalFilter = $event"
        />

        <!-- Table Section with full-height scrolling -->
        <div class="flex-1 relative overflow-hidden" ref="tableContainerRef"
            :class="{ 'opacity-60 pointer-events-none': props.isLoading }"> <!-- Dim table during load -->
            <!-- Add v-if="table" here -->
            <div v-if="table && table.getRowModel().rows?.length" class="absolute inset-0">
                <div
                    class="w-full h-full overflow-auto scrollbar-thin scrollbar-thumb-gray-400/50 scrollbar-track-transparent transition-opacity duration-150">
                    <table ref="tableRef" class="table-fixed border-separate border-spacing-0 text-sm shadow-sm"
                        :data-resizing="isResizing">
                        <thead class="sticky top-0 z-10 bg-card border-b shadow-sm">
                            <!-- Check table.getHeaderGroups() exists -->
                            <tr v-if="table.getHeaderGroups().length > 0 && table.getHeaderGroups()[0]"
                                class="border-b border-b-muted-foreground/10">
                                <!-- Expand indicator column header -->
                                <th class="w-6 bg-muted/30 border-r border-muted/30"></th>
                                <th v-for="header in table.getHeaderGroups()[0].headers" :key="header.id" scope="col"
                                    class="group relative h-9 text-sm font-medium text-left align-middle bg-muted/30 whitespace-nowrap sticky top-0 z-20 overflow-hidden border-r border-muted/30 p-0"
                                    :class="[
                                        getColumnType(header.column) === 'timestamp' ? 'font-semibold' : '',
                                        getColumnType(header.column) === 'severity' ? 'font-semibold' : '',
                                        header.column.getIsResizing() ? 'border-r-2 border-r-primary' : '',
                                        header.column.id === draggingColumnId ? 'opacity-50 bg-primary/10' : '',
                                        header.column.id === dragOverColumnId ? 'border-l-2 border-l-primary' : ''
                                    ]" :style="{
                                        width: `${header.getSize()}px`,
                                        minWidth: `${header.column.columnDef.minSize ?? defaultColumn.minSize}px`,
                                    }" draggable="true" @dragstart="onDragStart($event, header.column.id)"
                                    @dragenter="onDragEnter($event, header.column.id)" @dragover="onDragOver($event)"
                                    @dragleave="onDragLeave($event, header.column.id)"
                                    @drop="onDrop($event, header.column.id)" @dragend="onDragEnd">
                                    <div class="flex items-center h-full px-3">
                                        <!-- Drag Handle -->
                                        <span
                                            class="flex items-center justify-center flex-shrink-0 w-5 h-full mr-1 cursor-grab text-muted-foreground/50 group-hover:text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity duration-150"
                                            title="Drag to reorder column">
                                            <GripVertical class="h-4 w-4" />
                                        </span>

                                        <!-- Column Header Content (Title + Sort button from columns.ts) -->
                                        <div class="flex-grow min-w-0 overflow-hidden">
                                            <!-- Check header.column.columnDef.header exists -->
                                            <FlexRender v-if="!header.isPlaceholder && header.column.columnDef.header"
                                                :render="header.column.columnDef.header" :props="header.getContext()" />
                                        </div>

                                        <!-- Column Resizer (Absolute Positioned) - Double-click to auto-fit -->
                                        <div v-if="header.column.getCanResize()"
                                            class="absolute right-0 top-0 h-full w-5 cursor-col-resize select-none touch-none flex items-center justify-center hover:bg-muted/40 transition-colors z-10 opacity-0 group-hover:opacity-100 transition-opacity duration-150"
                                            @mousedown="(e) => { e.preventDefault(); e.stopPropagation(); handleResize(e, header); }"
                                            @touchstart="(e) => { e.preventDefault(); e.stopPropagation(); handleResize(e, header); }"
                                            @dblclick.stop="autoFitColumn(header)"
                                            @click.stop title="Drag to resize • Double-click to auto-fit">
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
                                <tr class="group cursor-pointer border-b transition-colors hover:bg-muted/30 h-8" :class="[
                                    row.getIsExpanded() ? 'expanded-row bg-primary/15' : index % 2 === 0 ? 'bg-transparent' : 'bg-muted/5'
                                ]" @click="handleRowClick(row)($event)">
                                    <!-- Expand/collapse indicator - first pseudo-cell -->
                                    <td class="w-6 px-1 text-center text-muted-foreground/50 group-hover:text-muted-foreground transition-colors">
                                        <ChevronDown v-if="!row.getIsExpanded()" class="h-3.5 w-3.5 inline-block" />
                                        <ChevronUp v-else class="h-3.5 w-3.5 inline-block text-primary" />
                                    </td>
                                    <td v-for="cell in row.getVisibleCells()" :key="cell.id"
                                        class="px-3 py-1.5 align-middle font-mono text-xs overflow-hidden border-r border-muted/20 relative cell-hover-target whitespace-nowrap transition-colors duration-200"
                                        :class="[
                                            cell.column.getIsResizing() ? 'border-r-2 border-r-primary' : '',
                                            copiedCellId === cell.id ? 'bg-emerald-500/10' : '',
                                        ]" :style="{
                                            width: `${cell.column.getSize()}px`,
                                            minWidth: `${cell.column.columnDef.minSize ?? defaultColumn.minSize}px`,
                                        }">
                                        <div class="cell-content-wrapper w-full overflow-hidden whitespace-nowrap text-ellipsis"
                                            :title="formatCellValue(cell.getValue())">
                                            <FlexRender v-if="cell.column.columnDef.cell"
                                                :render="cell.column.columnDef.cell" :props="cell.getContext()" />
                                        </div>
                                        <!-- Minimal hover actions - positioned absolute, doesn't take content space -->
                                        <div class="cell-actions absolute right-1 top-1/2 -translate-y-1/2 flex items-center gap-0.5 bg-background/95 backdrop-blur-sm rounded px-0.5 shadow-sm border border-border/50">
                                            <!-- Copy button - always available -->
                                            <button 
                                                class="p-0.5 hover:bg-muted rounded text-muted-foreground hover:text-foreground"
                                                @click.stop="handleCellClick($event, cell)"
                                                title="Copy value"
                                            >
                                                <Copy class="h-3 w-3" />
                                            </button>
                                            <!-- Filter buttons - only in logchefQL mode -->
                                            <template v-if="props.activeMode === 'logchefql' && cell.column.id !== timestampFieldName">
                                                <button 
                                                    class="p-0.5 hover:bg-muted rounded text-muted-foreground hover:text-foreground"
                                                    @click.stop="handleDrillDown(cell.column.id, cell.getValue(), '=')"
                                                    title="Filter = this value"
                                                >
                                                    <Equal class="h-3 w-3" />
                                                </button>
                                                <button 
                                                    class="p-0.5 hover:bg-muted rounded text-muted-foreground hover:text-foreground"
                                                    @click.stop="handleDrillDown(cell.column.id, cell.getValue(), '!=')"
                                                    title="Filter ≠ this value"
                                                >
                                                    <EqualNot class="h-3 w-3" />
                                                </button>
                                            </template>
                                        </div>
                                    </td>
                                </tr>

                                <tr v-if="row.getIsExpanded()" class="expanded-json-row">
                                    <td :colspan="row.getVisibleCells().length + 1" class="p-0">
                                        <div class="p-3 bg-muted/30 border-y border-y-primary/40">
                                            <div class="flex items-center justify-between mb-2 gap-2">
                                                <!-- Collapse hint -->
                                                <button 
                                                    class="text-xs text-muted-foreground hover:text-foreground flex items-center gap-1 cursor-pointer"
                                                    @click.stop="row.toggleExpanded()"
                                                >
                                                    <ChevronUp class="h-3 w-3" />
                                                    <span>Collapse</span>
                                                </button>
                                                <Button 
                                                    variant="outline" 
                                                    size="sm" 
                                                    class="h-7 text-xs"
                                                    @click.stop="openContextModal(row.original)"
                                                >
                                                    <Clock class="h-3 w-3 mr-1" />
                                                    Show Context
                                                </Button>
                                            </div>
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

    <!-- Log Context Modal -->
    <LogTimelineModal
        v-if="contextLog"
        :is-open="showContextModal"
        :source-id="props.sourceId"
        :team-id="props.teamId ?? 0"
        :log="contextLog"
        :timestamp-field="timestampFieldName"
        @update:is-open="showContextModal = $event"
    />
</template>

<style scoped>
/* Add scoped attribute back */
/* Table styling for log analytics - optimized for smooth resizing */
.table-fixed {
    table-layout: fixed;
    width: max-content; /* Allow table to grow beyond container for horizontal scrolling */
    min-width: 100%; /* Ensure it fills container when columns fit */
    border-collapse: separate;
    border-spacing: 0;
    border: 1px solid hsl(var(--border) / 0.7);
    /* Add subtle outer border */
    border-radius: 6px;
    overflow: hidden;
    /* Keep rounded corners */
}

/* Add proper cell borders */
.table-fixed th,
.table-fixed td {
    border-right: 1px solid hsl(var(--border) / 0.4);
    border-bottom: 1px solid hsl(var(--border) / 0.4);
}

/* Remove right border from last column */
.table-fixed th:last-child,
.table-fixed td:last-child {
    border-right: none;
}

/* Remove bottom border from last row */
.table-fixed tbody tr:last-child td {
    border-bottom: none;
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

/* Improved header styling for consistent appearance */
.table-fixed th {
    padding: 0 !important;
    font-weight: 500;
    background-color: hsl(var(--muted) / 0.4);
    /* Slightly darker header background */
    border-bottom: 1px solid hsl(var(--border) / 0.8);
    /* More prominent bottom border */
    text-overflow: ellipsis;
    overflow: hidden;
}

/* Compact single-line rows for log stream feel */
.table-fixed tbody tr {
    height: 32px;
    max-height: 32px;
}

/* Make table row alternating colors more visible */
.table-fixed tbody tr:nth-child(odd) {
    background-color: hsl(var(--muted) / 0.05);
    /* Very slight background */
}

.table-fixed tbody tr:nth-child(even) {
    background-color: transparent;
}

/* More prominent hover effect for table rows */
.table-fixed tbody tr:hover:not([data-expanded="true"]) {
    background-color: hsl(var(--muted) / 0.25) !important;
    position: relative;
    z-index: 1;
    /* Ensure hover appears above other rows */
}

/* Rendering for table cells - single line for log stream feel */
td {
    white-space: nowrap !important;
    /* Force single line - critical for log stream UX */
}

td .cell-content-wrapper {
    white-space: nowrap !important;
    /* Prevent wrapping for clean single-line display */
    overflow: hidden !important;
    /* Hide overflow within the cell's inner div */
    text-overflow: ellipsis !important;
    /* Always show ellipsis when content overflows */
}

/* Ensure ALL nested content stays on single line */
td .cell-content-wrapper :deep(*) {
    white-space: nowrap !important;
}

/* flex-render-content is the main cell content wrapper - MUST be inline block for ellipsis */
td .cell-content-wrapper :deep(.flex-render-content) {
    white-space: nowrap !important;
    overflow: hidden !important;
    text-overflow: ellipsis !important;
    display: inline-block !important;
    max-width: 100% !important;
    vertical-align: middle !important;
}

td .cell-content-wrapper :deep(span) {
    white-space: nowrap !important;
    display: inline !important;
}

/* Timestamp specific - ensure no line breaks */
td>.flex>.cell-content :deep(.timestamp),
td>.flex>.cell-content :deep(.timestamp-date),
td>.flex>.cell-content :deep(.timestamp-time),
td>.flex>.cell-content :deep(.timestamp-offset),
td>.flex>.cell-content :deep(.timestamp-separator) {
    white-space: nowrap !important;
    display: inline !important;
}

/* Handling for JSON objects in table cells */
:deep(.json-content) {
    white-space: nowrap !important;
    /* Prevent wrapping for JSON content */
    overflow: hidden !important;
    /* Hide overflow */
    text-overflow: ellipsis !important;
    /* Always show ellipsis when content overflows */
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

/* Cell styling - consistent pointer cursor for clickable rows */
.cell-hover-target {
    position: relative;
    cursor: pointer;
}

.cell-hover-target,
.cell-hover-target *,
.cell-content-wrapper,
.cell-content-wrapper * {
    cursor: pointer;
}

/* Exception: action buttons in the floating pill */
.cell-actions button {
    cursor: pointer;
}

/* Hover action buttons - appear on row hover, positioned outside content flow */
.cell-actions {
    pointer-events: auto;
    z-index: 5;
}

/* Only show actions on direct cell hover, not entire row */
.cell-hover-target .cell-actions {
    opacity: 0;
    transition: opacity 0.15s ease;
}

.cell-hover-target:hover .cell-actions {
    opacity: 1;
}

/* Style for drop indicator */
.border-l-primary {
    border-left-color: hsl(var(--primary)) !important;
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

/* Timestamp Formatting - MUST stay inline for log stream UX */
:deep(.timestamp) {
    display: inline !important;
    white-space: nowrap !important;
}

/* All timestamp parts MUST be inline */
:deep(.timestamp-date),
:deep(.timestamp-separator),
:deep(.timestamp-time),
:deep(.timestamp-offset) {
    display: inline !important;
    white-space: nowrap !important;
}

/* Refined Timestamp Colors for better distinction */
:deep(.timestamp-date) {
    color: hsl(var(--foreground) / 0.60);
}

.dark :deep(.timestamp-date) {
    color: hsl(var(--foreground) / 0.50);
}

:deep(.timestamp-separator) {
    color: hsl(var(--muted-foreground) / 0.5);
}

:deep(.timestamp-time) {
    color: hsl(var(--foreground));
    font-weight: 500;
}

.dark :deep(.timestamp-time) {
    color: hsl(var(--foreground));
    font-weight: 500;
}

:deep(.timestamp-offset) {
    color: hsl(var(--muted-foreground) / 0.6);
    font-size: 0.5625rem;
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

/* Better render of header contents */
.table-fixed th>div {
    height: 100%;
    width: 100%;
    padding: 4px 8px;
    white-space: nowrap;
    text-overflow: ellipsis;
    overflow: hidden;
}

/* Ensure column headers show text properly */
.table-fixed th :deep(.truncate) {
    max-width: 100%;
    display: inline-block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    /* Required for flex truncation to work */
}

/* Refined header text display to work with any column name */
.table-fixed th :deep(.flex-grow) {
    flex: 1 1 auto;
    min-width: 0;
    /* Critical for text-overflow to work in flex containers */
    max-width: calc(100% - 25px);
    /* Leave room for icons */
}

/* Add better spacing for header content */
.table-fixed th>div> :deep(.w-full) {
    padding: 0 2px;
    display: flex;
    align-items: center;
    min-width: 0;
    /* Required for flexbox text truncation */
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
</style>
