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
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table'
import { ref } from 'vue'
import { Button } from '@/components/ui/button'
import { Check, Copy, Search } from 'lucide-vue-next'
import { valueUpdater } from '@/lib/utils'
import { Input } from '@/components/ui/input'
import DataTableToolbar from './data-table-toolbar.vue'
import DataTableColumnSelector from './data-table-column-selector.vue'
import DataTablePagination from './data-table-pagination.vue'
import type { QueryStats } from '@/api/explore'

interface Props {
    columns: ColumnDef<Record<string, any>>[]
    data: Record<string, any>[]
    stats: QueryStats
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
const copyState = ref<Record<string, boolean>>({})

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
    row.toggleExpanded()
}

// Handle copy to clipboard
const copyToClipboard = (data: any, id: string) => {
    const text = JSON.stringify(data, null, 2)
    navigator.clipboard.writeText(text)
    copyState.value[id] = true
    setTimeout(() => {
        copyState.value[id] = false
    }, 2000)
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
            <Table>
                <TableHeader>
                    <TableRow class="border-b border-b-muted-foreground/20">
                        <TableHead v-for="header in table.getHeaderGroups()[0]?.headers || []" :key="header.id"
                            class="h-8 px-2 text-xs font-medium whitespace-nowrap min-w-[150px] max-w-[300px]">
                            <FlexRender v-if="!header.isPlaceholder" :render="header.column.columnDef.header"
                                :props="header.getContext()" />
                        </TableHead>
                    </TableRow>
                </TableHeader>
                <TableBody>
                    <template v-if="table.getRowModel().rows?.length">
                        <template v-for="row in table.getRowModel().rows" :key="row.id">
                            <TableRow :data-state="row.getIsSelected() ? 'selected' : undefined"
                                @click="(e) => handleRowClick(e, row)"
                                class="cursor-pointer hover:bg-muted/50 border-b border-b-muted-foreground/10">
                                <TableCell v-for="cell in row.getVisibleCells()" :key="cell.id"
                                    class="h-7 px-2 py-1 min-w-[150px] max-w-[300px] truncate">
                                    <FlexRender :render="cell.column.columnDef.cell" :props="cell.getContext()" />
                                </TableCell>
                            </TableRow>
                            <TableRow v-if="row.getIsExpanded()">
                                <TableCell :colspan="row.getVisibleCells().length" class="p-0">
                                    <div class="p-3 bg-muted/50 relative">
                                        <Button variant="ghost" size="icon" class="absolute left-3 top-3"
                                            @click.stop="copyToClipboard(row.original, row.id)"
                                            :title="copyState[row.id] ? 'Copied!' : 'Copy to clipboard'">
                                            <Check v-if="copyState[row.id]" class="h-3.5 w-3.5 text-green-500" />
                                            <Copy v-else class="h-3.5 w-3.5" />
                                        </Button>
                                        <pre
                                            class="text-xs font-mono whitespace-pre-wrap pl-10">{{ JSON.stringify(row.original, null, 2) }}</pre>
                                    </div>
                                </TableCell>
                            </TableRow>
                        </template>
                    </template>
                    <template v-else>
                        <TableRow>
                            <TableCell :colspan="props.columns.length" class="h-24 text-center text-sm">
                                No results.
                            </TableCell>
                        </TableRow>
                    </template>
                </TableBody>
            </Table>
        </div>
    </div>
</template>