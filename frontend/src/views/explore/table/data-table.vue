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
import LogTimelineModal from '@/components/log-timeline/LogTimelineModal.vue'
import { useExploreStore } from '@/stores/explore'
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

const exploreStore = useExploreStore()
const contextModalOpen = ref(false)
const selectedLog = ref<Record<string, any> | null>(null)

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

// Update showContext function
function showContext(log: Record<string, any>) {
    console.log('Opening context modal with:', { sourceId: props.sourceId, log })
    selectedLog.value = log
    contextModalOpen.value = true
}
</script>

<template>
    <div class="space-y-4">
        <!-- Search and Controls Bar -->
        <div class="flex items-center justify-between border-b pb-2">
            <!-- Left group - Search and Metadata controls -->
            <div class="flex items-center gap-4">
                <div class="relative w-72">
                    <Search class="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
                    <Input placeholder="Search in all columns..." v-model="globalFilter" class="pl-8" />
                </div>
                <DataTableToolbar :table="table" :stats="stats" :total-results="props.data.length" />
                <DataTableColumnSelector :table="table" />
            </div>

            <!-- Right group - Navigation controls -->
            <div class="flex items-center">
                <DataTablePagination :table="table" />
            </div>
        </div>

        <!-- Table Section -->
        <div class="rounded-md border">
            <div class="overflow-auto max-h-[calc(100vh-300px)]">
                <table class="w-full caption-bottom text-sm">
                    <thead class="sticky top-0 z-10 bg-background border-b">
                        <tr class="border-b border-b-muted-foreground/20">
                            <th v-for="header in table.getHeaderGroups()[0]?.headers || []" :key="header.id"
                                class="h-8 px-2 text-xs font-medium min-w-[150px] text-left align-middle">
                                <FlexRender v-if="!header.isPlaceholder" :render="header.column.columnDef.header"
                                    :props="header.getContext()" />
                            </th>
                        </tr>
                    </thead>
                    <tbody class="[&_tr:last-child]:border-0">
                        <template v-if="table.getRowModel().rows?.length">
                            <template v-for="row in table.getRowModel().rows" :key="row.id">
                                <!-- Main Row -->
                                <tr :data-state="row.getIsSelected() ? 'selected' : undefined"
                                    @click="(e) => handleRowClick(e, row)"
                                    class="cursor-pointer hover:bg-muted/50 border-b border-b-muted-foreground/10">
                                    <td v-for="cell in row.getVisibleCells()" :key="cell.id"
                                        class="h-auto px-2 py-1 min-w-[150px] whitespace-normal break-all align-middle group">
                                        <div class="max-w-none flex items-center gap-1">
                                            <!-- Cell Content -->
                                            <span v-if="getSeverityClasses(cell.getValue(), cell.column.id)"
                                                :class="getSeverityClasses(cell.getValue(), cell.column.id)">
                                                {{ cell.getValue() }}
                                            </span>
                                            <template v-else>
                                                <FlexRender :render="cell.column.columnDef.cell"
                                                    :props="cell.getContext()" />
                                            </template>

                                            <!-- Copy Value Button -->
                                            <Button variant="ghost" size="icon"
                                                class="h-4 w-4 p-0 opacity-0 group-hover:opacity-100 transition-opacity"
                                                @click.stop="copyCell(cell.getValue())" title="Copy value">
                                                <Copy class="h-3 w-3" />
                                            </Button>
                                        </div>
                                    </td>
                                </tr>

                                <!-- Expanded Row -->
                                <tr v-if="row.getIsExpanded()">
                                    <td :colspan="row.getVisibleCells().length" class="p-0">
                                        <div class="p-3 bg-muted/50">
                                            <JsonViewer :value="row.original" :expanded="false"
                                                :show-context-button="true" @show-context="showContext(row.original)" />
                                        </div>
                                    </td>
                                </tr>
                            </template>
                        </template>
                        <template v-else>
                            <tr>
                                <td :colspan="props.columns.length + 1" class="h-24 text-center text-sm">
                                    No results.
                                </td>
                            </tr>
                        </template>
                    </tbody>
                </table>
            </div>
        </div>

        <!-- Context Modal -->
        <LogTimelineModal v-if="selectedLog" v-model:isOpen="contextModalOpen" :source-id="props.sourceId"
            :log="selectedLog" />
    </div>
</template>

<style>
/* Remove hljs styles since they're now in JsonViewer */
</style>
