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
import { ref, onMounted } from 'vue'
import { Button } from '@/components/ui/button'
import { Check, Copy, Search } from 'lucide-vue-next'
import { valueUpdater, cn, getSeverityClasses } from '@/lib/utils'
import { Input } from '@/components/ui/input'
import DataTableToolbar from './data-table-toolbar.vue'
import DataTableColumnSelector from './data-table-column-selector.vue'
import DataTablePagination from './data-table-pagination.vue'
import DataTableDropdown from './data-table-dropdown.vue'
import hljs from 'highlight.js/lib/core'
import json from 'highlight.js/lib/languages/json'
import 'highlight.js/styles/stackoverflow-light.css'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
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

const { toast } = useToast()

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

// Initialize highlight.js with JSON language
hljs.registerLanguage('json', json)

// Format JSON with indentation and highlighting
function formatJSON(data: any): string {
    const jsonString = JSON.stringify(data, null, 2)
    return hljs.highlight(jsonString, { language: 'json' }).value
}

// Handle row click for expansion
const handleRowClick = (e: MouseEvent, row: any) => {
    // Don't expand if clicking on the dropdown
    if ((e.target as HTMLElement).closest('.actions-dropdown')) {
        return
    }
    row.toggleExpanded()
}

// Handle copy to clipboard with toast notification
const copyToClipboard = (data: any, id: string) => {
    const text = JSON.stringify(data, null, 2)
    navigator.clipboard.writeText(text)
    toast({
        title: 'Copied',
        description: 'Log data copied to clipboard',
        duration: TOAST_DURATION.SUCCESS,
    })
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
                            <!-- Actions column -->
                            <th class="w-10 px-2"></th>
                        </tr>
                    </thead>
                    <tbody class="[&_tr:last-child]:border-0">
                        <template v-if="table.getRowModel().rows?.length">
                            <template v-for="row in table.getRowModel().rows" :key="row.id">
                                <tr :data-state="row.getIsSelected() ? 'selected' : undefined"
                                    @click="(e) => handleRowClick(e, row)"
                                    class="cursor-pointer hover:bg-muted/50 border-b border-b-muted-foreground/10">
                                    <td v-for="cell in row.getVisibleCells()" :key="cell.id"
                                        class="h-auto px-2 py-1 min-w-[150px] whitespace-normal break-all align-middle">
                                        <div class="max-w-none">
                                            <span v-if="getSeverityClasses(cell.getValue(), cell.column.id)"
                                                :class="getSeverityClasses(cell.getValue(), cell.column.id)">
                                                {{ cell.getValue() }}
                                            </span>
                                            <template v-else>
                                                <FlexRender :render="cell.column.columnDef.cell"
                                                    :props="cell.getContext()" />
                                            </template>
                                        </div>
                                    </td>
                                    <!-- Actions dropdown -->
                                    <td class="w-10 px-2">
                                        <DataTableDropdown :log="row.original" @expand="row.toggleExpanded()"
                                            @copy="copyToClipboard(row.original, row.id)" />
                                    </td>
                                </tr>
                                <tr v-if="row.getIsExpanded()">
                                    <td :colspan="row.getVisibleCells().length + 1" class="p-0">
                                        <div class="p-3 bg-muted/50 relative">
                                            <!-- Copy button -->
                                            <Button variant="ghost" size="icon" class="absolute right-3 top-3"
                                                @click.stop="copyToClipboard(row.original, row.id)"
                                                :title="'Copy to clipboard'">
                                                <Copy class="h-4 w-4" />
                                            </Button>
                                            <!-- Highlighted JSON -->
                                            <pre
                                                class="text-sm font-mono overflow-auto"><code v-html="formatJSON(row.original)" /></pre>
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
    </div>
</template>

<style>
/* Just remove padding and background since we're in a pre tag already */
pre code.hljs {
    background: transparent;
    padding: 0;
}
</style>